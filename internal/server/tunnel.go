package server

// The public tunnel: GET, POST and DELETE /api/v1/tunnel.
//
// The companion listens on loopback and has read and write access to the user's
// repositories. A tunnel takes that server and publishes it on the internet,
// where the bearer token is the only thing between a stranger with the URL and
// the backlog. Everything in this file is written around that one fact:
//
//   - the tunnel is never opened implicitly. It takes an explicit POST, the
//     `--tunnel` flag or `server.tunnel.enabled` in the configuration file;
//   - it is refused outright when the server runs without a token. New()
//     refuses an empty token only on a non-loopback bind, and a tunnel keeps
//     the bind on loopback, so that check never fires here: without this
//     refusal `gintrack serve --token none --tunnel` would publish the
//     workspace wide open;
//   - a toggle made over the API is not written back to the configuration, so
//     it cannot silently republish the workspace on the next `serve`
//     (config.Tunnel documents the reasoning);
//   - the token appears in no response, no log line and no event payload. The
//     public hostname does: it is not a secret, and an operator needs to see
//     what was published and when.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/tunnel"
)

// tunnelStopTimeout bounds how long a disable waits for the tunnel to drain.
// It is deliberately independent of the request context: a client that hangs up
// mid-DELETE must not leave a half-torn-down tunnel behind.
const tunnelStopTimeout = 10 * time.Second

// errTunnelNeedsToken is why New refuses to start with a tunnel and no token.
// It is a startup error rather than a warning: a warning would leave the
// workspace published and unauthenticated for as long as the process runs.
var errTunnelNeedsToken = errors.New(
	"refusing to open a tunnel without a bearer token: a tunnel publishes this server on the internet, " +
		"where the token is the only thing protecting the repositories; drop `--token none` or unset `server.tunnel.enabled`")

// TunnelDriver is the slice of internal/tunnel this package drives. It is an
// interface so that a test can enable and disable the feature with a stub
// instead of provisioning a real tunnel against Cloudflare.
type TunnelDriver interface {
	// Start provisions the tunnel and returns once its public URL is known,
	// which is before it is reachable.
	Start(ctx context.Context) (tunnel.Status, error)
	// Stop tears it down and waits for it. It is idempotent.
	Stop(ctx context.Context) error
	// Status returns the current snapshot.
	Status() tunnel.Status
	// OnChange registers the single callback fired on every status change.
	OnChange(fn func(tunnel.Status))
}

// TunnelFactory builds the driver the server toggles. Options.NewTunnel
// replaces it in tests; production leaves it nil and gets a real Cloudflare
// quick tunnel.
type TunnelFactory func(tunnel.Options) (TunnelDriver, error)

// defaultTunnel builds the Cloudflare-backed manager.
func defaultTunnel(opts tunnel.Options) (TunnelDriver, error) {
	return tunnel.NewManager(opts), nil
}

// tunnelState owns the process-wide tunnel driver. One driver is created on the
// first enable and reused for every later toggle: internal/tunnel.Manager is
// process-scoped and starting a fresh one per enable would leak a goroutine
// each time.
type tunnelState struct {
	// provider and supported are fixed by the configuration at New time and
	// are read without the lock.
	provider  string
	supported bool
	// factory builds the driver on first use.
	factory TunnelFactory

	// mu serializes the toggles: a POST and a DELETE landing together must not
	// interleave a start with a stop.
	mu     sync.Mutex
	driver TunnelDriver
	live   bool
	// cancel stops the context governing the running tunnel, so that a driver
	// that dies on its own does not keep the round's goroutines alive.
	cancel context.CancelFunc
}

// tunnelBody is the JSON shape of every /api/v1/tunnel response. The web client
// is written against these exact field names; `since` is null rather than
// absent so that a client never has to tell "no tunnel" from "malformed body".
type tunnelBody struct {
	Supported   bool   `json:"supported"`
	Provider    string `json:"provider"`
	State       string `json:"state"`
	URL         string `json:"url"`
	Connections int    `json:"connections"`
	// Since is RFC 3339, null while there is no tunnel.
	Since *string `json:"since"`
	Error string  `json:"error"`
	// TokenConfigured says whether this server has a bearer token at all. It
	// never carries the token itself.
	TokenConfigured bool `json:"tokenConfigured"`
}

// TunnelInfo is what the command line needs to print about the tunnel, without
// exposing the whole HTTP body type.
type TunnelInfo struct {
	State string
	URL   string
	Err   string
}

// newTunnelState builds the tunnel layer for a set of options. An unknown
// provider is reported as unsupported rather than quietly served by Cloudflare.
func newTunnelState(opts Options) *tunnelState {
	provider := opts.Tunnel.Provider
	if provider == "" {
		provider = config.DefaultTunnelProvider
	}
	factory := opts.NewTunnel
	if factory == nil {
		factory = defaultTunnel
	}
	return &tunnelState{
		provider:  provider,
		supported: provider == config.DefaultTunnelProvider,
		factory:   factory,
	}
}

// tunnelSupported reports whether this build and configuration can tunnel at
// all. It is what /capabilities advertises as `features.tunnel`.
func (s *Server) tunnelSupported() bool { return s.tunnel.supported }

// handleTunnelStatus answers GET /api/v1/tunnel.
func (s *Server) handleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, s.tunnelView(s.tunnelSnapshot()))
}

// handleTunnelEnable answers POST /api/v1/tunnel. It replies 202: the tunnel is
// provisioned but not yet reachable when the URL is known, so the client polls
// or follows the `tunnel.changed` event until the state settles.
func (s *Server) handleTunnelEnable(w http.ResponseWriter, r *http.Request) {
	if s.opts.Token == "" {
		// The security refusal this whole file exists for. It is enforced here
		// rather than in the UI because the UI is not a boundary.
		failProblem(w, r, codeTunnelRequiresToken,
			"This companion runs without authentication, so a tunnel would publish the workspace to anyone "+
				"holding the URL. Restart it with a token (`gintrack serve --token new`) and try again.")
		return
	}
	if !s.tunnel.supported {
		failProblem(w, r, codeNotImplemented,
			fmt.Sprintf("This build cannot open a %q tunnel; set `server.tunnel.provider: %s`.",
				s.tunnel.provider, config.DefaultTunnelProvider))
		return
	}

	status, err := s.enableTunnel(r.Context())
	if err != nil {
		failProblem(w, r, codeTunnelFailed, err.Error())
		return
	}
	writeJSON(w, r, http.StatusAccepted, s.tunnelView(status))
}

// handleTunnelDisable answers DELETE /api/v1/tunnel. Disabling a tunnel that is
// not running is a success: the caller asked for "not published", and it is not.
func (s *Server) handleTunnelDisable(w http.ResponseWriter, r *http.Request) {
	status, err := s.disableTunnel(r.Context())
	if err != nil {
		failProblem(w, r, codeTunnelFailed, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, s.tunnelView(status))
}

// enableTunnel starts the tunnel, building the driver on first use.
func (s *Server) enableTunnel(ctx context.Context) (tunnel.Status, error) {
	t := s.tunnel
	t.mu.Lock()
	defer t.mu.Unlock()

	driver, err := t.driverLocked(s)
	if err != nil {
		return tunnel.Status{State: tunnel.StateOff}, err
	}

	// The tunnel outlives the request that asked for it: only a DELETE or the
	// server shutting down may tear it down, never the client hanging up.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	status, err := driver.Start(runCtx)
	if err != nil {
		cancel()
		return status, fmt.Errorf("open the tunnel: %w", err)
	}
	t.live = true
	t.cancel = cancel

	// WARN, not INFO: publishing a workspace on the internet is worth a line an
	// operator will actually see. The hostname is public; the token is not
	// logged here or anywhere else.
	s.log.Warn("public tunnel opened", "provider", t.provider, "url", status.URL, "origin", s.Addr())
	return status, nil
}

// disableTunnel stops the tunnel and waits for it to drain.
func (s *Server) disableTunnel(ctx context.Context) (tunnel.Status, error) {
	t := s.tunnel
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.driver == nil || !t.live {
		return t.snapshotLocked(), nil
	}
	published := t.driver.Status().URL
	cancel := t.cancel
	t.live, t.cancel = false, nil

	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), tunnelStopTimeout)
	defer stopCancel()
	err := t.driver.Stop(stopCtx)
	if cancel != nil {
		cancel()
	}
	s.log.Warn("public tunnel closed", "provider", t.provider, "url", published)
	if err != nil {
		return t.snapshotLocked(), fmt.Errorf("close the tunnel: %w", err)
	}
	return t.snapshotLocked(), nil
}

// driverLocked returns the process-wide driver, creating it on first use. The
// origin has to be the address the listener actually resolved, which is why the
// driver cannot be built in New: `--port 0` only learns it at listen time.
func (t *tunnelState) driverLocked(s *Server) (TunnelDriver, error) {
	if t.driver != nil {
		return t.driver, nil
	}
	driver, err := t.factory(tunnel.Options{
		Origin:  s.Addr(),
		Version: s.opts.Version,
		Logger:  s.log,
	})
	if err != nil {
		return nil, fmt.Errorf("build the tunnel driver: %w", err)
	}
	driver.OnChange(func(status tunnel.Status) {
		// The driver calls this from its own goroutine while holding its own
		// lock, so this must neither block nor reach back for t.mu. Publishing
		// to the hub does neither: it takes only the hub's lock and never
		// blocks on a slow client.
		s.hub.Publish(eventTunnelChanged, s.tunnelView(status))
	})
	t.driver = driver
	return driver, nil
}

// snapshotLocked reads the driver's status, or the "off" status when no driver
// has ever been built. The caller holds t.mu.
func (t *tunnelState) snapshotLocked() tunnel.Status {
	if t.driver == nil {
		return tunnel.Status{State: tunnel.StateOff}
	}
	return t.driver.Status()
}

// tunnelSnapshot reads the current status under the toggle lock.
func (s *Server) tunnelSnapshot() tunnel.Status {
	s.tunnel.mu.Lock()
	defer s.tunnel.mu.Unlock()
	return s.tunnel.snapshotLocked()
}

// TunnelStatus reports the running tunnel to the command line, which prints the
// public URL on the startup banner once it exists.
func (s *Server) TunnelStatus() TunnelInfo {
	status := s.tunnelSnapshot()
	return TunnelInfo{State: string(normalizeTunnelState(status.State)), URL: status.URL, Err: status.Err}
}

// normalizeTunnelState normalizes the zero State, which internal/tunnel documents as
// meaning the same thing as StateOff.
func normalizeTunnelState(state tunnel.State) tunnel.State {
	if state == "" {
		return tunnel.StateOff
	}
	return state
}

// tunnelView renders a driver status as the frozen JSON body.
func (s *Server) tunnelView(status tunnel.Status) tunnelBody {
	var since *string
	if !status.Since.IsZero() {
		stamp := status.Since.UTC().Format(time.RFC3339)
		since = &stamp
	}
	return tunnelBody{
		Supported:   s.tunnel.supported,
		Provider:    s.tunnel.provider,
		State:       string(normalizeTunnelState(status.State)),
		URL:         status.URL,
		Connections: status.Connections,
		Since:       since,
		Error:       status.Err,
		// Whether a token exists, never the token.
		TokenConfigured: s.opts.Token != "",
	}
}

// startTunnel opens the tunnel asked for by `server.tunnel.enabled` or
// `--tunnel`, once the listener has an address. A failure degrades the run to
// "local only" with an error in the log: the companion is still useful on
// loopback, and killing it would be a worse answer than not publishing it.
func (s *Server) startTunnel(ctx context.Context) {
	if !s.opts.Tunnel.Enabled {
		return
	}
	if _, err := s.enableTunnel(ctx); err != nil {
		s.log.Error("the public tunnel could not be opened; serving on loopback only", "error", err)
	}
}

// stopTunnel closes the tunnel on shutdown, so that a process that exits never
// leaves a published workspace behind.
func (s *Server) stopTunnel(ctx context.Context) {
	if _, err := s.disableTunnel(ctx); err != nil {
		s.log.Warn("closing the public tunnel", "error", err)
	}
}
