package server

// The browser-git CORS proxy, story GIT-US-0042 (docs/06-git-sync.md section
// 6.3, ADR-025).
//
// Browser-only mode speaks git over HTTP from a tab. Git hosts answer the smart
// HTTP endpoints without any `Access-Control-Allow-Origin`, so the tab cannot
// read the response and the network half of a sync cannot run. The companion,
// which is already running on the same machine as the browser, forwards those
// three requests and adds the headers.
//
// Forwarding requests on behalf of a browser is exactly the shape of a
// server-side request forgery hole, so the handler is written as a series of
// refusals and only the last line does any forwarding:
//
//  1. the caller must present an allowed `Origin` and the per-run bearer token
//     in `X-Gintrack-Token` — the token never travels in `Authorization`, which
//     is reserved for the git host's own credential;
//  2. the path must be one of the three git smart-HTTP endpoints, with the
//     method that endpoint takes;
//  3. the target host must be on the allow-list, which is derived from the
//     remotes of the registered repositories plus `git.corsProxy.allowedHosts`;
//  4. every IP the host resolves to must be a public unicast address, and the
//     connection is made to one of those exact addresses, so a name that
//     re-resolves to 127.0.0.1 between the check and the dial cannot be reached;
//  5. the request and response bodies are bounded, the whole exchange has a
//     deadline, and a redirect is re-validated against every rule above;
//  6. headers are copied through two allow-lists, never wholesale, in both
//     directions.
//
// Nothing here is reachable in browser-only mode without the companion: this is
// native server code, and `internal/core` stays free of it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"

	"github.com/digiogithub/git-in-track/internal/config"
)

// corsProxyPath is where the proxy is mounted. isomorphic-git rewrites a remote
// URL to `<corsProxy>/<host>/<path>`, so the mount point is a path prefix and
// the first segment after it is the target host.
const corsProxyPath = "/cors-proxy"

// proxyTokenHeader carries the companion's own bearer token. It is deliberately
// not `Authorization`: that header holds the git host's credential and is
// forwarded upstream, and the two must never be the same value.
const proxyTokenHeader = "X-Gintrack-Token"

// The bounds of one proxied exchange.
const (
	// defaultProxyMaxRequest bounds an uploaded pack. A backlog repository's
	// push is kilobytes; 32 MiB is far beyond any of them and still small
	// enough that a runaway client cannot occupy the machine's memory.
	defaultProxyMaxRequest = 32 << 20
	// defaultProxyMaxResponse bounds a fetched pack.
	defaultProxyMaxResponse = 256 << 20
	// proxyTimeout is the deadline of the whole exchange, including the dial.
	proxyTimeout = 120 * time.Second
	// proxyMaxRedirects is how many hops are followed. Hosts commonly answer
	// one redirect (`/repo` to `/repo.git`); nothing legitimate needs four.
	proxyMaxRedirects = 3
	// proxyRemoteCacheTTL is how long the allow-list derived from the
	// repositories' remotes is reused before it is read from disk again.
	proxyRemoteCacheTTL = 60 * time.Second
)

// The problem codes this endpoint raises. They are part of the catalog of
// docs/07-cli-and-api.md section 5.4.
const (
	codeProxyDisabled   = "cors_proxy_disabled"
	codeProxyForbidden  = "cors_proxy_forbidden"
	codeProxyBadTarget  = "cors_proxy_bad_target"
	codeProxyHostDenied = "cors_proxy_host_not_allowed"
	codeProxyBlocked    = "cors_proxy_target_blocked"
	codeProxyTooLarge   = "cors_proxy_too_large"
	codeProxyUpstream   = "cors_proxy_upstream_failed"
)

// errRequestTooLarge is returned by the bounded request body once the caller
// has sent more than the limit allows.
var errRequestTooLarge = errors.New("proxied request body exceeds the limit")

// errBlockedAddress is returned by the dialer for an address the policy refuses.
var errBlockedAddress = errors.New("target address is not a public unicast address")

// proxyRequestHeaders are the request headers copied upstream. `Authorization`
// is the git host's credential, supplied by the browser and meant for it;
// everything absent from this list — `Cookie`, `Origin`, `Referer`, the
// forwarding headers and the companion's own token header — is dropped.
var proxyRequestHeaders = []string{
	"Accept",
	"Accept-Language",
	"Authorization",
	"Content-Type",
	"Git-Protocol",
}

// proxyResponseHeaders are the response headers copied back. `Set-Cookie` and
// any upstream credential material are absent by construction: a header that is
// not on this list does not reach the tab.
var proxyResponseHeaders = []string{
	"Cache-Control",
	"Content-Type",
	"Expires",
	"Pragma",
	"WWW-Authenticate",
}

// proxyUserAgent is what the git host sees. The browser's own `User-Agent` is
// not forwarded: it identifies the user's machine and adds nothing.
const proxyUserAgent = "git/gintrack-cors-proxy"

// corsProxy is the state behind the endpoint: the policy, the HTTP client whose
// dialer enforces the address rules, and the cached allow-list.
type corsProxy struct {
	srv      *Server
	settings config.CORSProxy

	maxRequest  int64
	maxResponse int64

	// allowLoopback lets a test point the proxy at an httptest server. It is
	// never set outside this package's tests: production always refuses a
	// loopback target.
	allowLoopback bool

	// resolve is the name resolver. Tests replace it to exercise a name that
	// resolves into a blocked range without needing DNS.
	resolve func(ctx context.Context, host string) ([]net.IP, error)

	client *http.Client

	mu        sync.Mutex
	cached    map[string]struct{}
	cachedAt  time.Time
	nowFn     func() time.Time
	repoPaths func() []string
}

// newCORSProxy builds the proxy for a server.
func newCORSProxy(s *Server) *corsProxy {
	p := &corsProxy{
		srv:         s,
		settings:    s.opts.Git.CORSProxy,
		maxRequest:  defaultProxyMaxRequest,
		maxResponse: defaultProxyMaxResponse,
		nowFn:       s.now,
	}
	p.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", host, err)
		}
		ips := make([]net.IP, 0, len(addrs))
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
		return ips, nil
	}
	p.repoPaths = func() []string {
		mounted := s.repos.all()
		paths := make([]string, 0, len(mounted))
		for _, m := range mounted {
			paths = append(paths, m.path)
		}
		return paths
	}
	p.client = &http.Client{
		Transport: &http.Transport{
			DialContext:           p.dial,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          8,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: p.checkRedirect,
	}
	return p
}

// enabled reports whether the endpoint forwards anything at all.
func (p *corsProxy) enabled() bool { return p.settings.Enabled }

// mountCORSProxy mounts the proxy at /cors-proxy/. It sits outside the REST
// prefix because isomorphic-git builds the URL by concatenation and cannot be
// told to insert a version segment.
func (s *Server) mountCORSProxy(r chi.Router) {
	r.Handle(corsProxyPath, http.HandlerFunc(s.proxy.handle))
	r.Handle(corsProxyPath+"/*", http.HandlerFunc(s.proxy.handle))
}

// handle answers one proxied git request.
func (p *corsProxy) handle(w http.ResponseWriter, r *http.Request) {
	if !p.enabled() {
		writeProblem(w, r, http.StatusNotImplemented, codeProxyDisabled, "CORS proxy disabled",
			"This companion does not forward git requests. Set `git.corsProxy.enabled: true` in the configuration, "+
				"or configure your own proxy in Settings -> Sync (docs/06-git-sync.md section 6.3).")
		return
	}
	// Only a browser has any business here, and only one whose origin the
	// companion already trusts. A page on the open internet reaches the port
	// but never gets past this line.
	origin := r.Header.Get("Origin")
	if !p.srv.originAllowed(origin, r) {
		writeProblem(w, r, http.StatusForbidden, codeProxyForbidden, "Forbidden origin",
			"The CORS proxy answers only browser origins this companion trusts. Open the UI the companion serves, "+
				"or start it with `--allow-origin` for the origin you use.")
		return
	}
	if !p.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="gintrack", header="`+proxyTokenHeader+`"`)
		writeProblem(w, r, http.StatusUnauthorized, codeUnauthorized, "Unauthorized",
			"The CORS proxy requires this companion's bearer token in the "+proxyTokenHeader+" header. "+
				"The Authorization header is reserved for the git host's own credential and is forwarded to it.")
		return
	}

	target, err := p.parseTarget(r)
	if err != nil {
		var denied *proxyDenial
		if errors.As(err, &denied) {
			writeProblem(w, r, denied.status, denied.code, titleForCode(denied.code), denied.detail)
			return
		}
		writeProblem(w, r, http.StatusBadRequest, codeProxyBadTarget, "Bad proxy target", err.Error())
		return
	}

	allowed, err := p.allowedHosts(r.Context())
	if err != nil {
		p.srv.log.Warn("cors proxy: reading the repository remotes failed", "error", err)
	}
	if _, ok := allowed[target.Host]; !ok {
		writeProblem(w, r, http.StatusForbidden, codeProxyHostDenied, "Host not allowed",
			fmt.Sprintf("%s is not a remote of any registered repository and is not listed in "+
				"`git.corsProxy.allowedHosts`. The companion forwards git traffic only to hosts you already use.",
				target.Host))
		return
	}

	p.forward(w, r, target)
}

// authorized reports whether the caller presented this run's bearer token.
func (p *corsProxy) authorized(r *http.Request) bool {
	token := p.srv.opts.Token
	if token == "" {
		// Authentication is disabled, which New allows on a loopback bind only.
		return true
	}
	return tokenMatches(token, strings.TrimSpace(r.Header.Get(proxyTokenHeader)))
}

// proxyDenial is a refusal that already knows its problem code and status.
type proxyDenial struct {
	status int
	code   string
	detail string
}

// Error implements the error interface.
func (d *proxyDenial) Error() string { return d.detail }

// proxyTarget is a validated upstream request.
type proxyTarget struct {
	// Host is the host:port the request goes to, port always explicit.
	Host string
	// URL is the full upstream URL, always https.
	URL *url.URL
}

// parseTarget turns `/cors-proxy/<host>/<path>` into a validated https URL.
//
// It refuses everything that is not one of the three git smart-HTTP endpoints,
// which is what keeps the companion from being a general-purpose forwarder: the
// worst a caller can do with it is speak the git wire protocol to a host they
// already have a repository on.
func (p *corsProxy) parseTarget(r *http.Request) (proxyTarget, error) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), corsProxyPath)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return proxyTarget{}, errors.New("the proxy path is /cors-proxy/<host>/<repo path>")
	}
	host, path, ok := strings.Cut(rest, "/")
	if !ok || path == "" {
		return proxyTarget{}, errors.New("the proxy path is /cors-proxy/<host>/<repo path>")
	}
	normalized, err := normalizeHostPort(host)
	if err != nil {
		return proxyTarget{}, err
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return proxyTarget{}, errors.New("the repository path must not contain relative segments")
		}
	}

	query := r.URL.Query()
	if err := checkGitEndpoint(r.Method, path, query.Get("service")); err != nil {
		return proxyTarget{}, err
	}

	upstream := &url.URL{Scheme: "https", Host: normalized, Path: "/" + path, RawQuery: r.URL.RawQuery}
	// Path was taken from EscapedPath, so re-parse it to keep the escaping the
	// client sent rather than re-encoding it.
	upstream.RawPath = "/" + path
	if unescaped, err := url.PathUnescape(path); err == nil {
		upstream.Path = "/" + unescaped
	}
	return proxyTarget{Host: normalized, URL: upstream}, nil
}

// gitServices are the two services `info/refs` advertises.
var gitServices = map[string]bool{"git-upload-pack": true, "git-receive-pack": true}

// checkGitEndpoint refuses any path that is not the git smart-HTTP surface
// docs/06 section 6.3 names, and any method that surface does not use.
func checkGitEndpoint(method, path, service string) error {
	switch {
	case strings.HasSuffix(path, "/info/refs") || path == "info/refs":
		if method != http.MethodGet {
			return &proxyDenial{status: http.StatusMethodNotAllowed, code: codeProxyBadTarget,
				detail: "info/refs is a GET."}
		}
		if !gitServices[service] {
			return &proxyDenial{status: http.StatusForbidden, code: codeProxyBadTarget,
				detail: "info/refs is forwarded only for ?service=git-upload-pack or ?service=git-receive-pack, " +
					"never for the dumb protocol."}
		}
		return nil
	case strings.HasSuffix(path, "/git-upload-pack"), strings.HasSuffix(path, "/git-receive-pack"):
		if method != http.MethodPost {
			return &proxyDenial{status: http.StatusMethodNotAllowed, code: codeProxyBadTarget,
				detail: "the pack endpoints are a POST."}
		}
		return nil
	default:
		return &proxyDenial{status: http.StatusForbidden, code: codeProxyBadTarget,
			detail: "the proxy forwards only /info/refs, /git-upload-pack and /git-receive-pack. " +
				"It is a git proxy, not a general-purpose one."}
	}
}

// normalizeHostPort lower-cases a host and gives it an explicit port, so that
// the allow-list comparison is a string equality and cannot be fooled by
// spelling. `github.com` and `github.com:443` are the same target.
func normalizeHostPort(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" {
		return "", errors.New("the proxy path names no host")
	}
	if unescaped, err := url.PathUnescape(host); err == nil {
		host = unescaped
	}
	if strings.ContainsAny(host, "@/\\ ") {
		return "", errors.New("the host segment must be a bare host or host:port")
	}
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name, port = host, "443"
	}
	if name == "" || port == "" {
		return "", errors.New("the host segment must be a bare host or host:port")
	}
	if strings.HasPrefix(name, "[") {
		name = strings.Trim(name, "[]")
	}
	if strings.Contains(name, ":") {
		// An IPv6 literal: keep the bracketed spelling the URL needs.
		return "[" + name + "]:" + port, nil
	}
	return name + ":" + port, nil
}

// allowedHosts is the set of `host:port` targets the proxy will speak to: every
// remote of every registered repository, plus the explicitly configured hosts.
//
// Deriving it from the remotes is what makes the endpoint useful with no
// configuration at all and still not a general forwarder — a caller can only
// reach a host the user already keeps a repository on.
func (p *corsProxy) allowedHosts(ctx context.Context) (map[string]struct{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached != nil && p.nowFn().Sub(p.cachedAt) < proxyRemoteCacheTTL {
		return p.cached, nil
	}

	hosts := make(map[string]struct{})
	for _, raw := range p.settings.AllowedHosts {
		if normalized, err := normalizeHostPort(raw); err == nil {
			hosts[normalized] = struct{}{}
		}
	}
	var readErr error
	if p.settings.AllowRepoRemotes {
		for _, path := range p.repoPaths() {
			if err := ctx.Err(); err != nil {
				readErr = fmt.Errorf("collect remotes: %w", err)
				break
			}
			for _, host := range remoteHosts(path) {
				hosts[host] = struct{}{}
			}
		}
	}
	p.cached, p.cachedAt = hosts, p.nowFn()
	return hosts, readErr
}

// remoteHosts reads the hosts of one repository's remotes. A folder that is not
// a git working tree — a Jujutsu repository with no colocated git, a plain
// directory — simply contributes nothing.
func remoteHosts(path string) []string {
	repo, err := gogit.PlainOpenWithOptions(path, &gogit.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil
	}
	remotes, err := repo.Remotes()
	if err != nil {
		return nil
	}
	var out []string
	for _, remote := range remotes {
		for _, raw := range remote.Config().URLs {
			if host := hostOfRemoteURL(raw); host != "" {
				out = append(out, host)
			}
		}
	}
	return out
}

// hostOfRemoteURL extracts `host:port` from a remote URL, understanding both
// the URL form and git's scp-like `git@host:org/repo.git`. An SSH remote counts
// too: browser mode reaches the same host over HTTPS through the per-repository
// sync URL override of docs/06 section 6.2, and the host is the same one.
func hostOfRemoteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		// A URL form: `https://host/path`, `ssh://git@host:2222/path`, and the
		// `file://` remote of a local clone, which names no host at all.
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return ""
		}
		host, err := normalizeHostPort(u.Hostname())
		if err != nil {
			return ""
		}
		if port := u.Port(); port != "" {
			if host, err = normalizeHostPort(u.Host); err != nil {
				return ""
			}
		}
		return host
	}
	// scp-like: [user@]host:path, with no scheme and no leading slash.
	authority, _, ok := strings.Cut(raw, ":")
	if !ok {
		return ""
	}
	if _, after, found := strings.Cut(authority, "@"); found {
		authority = after
	}
	host, err := normalizeHostPort(authority)
	if err != nil {
		return ""
	}
	return host
}

// forward performs the upstream exchange and copies the answer back.
func (p *corsProxy) forward(w http.ResponseWriter, r *http.Request, target proxyTarget) {
	if r.ContentLength > p.maxRequest {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, codeProxyTooLarge, "Request too large",
			fmt.Sprintf("The proxy forwards at most %d bytes of request body.", p.maxRequest))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), proxyTimeout)
	defer cancel()

	var body io.Reader
	if r.Body != nil && r.Method == http.MethodPost {
		body = &boundedReader{r: r.Body, remaining: p.maxRequest}
	}
	upstream, err := http.NewRequestWithContext(ctx, r.Method, target.URL.String(), body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, codeProxyBadTarget, "Bad proxy target", err.Error())
		return
	}
	copyHeaders(upstream.Header, r.Header, proxyRequestHeaders)
	upstream.Header.Set("User-Agent", proxyUserAgent)
	// The upstream connection must never carry the browser's identity or the
	// companion's own token; both are absent from proxyRequestHeaders, and this
	// makes the intent explicit for the next reader.
	upstream.Header.Del(proxyTokenHeader)
	upstream.Header.Del("Cookie")

	resp, err := p.client.Do(upstream)
	if err != nil {
		p.writeUpstreamFailure(w, r, target, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.ContentLength > p.maxResponse {
		writeProblem(w, r, http.StatusBadGateway, codeProxyTooLarge, "Response too large",
			fmt.Sprintf("%s answered with %d bytes, more than the %d the proxy forwards. Run the companion's own sync for this repository.",
				target.Host, resp.ContentLength, p.maxResponse))
		return
	}

	header := w.Header()
	copyHeaders(header, resp.Header, proxyResponseHeaders)
	header.Set("X-Gintrack-Proxied-Host", target.Host)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, p.maxResponse)); err != nil {
		// The status line is already on the wire, so the truncated body is all
		// the client gets; the log is where this is visible.
		p.srv.log.Warn("cors proxy: copying the upstream response failed", "host", target.Host, "error", err)
	}
}

// writeUpstreamFailure maps a transport failure onto the right refusal, keeping
// a blocked address distinct from a host that simply did not answer.
func (p *corsProxy) writeUpstreamFailure(w http.ResponseWriter, r *http.Request, target proxyTarget, err error) {
	switch {
	case errors.Is(err, errRequestTooLarge):
		writeProblem(w, r, http.StatusRequestEntityTooLarge, codeProxyTooLarge, "Request too large",
			fmt.Sprintf("The proxy forwards at most %d bytes of request body.", p.maxRequest))
	case errors.Is(err, errBlockedAddress):
		writeProblem(w, r, http.StatusForbidden, codeProxyBlocked, "Target blocked",
			fmt.Sprintf("%s resolves to an address the proxy refuses to reach: loopback, private, link-local and "+
				"other non-public ranges are never forwarded to, whatever the name resolves to.", target.Host))
	default:
		var denied *proxyDenial
		if errors.As(err, &denied) {
			writeProblem(w, r, denied.status, denied.code, titleForCode(denied.code), denied.detail)
			return
		}
		p.srv.log.Debug("cors proxy: upstream request failed", "host", target.Host, "error", err)
		writeProblem(w, r, http.StatusBadGateway, codeProxyUpstream, "Upstream failed",
			fmt.Sprintf("The request to %s did not complete.", target.Host))
	}
}

// checkRedirect re-runs every rule on each hop. A host that answers a redirect
// into an internal service, or off the allow-list, gets the same refusal the
// original request would have got.
func (p *corsProxy) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= proxyMaxRedirects {
		return &proxyDenial{status: http.StatusBadGateway, code: codeProxyUpstream,
			detail: fmt.Sprintf("the git host redirected more than %d times", proxyMaxRedirects)}
	}
	if req.URL.Scheme != "https" {
		return &proxyDenial{status: http.StatusForbidden, code: codeProxyBlocked,
			detail: "the git host redirected to a non-HTTPS URL, which the proxy does not follow"}
	}
	host, err := normalizeHostPort(req.URL.Host)
	if err != nil {
		return &proxyDenial{status: http.StatusForbidden, code: codeProxyBlocked, detail: err.Error()}
	}
	allowed, _ := p.allowedHosts(req.Context())
	if _, ok := allowed[host]; !ok {
		return &proxyDenial{status: http.StatusForbidden, code: codeProxyHostDenied,
			detail: fmt.Sprintf("the git host redirected to %s, which is not on the allow-list", host)}
	}
	if err := checkGitEndpoint(req.Method, strings.TrimPrefix(req.URL.Path, "/"),
		req.URL.Query().Get("service")); err != nil {
		return err
	}
	// A redirect must not carry the credential to a different host even when
	// that host is allowed: the browser aimed the credential at one of them.
	if len(via) > 0 && via[0].URL.Host != req.URL.Host {
		req.Header.Del("Authorization")
	}
	return nil
}

// dial resolves the target and connects to an address the policy allows.
//
// Resolution happens exactly once, here, and the connection is made to one of
// the addresses that were checked. A name whose answer changes between the
// check and the connection — the DNS rebinding attack — therefore reaches
// nothing: there is no second lookup to poison.
func (p *corsProxy) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("split %s: %w", addr, err)
	}
	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		ips, err = p.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: %w", host, errBlockedAddress)
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, ip := range ips {
		if !p.addressAllowed(ip) {
			lastErr = fmt.Errorf("%s: %w", ip, errBlockedAddress)
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err != nil {
			lastErr = fmt.Errorf("dial %s: %w", ip, err)
			continue
		}
		return conn, nil
	}
	if lastErr == nil {
		lastErr = errBlockedAddress
	}
	return nil, lastErr
}

// addressAllowed reports whether an IP is a public unicast address the proxy may
// reach. Everything the machine can see that the open internet cannot — the
// loopback interface, the LAN, the link-local metadata services of every cloud,
// the carrier-grade NAT range — is refused.
func (p *corsProxy) addressAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return p.allowLoopback
	}
	if ip.IsUnspecified() || ip.IsPrivate() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		// 100.64.0.0/10, carrier-grade NAT.
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return false
		// 192.0.0.0/24, IETF protocol assignments.
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
			return false
		// 198.18.0.0/15, benchmarking.
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
			return false
		// 240.0.0.0/4, reserved, and 255.255.255.255.
		case v4[0] >= 240:
			return false
		}
		return true
	}
	// IPv6 unique local addresses, fc00::/7. net.IP.IsPrivate covers them, but
	// being explicit keeps the rule readable next to the IPv4 ones.
	if len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc {
		return false
	}
	return true
}

// boundedReader forwards at most `remaining` bytes and then fails, so an
// oversized upload is refused mid-stream instead of being buffered whole.
type boundedReader struct {
	r         io.Reader
	remaining int64
}

// Read implements io.Reader.
func (b *boundedReader) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errRequestTooLarge
	}
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.r.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		return 0, errRequestTooLarge
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("read proxied request body: %w", err)
	}
	return n, err //nolint:wrapcheck // io.EOF must reach the transport unwrapped.
}

// copyHeaders copies the named headers from src to dst, and nothing else.
func copyHeaders(dst, src http.Header, names []string) {
	for _, name := range names {
		for _, value := range src.Values(name) {
			dst.Add(name, value)
		}
	}
}

// corsProxyInfo is what GET /api/v1/git/cors-proxy answers: where the proxy is,
// how to authenticate to it and which hosts it will speak to. The web app uses
// it to offer the companion's proxy as the zero-configuration default.
type corsProxyInfo struct {
	Enabled bool `json:"enabled"`
	// URL is the value to configure as the CORS proxy, absolute so that a tab
	// on another origin can use it too.
	URL string `json:"url,omitempty"`
	// TokenHeader is the header the companion's bearer token goes in.
	TokenHeader string `json:"tokenHeader,omitempty"`
	// AllowedHosts is the effective allow-list, sorted, for the settings UI.
	AllowedHosts []string `json:"allowedHosts"`
	// MaxRequestBytes and MaxResponseBytes are the bounds of one exchange.
	MaxRequestBytes  int64 `json:"maxRequestBytes"`
	MaxResponseBytes int64 `json:"maxResponseBytes"`
	// Reason explains a disabled proxy.
	Reason string `json:"reason,omitempty"`
}

// handleCORSProxyInfo serves GET /api/v1/git/cors-proxy.
func (s *Server) handleCORSProxyInfo(w http.ResponseWriter, r *http.Request) {
	p := s.proxy
	info := corsProxyInfo{
		Enabled:          p.enabled(),
		AllowedHosts:     []string{},
		MaxRequestBytes:  p.maxRequest,
		MaxResponseBytes: p.maxResponse,
	}
	if !info.Enabled {
		info.Reason = "The companion is configured with `git.corsProxy.enabled: false`."
		writeJSON(w, r, http.StatusOK, info)
		return
	}
	info.URL = s.URL() + corsProxyPath
	info.TokenHeader = proxyTokenHeader
	hosts, err := p.allowedHosts(r.Context())
	if err != nil {
		s.log.Warn("cors proxy: reading the repository remotes failed", "error", err)
	}
	for host := range hosts {
		info.AllowedHosts = append(info.AllowedHosts, host)
	}
	slices.Sort(info.AllowedHosts)
	writeJSON(w, r, http.StatusOK, info)
}
