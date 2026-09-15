package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/config"
)

// agentPath is where the AG-UI proxy is mounted, inside the bearer-auth group.
const agentPath = apiPrefix + "/agent"

// agentRunPath is the companion-relative URL every upstream agent descriptor is
// rewritten to, so that a browser never learns the Pando origin.
const agentRunPath = agentPath + "/run"

// agentProbeTimeout bounds the short, non-streaming upstream calls (/info,
// /healthz, the thread reads). The streaming routes carry no deadline of their
// own: an agent turn takes as long as it takes, and a client disconnect is what
// ends it.
const agentProbeTimeout = 15 * time.Second

// agentRetryAfter is the Retry-After, in seconds, on a refusal over the
// in-flight cap. A run is a conversation turn, so a few seconds is the right
// order of magnitude to come back in.
const agentRetryAfter = "5"

// The problem codes of the agent proxy. They are local to this file because
// every one of them is written with an explicit status: a client that has never
// heard of them still learns from the status whether it may retry.
const (
	// codeAgentRepoUnknown means ?repo= names no mounted repository. It is a
	// 404 rather than a fallback: relaying a turn to another repository's agent
	// would hand it the wrong working tree.
	codeAgentRepoUnknown = "agent_repo_unknown"
	// codeAgentBusy means the global in-flight run cap is reached.
	codeAgentBusy = "agent_busy"
	// codeAgentUpstream means the AG-UI adapter could not be reached, or
	// answered with a status this proxy will not forward. Its detail never
	// carries the upstream body, which is how an upstream error message cannot
	// smuggle a credential back out.
	codeAgentUpstream = "agent_upstream"
)

// agentState owns the AG-UI proxy: the resolved routing table, the HTTP
// transports that dial it and the semaphore that caps the runs in flight.
//
// The tokens live inside the config.PandoTargets snapshot, which has no
// exported field and no marshaler, so nothing here can copy one into a response
// or a log line.
type agentState struct {
	// enabled is the process-level switch: `agent.enabled` or `serve --agent`.
	enabled bool
	// targets is the resolved `projectId -> {url, token}` routing table.
	targets config.PandoTargets
	log     *slog.Logger
	// sem is the global in-flight run cap. A send that cannot proceed is a 503,
	// never a queue: a browser waiting on an unbounded queue looks identical to
	// a hung agent.
	sem chan struct{}

	// mu guards the lazily built transports.
	mu       sync.Mutex
	secure   http.RoundTripper
	insecure http.RoundTripper
}

// newAgentState builds the proxy state from the resolved options. It always
// returns a value: a disabled proxy answers `not_implemented`, which is a
// different thing from a route that is not there.
func newAgentState(opts Options) *agentState {
	maxRuns := opts.Pando.MaxRuns()
	if maxRuns <= 0 {
		maxRuns = config.DefaultPandoMaxRuns
	}
	return &agentState{
		enabled: opts.Agent,
		targets: opts.Pando,
		log:     opts.Logger,
		sem:     make(chan struct{}, maxRuns),
	}
}

// available reports whether the proxy will serve a request: the feature has to
// be switched on and an upstream has to be configured. It is what
// `features.agent` reports.
func (a *agentState) available() bool {
	return a != nil && a.enabled && a.targets.Configured()
}

// acquire takes one of the in-flight run slots. It reports false immediately
// when the cap is reached rather than blocking.
func (a *agentState) acquire() (release func(), ok bool) {
	select {
	case a.sem <- struct{}{}:
		return func() { <-a.sem }, true
	default:
		return nil, false
	}
}

// transport returns the round tripper for an upstream. The insecure one exists
// for an `agui-serve` left on its self-signed default; the recommended
// deployment is `--no-tls` on loopback and never reaches it.
func (a *agentState) transport(insecureTLS bool) http.RoundTripper {
	a.mu.Lock()
	defer a.mu.Unlock()
	if insecureTLS {
		if a.insecure == nil {
			t := newAgentTransport()
			t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // `agent.pando.insecureTls` is an explicit operator opt-in
			a.insecure = t
		}
		return a.insecure
	}
	if a.secure == nil {
		a.secure = newAgentTransport()
	}
	return a.secure
}

// newAgentTransport builds a transport tuned for a long-lived SSE hop: no
// response-header timeout to cut an agent's thinking pause short, and
// compression disabled so a frame is flushed the moment it arrives instead of
// waiting for a compression window.
func newAgentTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil, // a loopback hop never goes through a proxy
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
	}
}

// mountAgent registers the proxy routes. Every one of them relays a single
// upstream AG-UI route, so that the browser SDK sees the contract it expects
// without ever holding the upstream token or learning its origin.
func (s *Server) mountAgent(r chi.Router) {
	r.Get("/info", s.handleAgentInfo)
	r.Get("/health", s.handleAgentHealth)
	r.Post("/run", s.handleAgentRun)
	r.Get("/threads", s.handleAgentThreads)
	r.Get("/threads/{id}/messages", s.handleAgentThreadMessages)
	r.Get("/threads/{id}/stream", s.handleAgentThreadStream)
	r.Delete("/threads/{id}", s.handleAgentThreadDelete)
	r.Post("/runs/{id}/cancel", s.handleAgentRunCancel)
}

// handleAgentInfo serves the discovery document. It is the one route that is
// not a byte-for-byte relay: the upstream answers with absolute URLs into its
// own origin, and this rewrites every one of them to a companion-relative URL
// and drops whatever is left that would name the Pando listener.
func (s *Server) handleAgentInfo(w http.ResponseWriter, r *http.Request) {
	target, repoID, ok := s.agentUpstream(w, r)
	if !ok {
		return
	}
	doc, ok := s.agentFetchJSON(w, r, target, s.agent.targets.Token(repoID), target.Path+"/info")
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, rewriteAgentInfo(doc))
}

// handleAgentHealth relays the upstream liveness probe. It is the only route
// that dials the adapter without a token: /healthz is unauthenticated on
// Pando's side and carries nothing a caller could not learn from a refused
// connection.
func (s *Server) handleAgentHealth(w http.ResponseWriter, r *http.Request) {
	s.relayAgent(w, r, agentRelay{suffix: "/healthz", anonymous: true})
}

// handleAgentRun streams one conversation turn. It takes an in-flight slot,
// caps the request body, strips the browser's Origin and injects the upstream
// token, then copies the SSE frames through unbuffered until the upstream ends
// the run or the client goes away.
func (s *Server) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	target, _, ok := s.agentUpstream(w, r)
	if !ok {
		return
	}
	release, ok := s.agent.acquire()
	if !ok {
		w.Header().Set("Retry-After", agentRetryAfter)
		writeProblem(w, r, http.StatusServiceUnavailable, codeAgentBusy, "Agent busy",
			fmt.Sprintf("This companion already has %d agent runs in flight. Retry in a few seconds, or raise agent.pando.maxRuns.",
				s.agent.targets.MaxRuns()))
		return
	}
	defer release()
	s.relayAgent(w, r, agentRelay{
		suffix:  "/" + url.PathEscape(target.Agent),
		stream:  true,
		maxBody: maxRequestBody,
	})
}

// handleAgentThreads relays the thread list.
func (s *Server) handleAgentThreads(w http.ResponseWriter, r *http.Request) {
	s.relayAgent(w, r, agentRelay{suffix: "/threads"})
}

// handleAgentThreadMessages relays one thread's transcript.
func (s *Server) handleAgentThreadMessages(w http.ResponseWriter, r *http.Request) {
	s.relayAgent(w, r, agentRelay{suffix: "/threads/" + url.PathEscape(chi.URLParam(r, "id")) + "/messages"})
}

// handleAgentThreadStream reattaches to a thread's live run, which is an SSE
// stream and is relayed with the same flush behavior as a run.
func (s *Server) handleAgentThreadStream(w http.ResponseWriter, r *http.Request) {
	s.relayAgent(w, r, agentRelay{
		suffix: "/threads/" + url.PathEscape(chi.URLParam(r, "id")) + "/stream",
		stream: true,
	})
}

// handleAgentThreadDelete relays a thread deletion.
func (s *Server) handleAgentThreadDelete(w http.ResponseWriter, r *http.Request) {
	s.relayAgent(w, r, agentRelay{suffix: "/threads/" + url.PathEscape(chi.URLParam(r, "id"))})
}

// handleAgentRunCancel relays a cancellation.
func (s *Server) handleAgentRunCancel(w http.ResponseWriter, r *http.Request) {
	s.relayAgent(w, r, agentRelay{
		suffix:  "/runs/" + url.PathEscape(chi.URLParam(r, "id")) + "/cancel",
		maxBody: maxRequestBody,
	})
}

// agentRelay describes one relayed route.
type agentRelay struct {
	// suffix is the upstream path under the AG-UI mount point, leading slash
	// included.
	suffix string
	// stream marks a response that must reach the client frame by frame.
	stream bool
	// anonymous omits the injected Authorization header.
	anonymous bool
	// maxBody caps the request body; zero means the route carries none worth
	// buffering.
	maxBody int64
}

// relayAgent forwards one request to the AG-UI adapter.
//
// Four things make this a proxy and not a fetch: the browser's Origin, Referer,
// Cookie and Authorization headers are dropped so that Pando's CORS check never
// fires and the companion's own bearer token never leaves the machine; the
// upstream token is injected server-side; `FlushInterval: -1` copies every write
// through the moment it arrives, which is what an SSE stream needs; and the
// inbound request context is passed straight through, so a browser that aborts
// cancels the upstream run rather than leaving an agent talking to nobody.
func (s *Server) relayAgent(w http.ResponseWriter, r *http.Request, rel agentRelay) {
	target, repoID, ok := s.agentUpstream(w, r)
	if !ok {
		return
	}
	base, err := url.Parse(target.URL)
	if err != nil {
		writeProblem(w, r, http.StatusBadGateway, codeAgentUpstream, "Agent upstream",
			"The configured agent.pando.url is not a URL this companion can dial.")
		return
	}
	if rel.maxBody > 0 && !bufferAgentBody(w, r, rel.maxBody) {
		return
	}
	token := ""
	if !rel.anonymous {
		token = s.agent.targets.Token(repoID)
	}
	upstreamPath := target.Path + rel.suffix
	query := forwardedAgentQuery(r.URL.Query())

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = base.Scheme
			pr.Out.URL.Host = base.Host
			pr.Out.URL.Path = upstreamPath
			pr.Out.URL.RawQuery = query
			pr.Out.Host = base.Host
			// Nothing the browser sent that identifies an origin or carries a
			// credential is forwarded. SetXForwarded is deliberately not
			// called for the same reason.
			for _, header := range []string{
				"Origin", "Referer", "Cookie", "Authorization",
				"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto",
			} {
				pr.Out.Header.Del(header)
			}
			if token != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+token)
			}
		},
		Transport: s.agent.transport(target.InsecureTLS),
		ModifyResponse: func(resp *http.Response) error {
			// The companion sets its own CORS headers; the upstream's would
			// describe the Pando listener's policy, not this one's.
			resp.Header.Del("Access-Control-Allow-Origin")
			resp.Header.Del("Access-Control-Allow-Credentials")
			resp.Header.Del("Access-Control-Expose-Headers")
			if rel.stream {
				resp.Header.Set("Cache-Control", "no-cache")
				resp.Header.Set("X-Accel-Buffering", "no")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.agentUpstreamError(w, r, err)
		},
	}
	if rel.stream {
		proxy.FlushInterval = -1
	}
	proxy.ServeHTTP(w, r)
}

// forwardedAgentQuery rebuilds the upstream query string. The companion's own
// routing parameter and anything that looks like a credential are dropped: this
// proxy authenticates with a header, and a `?token=` on either hop would put a
// secret into a URL, a log and a browser history.
func forwardedAgentQuery(in url.Values) string {
	out := url.Values{}
	for key, values := range in {
		switch strings.ToLower(key) {
		case "repo", "token", "access_token", "api_key":
			continue
		}
		out[key] = values
	}
	return out.Encode()
}

// bufferAgentBody reads the request body into memory under a cap, so that an
// oversized turn is refused with a problem document before anything is sent
// upstream — rather than as a truncated stream halfway through a run.
func bufferAgentBody(w http.ResponseWriter, r *http.Request, maxBytes int64) bool {
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		failProblem(w, r, codeInvalidRequest, fmt.Sprintf("The request body could not be read: %v", err))
		return false
	}
	if int64(len(body)) > maxBytes {
		failProblem(w, r, codeInvalidRequest,
			fmt.Sprintf("The request body is larger than the %d byte limit of this endpoint.", maxBytes))
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return true
}

// agentUpstream resolves the upstream one request relays to, writing the
// problem document itself when it cannot.
func (s *Server) agentUpstream(w http.ResponseWriter, r *http.Request) (config.PandoTarget, string, bool) {
	if !s.agent.available() {
		failProblem(w, r, codeNotImplemented,
			"The agent proxy is off. Start the companion with `gintrack serve --agent` and set agent.pando.url in the configuration.")
		return config.PandoTarget{}, "", false
	}
	repoID := strings.TrimSpace(r.URL.Query().Get("repo"))
	if repoID != "" && !s.agent.targets.Known(repoID) {
		if _, mounted := s.repos.lookup(repoID); !mounted {
			writeProblem(w, r, http.StatusNotFound, codeAgentRepoUnknown, "Unknown repository",
				fmt.Sprintf("No mounted repository is called %q, and agent.pando.repos declares no upstream for it.", repoID))
			return config.PandoTarget{}, "", false
		}
	}
	target, ok := s.agent.targets.Target(repoID)
	if !ok {
		failProblem(w, r, codeNotImplemented,
			"No agent upstream is configured for this repository; set agent.pando.url or an agent.pando.repos row.")
		return config.PandoTarget{}, "", false
	}
	return target, repoID, true
}

// agentFetchJSON performs one short, non-streaming upstream call and decodes
// the JSON document it answers with.
func (s *Server) agentFetchJSON(w http.ResponseWriter, r *http.Request, target config.PandoTarget,
	token, path string,
) (map[string]any, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), agentProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL+path, http.NoBody)
	if err != nil {
		s.agentUpstreamError(w, r, err)
		return nil, false
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Transport: s.agent.transport(target.InsecureTLS)}).Do(req)
	if err != nil {
		s.agentUpstreamError(w, r, err)
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// The upstream body is deliberately not echoed: it is an error message
		// written by another process, and this response goes to a browser.
		writeProblem(w, r, http.StatusBadGateway, codeAgentUpstream, "Agent upstream",
			fmt.Sprintf("The agent adapter answered %d %s.", resp.StatusCode, http.StatusText(resp.StatusCode)))
		return nil, false
	}
	var doc map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRequestBody)).Decode(&doc); err != nil {
		writeProblem(w, r, http.StatusBadGateway, codeAgentUpstream, "Agent upstream",
			"The agent adapter did not answer with a JSON document.")
		return nil, false
	}
	return doc, true
}

// agentUpstreamError maps a transport failure onto a problem document. A
// cancelled request writes nothing: the client that would read it is the one
// that went away.
func (s *Server) agentUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(r.Context().Err(), context.Canceled) {
		s.log.Debug("agent run cancelled by the client", "path", r.URL.Path)
		return
	}
	// The error can name the upstream host, which is exactly what this proxy
	// hides from the browser, so the detail is written here rather than
	// forwarded. The full error goes to the operator's log instead.
	s.log.Warn("agent upstream failed", "path", r.URL.Path, "error", err)
	writeProblem(w, r, http.StatusBadGateway, codeAgentUpstream, "Agent upstream",
		"The agent adapter could not be reached. Check that `pando agui-serve` is running.")
}

// rewriteAgentInfo turns Pando's discovery document into one a browser may
// hold: every agent is addressable at the companion's own run endpoint, the
// declared mount point becomes the companion's, and every absolute URL left
// anywhere in the document is removed rather than forwarded.
//
// Removing rather than rewriting is deliberate. The proxy knows what
// `agents[].url` means; it does not know what some future key pointing at the
// Pando origin means, and a key the browser never sees cannot leak an origin.
func rewriteAgentInfo(doc map[string]any) map[string]any {
	if doc == nil {
		return map[string]any{}
	}
	if agents, ok := doc["agents"].([]any); ok {
		for _, entry := range agents {
			descriptor, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			descriptor["url"] = agentRunPath
		}
	}
	if _, ok := doc["path"]; ok {
		doc["path"] = agentPath
	}
	scrubbed, _ := scrubAbsoluteURLs(doc).(map[string]any)
	return scrubbed
}

// scrubAbsoluteURLs removes every string that parses as an absolute URL from a
// decoded JSON document, at any depth.
func scrubAbsoluteURLs(node any) any {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if text, ok := child.(string); ok {
				if isAbsoluteURL(text) {
					delete(value, key)
				}
				continue
			}
			value[key] = scrubAbsoluteURLs(child)
		}
		return value
	case []any:
		out := make([]any, 0, len(value))
		for _, child := range value {
			if text, ok := child.(string); ok {
				if isAbsoluteURL(text) {
					continue
				}
				out = append(out, text)
				continue
			}
			out = append(out, scrubAbsoluteURLs(child))
		}
		return out
	default:
		return node
	}
}

// isAbsoluteURL reports whether a string names a scheme and a host, which is
// the shape of a value that would tell a browser where Pando lives.
func isAbsoluteURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme != "" && u.Host != ""
}
