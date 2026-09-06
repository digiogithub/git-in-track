// Package tunnel exposes an already-listening local address to the public
// internet through a free Cloudflare "quick tunnel" — the anonymous
// *.trycloudflare.com service that needs no Cloudflare account, no DNS record
// and no inbound port.
//
// It drives github.com/cloudflare/cloudflared as a library rather than shelling
// out to the binary, and deliberately imports nothing under cloudflared/cmd/:
// that tree initializes Sentry, installs process-global signal handlers, starts
// an auto-updater and writes pidfiles, none of which belongs inside gintrack.
//
// A Manager can be started and stopped repeatedly inside one long-running
// process, which is what the "share this board" toggle in the web UI needs.
//
// The tunnel is anonymous and unauthenticated: anyone holding the URL reaches
// the origin. Callers decide whether that is acceptable and are responsible for
// tearing the tunnel down.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudflare/cloudflared/connection"
)

// gracePeriod is how long cloudflared waits for in-flight requests when the
// tunnel is asked to shut down.
const gracePeriod = 3 * time.Second

// eventBuffer sizes the channel between the cloudflared observer and the
// manager's dispatch loop. Sends are non-blocking, so a full buffer drops
// events rather than stalling cloudflared's own dispatcher.
const eventBuffer = 64

// Sentinel errors returned by the package.
var (
	// ErrNoOrigin reports that Options.Origin was empty.
	ErrNoOrigin = errors.New("tunnel origin address is empty")

	errBrokerRefused      = errors.New("provision quick tunnel: broker reported failure")
	errNoHostname         = errors.New("broker returned no hostname")
	errUnknownTLSSettings = errors.New("unknown TLS settings")
	errTunnelExited       = errors.New("tunnel supervisor exited unexpectedly")
)

// State is the coarse lifecycle state of a tunnel, as shown in the UI.
type State string

// The states a tunnel can be in.
const (
	// StateOff means no tunnel is provisioned or running.
	StateOff State = "off"
	// StateStarting means a tunnel is provisioned but not yet reachable.
	StateStarting State = "starting"
	// StateConnected means at least one edge connection is established.
	StateConnected State = "connected"
	// StateReconnecting means every edge connection dropped and cloudflared is
	// retrying.
	StateReconnecting State = "reconnecting"
	// StateError means the tunnel could not be started or died unrecoverably.
	StateError State = "error"
)

// Status is a snapshot of the tunnel. The zero value is a valid "off" status:
// an empty State means the same thing as StateOff, although a Manager always
// reports StateOff explicitly.
type Status struct {
	State       State
	URL         string    // "https://<name>.trycloudflare.com"; empty unless a tunnel is provisioned
	Connections int       // established edge connections
	Since       time.Time // when the current state was entered; zero when off
	Err         string    // last error, cleared on a successful start
}

// Options configures a Manager.
type Options struct {
	// Origin is the local address the tunnel forwards to, for example
	// "127.0.0.1:7317". It must already be listening: cloudflared dials it as
	// an ordinary loopback HTTP client.
	Origin string
	// Version is reported to the Cloudflare edge and used in the User-Agent.
	Version string
	// Logger receives cloudflared's own log stream. Nil means slog.Default().
	Logger *slog.Logger

	// endpoint overrides the trycloudflare.com broker base URL. Tests point it
	// at an httptest.Server; production always uses defaultAPIEndpoint.
	endpoint string
}

// Manager owns at most one quick tunnel at a time and can start and stop it
// repeatedly.
//
// The zero value is not usable; call NewManager.
type Manager struct {
	origin   string
	version  string
	endpoint string
	log      *slog.Logger

	// quiet is read by the zerolog bridge to demote cloudflared's cancellation
	// noise while a tunnel is being torn down.
	quiet atomic.Bool

	// generation identifies the current tunnel round. Start and Stop both
	// increment it under mu; the observer sink reads it without the lock, which
	// is why it is atomic. Events stamped with a stale generation are dropped,
	// so a late Disconnected from round 1 cannot clobber round 2's state.
	generation atomic.Uint64

	mu       sync.Mutex
	status   Status
	onChange func(Status)
	running  bool
	stopping bool
	conns    map[uint8]struct{}
	cancel   context.CancelFunc
	done     chan struct{}

	// observerOnce guards the single connection.Observer this manager ever
	// creates. connection.NewObserver spawns a dispatch goroutine that cannot
	// be stopped, so one per manager is created lazily and reused for every
	// round; a fresh one per Start would leak a goroutine each time.
	observerOnce sync.Once
	observer     *connection.Observer
	events       chan stampedEvent

	// now and runTunnel are indirection points for tests: the state machine can
	// be driven with a fake clock and without reaching Cloudflare.
	now       func() time.Time
	runTunnel func(ctx context.Context, creds connection.Credentials, hostname string) error
}

// stampedEvent is a cloudflared event tagged with the generation that was
// current when the sink received it.
type stampedEvent struct {
	generation uint64
	event      connection.Event
}

// NewManager returns a Manager for the origin in opts. No network calls happen
// until Start.
func NewManager(opts Options) *Manager {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	endpoint := opts.endpoint
	if endpoint == "" {
		endpoint = defaultAPIEndpoint
	}
	m := &Manager{
		origin:   opts.Origin,
		version:  opts.Version,
		endpoint: endpoint,
		log:      log,
		conns:    make(map[uint8]struct{}),
		status:   Status{State: StateOff},
		now:      time.Now,
	}
	m.runTunnel = m.runCloudflared
	return m
}

// Status returns the current snapshot.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// OnChange registers the single callback invoked whenever the status changes.
// It replaces any previously registered callback and is never invoked with the
// manager's lock held; the callback must not block.
func (m *Manager) OnChange(fn func(Status)) {
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

// Start provisions a quick tunnel and runs it in the background. It returns
// once the public URL is known, which is BEFORE the tunnel is reachable: the
// returned status is StateStarting with URL populated, and the manager only
// moves to StateConnected when the edge confirms a connection.
//
// ctx governs the whole lifetime of the tunnel, not just provisioning;
// canceling it stops the tunnel exactly as Stop does.
//
// Starting an already-running tunnel returns the current status and no error.
func (m *Manager) Start(ctx context.Context) (Status, error) {
	if m.origin == "" {
		return m.Status(), ErrNoOrigin
	}

	m.mu.Lock()
	if m.running {
		current := m.status
		m.mu.Unlock()
		return current, nil
	}
	m.running = true
	m.stopping = false
	m.quiet.Store(false)
	m.conns = make(map[uint8]struct{})
	generation := m.generation.Add(1)
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	done := make(chan struct{})
	m.done = done
	m.mu.Unlock()

	m.set(generation, func(s *Status) {
		*s = Status{State: StateStarting}
	})

	creds, hostname, err := provision(runCtx, m.endpoint, "cloudflared/"+m.version)
	if err != nil {
		cancel()
		close(done)
		m.fail(generation, err)
		return m.Status(), err
	}

	// The public URL is known here, before any connectivity exists.
	current := m.set(generation, func(s *Status) {
		s.State = StateStarting
		s.URL = "https://" + hostname
	})
	m.log.Info("cloudflare quick tunnel provisioned", "url", current.URL, "origin", m.origin)

	go func() {
		defer close(done)
		runErr := m.runTunnel(runCtx, creds, hostname)
		// Read the cancellation cause before canceling, otherwise the cleanup
		// below would make every exit look deliberate.
		cancelled := runCtx.Err() != nil
		cancel()
		if cancelled {
			// Cancelled on purpose: Stop (or the caller's ctx) owns the
			// resulting state, not this goroutine.
			return
		}
		if runErr == nil {
			runErr = errTunnelExited
		}
		m.fail(generation, runErr)
	}()

	return current, nil
}

// Stop tears the tunnel down and waits for it. Stopping a stopped tunnel is a
// no-op. It returns an error only when ctx expires before the tunnel has
// finished shutting down.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	m.stopping = true
	m.quiet.Store(true)
	// Bump the generation so events still in flight from this round are dropped
	// instead of being applied to the next one.
	generation := m.generation.Add(1)
	cancel, done := m.cancel, m.done
	m.cancel, m.done = nil, nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	var waitErr error
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			waitErr = fmt.Errorf("wait for tunnel shutdown: %w", ctx.Err())
		}
	}

	m.set(generation, func(s *Status) { *s = Status{State: StateOff} })

	m.mu.Lock()
	m.stopping = false
	m.conns = make(map[uint8]struct{})
	m.mu.Unlock()
	m.quiet.Store(false)

	return waitErr
}

// fail records err as the reason the tunnel is not running.
func (m *Manager) fail(generation uint64, err error) {
	m.mu.Lock()
	if generation != m.generation.Load() {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	m.set(generation, func(s *Status) {
		s.State = StateError
		s.Connections = 0
		s.Err = err.Error()
	})
	m.log.Error("cloudflare quick tunnel stopped", "error", err)
}

// set applies mutate to the status when generation is still current, and
// invokes the change callback outside the lock when the snapshot changed.
// It returns the snapshot after the mutation.
func (m *Manager) set(generation uint64, mutate func(*Status)) Status {
	m.mu.Lock()
	if generation != m.generation.Load() {
		current := m.status
		m.mu.Unlock()
		return current
	}
	before := m.status
	mutate(&m.status)
	switch {
	case m.status.State == StateOff:
		m.status.Since = time.Time{}
	case m.status.State != before.State:
		m.status.Since = m.now()
	}
	after := m.status
	changed := after != before
	callback := m.onChange
	m.mu.Unlock()

	if changed && callback != nil {
		callback(after)
	}
	return after
}

// runCloudflared is the production tunnel runner: it builds the cloudflared
// configuration and blocks until ctx is cancelled or the tunnel dies.
func (m *Manager) runCloudflared(ctx context.Context, creds connection.Credentials, hostname string) error {
	observer := m.ensureObserver()
	zlog := newZerolog(m.log, &m.quiet)

	tun, err := buildTunnel(ctx, creds, hostname, m.origin, m.version, observer, &zlog)
	if err != nil {
		return err
	}
	observer.SendURL(hostname)
	return tun.run(ctx)
}

// ensureObserver returns the manager's single connection.Observer, creating it
// and its dispatch loop on first use.
//
// connection.NewObserver starts a goroutine that cannot be stopped and events
// keep arriving from a tunnel that is already dead, so exactly one observer and
// exactly one sink exist for the manager's lifetime; the generation stamp is
// what separates one round from the next.
func (m *Manager) ensureObserver() *connection.Observer {
	m.observerOnce.Do(func() {
		zlog := newZerolog(m.log, &m.quiet)
		m.observer = connection.NewObserver(&zlog, &zlog)
		m.events = make(chan stampedEvent, eventBuffer)

		events := m.events
		m.observer.RegisterSink(connection.EventSinkFunc(func(event connection.Event) {
			// Never block cloudflared's dispatcher: a full buffer means the
			// manager is behind and the dropped event will be superseded.
			select {
			case events <- stampedEvent{generation: m.generation.Load(), event: event}:
			default:
			}
		}))

		go func() {
			for stamped := range events {
				m.applyEvent(stamped.generation, stamped.event)
			}
		}()
	})
	return m.observer
}

// applyEvent folds one cloudflared connection event into the status, ignoring
// events from a previous round or arriving while no tunnel is running.
func (m *Manager) applyEvent(generation uint64, event connection.Event) {
	m.mu.Lock()
	if generation != m.generation.Load() || !m.running || m.stopping {
		m.mu.Unlock()
		return
	}
	switch event.EventType {
	case connection.Connected:
		m.conns[event.Index] = struct{}{}
	case connection.Disconnected, connection.Reconnecting, connection.Unregistering:
		delete(m.conns, event.Index)
	case connection.SetURL, connection.RegisteringTunnel:
		// The URL is already known from provisioning, and registration is not
		// yet a connection.
	}
	connections := len(m.conns)
	m.mu.Unlock()

	m.set(generation, func(s *Status) {
		s.Connections = connections
		switch {
		case connections > 0:
			s.State = StateConnected
			s.Err = ""
		case s.State == StateConnected:
			// Every connection dropped; cloudflared retries on its own.
			s.State = StateReconnecting
		case event.EventType == connection.Reconnecting:
			s.State = StateReconnecting
		}
	})
}
