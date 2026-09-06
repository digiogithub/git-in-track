package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
)

// proxyOrigin is the loopback origin the companion trusts by default.
const proxyOrigin = "http://127.0.0.1:7317"

// newProxyServer builds a companion whose CORS proxy is enabled, allowed to
// speak to `hosts`, and — because an httptest upstream necessarily listens on
// loopback — allowed to dial loopback. Production never sets that last flag;
// TestCORSProxyRefusesPrivateTargets exercises the policy with it off.
func newProxyServer(t *testing.T, hosts ...string) *Server {
	t.Helper()

	s, err := New(Options{
		Token:   "test-token",
		Version: "0.0.1-test",
		Git: config.Git{CORSProxy: config.CORSProxy{
			Enabled:      true,
			AllowedHosts: hosts,
		}},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	s.proxy.allowLoopback = true
	return s
}

// trustUpstream makes the proxy's client accept the httptest server's
// self-signed certificate without loosening anything else about the transport.
func trustUpstream(t *testing.T, s *Server, upstream *httptest.Server) {
	t.Helper()

	transport, ok := s.proxy.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("proxy transport is %T", s.proxy.client.Transport)
	}
	certs := upstream.Client().Transport.(*http.Transport).TLSClientConfig
	transport.TLSClientConfig = &tls.Config{RootCAs: certs.RootCAs, MinVersion: tls.VersionTLS12}
}

// proxyRequest issues one request at the proxy with the given headers and body.
func proxyRequest(t *testing.T, s *Server, method, target string, header map[string]string, body io.Reader) *http.Response {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, body)
	req.Host = "127.0.0.1:7317"
	req.Header.Set("Origin", proxyOrigin)
	req.Header.Set(proxyTokenHeader, "test-token")
	for k, v := range header {
		if v == "" {
			req.Header.Del(k)
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Result()
}

// problemCode reads the machine-readable code of a problem document.
func problemCode(t *testing.T, resp *http.Response) string {
	t.Helper()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	// The body is read once and handed back, so a caller may assert on it too.
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	var doc struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode problem document %q: %v", raw, err)
	}
	return doc.Code
}

// decodeJSON decodes a JSON response body into dst.
func decodeJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// TestCORSProxyRefusals is the security surface of GIT-US-0042: one sub-test per
// way in, each of which must be refused before anything is forwarded.
func TestCORSProxyRefusals(t *testing.T) {
	t.Parallel()

	// The upstream records what actually reached it, so that a refusal can be
	// asserted as "no request was made" and not merely as a status code.
	var reached int
	var gotHeader http.Header
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		gotHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.Header().Set("Set-Cookie", "session=leaked")
		_, _ = io.WriteString(w, "001e# service=git-upload-pack\n")
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "https://")
	refsPath := "/cors-proxy/" + host + "/acme/web.git/info/refs?service=git-upload-pack"

	cases := []struct {
		name    string
		method  string
		target  string
		header  map[string]string
		body    io.Reader
		status  int
		code    string
		forward bool
	}{
		{
			name:    "an allowed host is forwarded",
			method:  http.MethodGet,
			target:  refsPath,
			status:  http.StatusOK,
			forward: true,
		},
		{
			name:   "a host outside the allow-list is refused",
			method: http.MethodGet,
			target: "/cors-proxy/evil.example.test/acme/web.git/info/refs?service=git-upload-pack",
			status: http.StatusForbidden,
			code:   codeProxyHostDenied,
		},
		{
			name:   "a caller with no token is refused",
			method: http.MethodGet,
			target: refsPath,
			header: map[string]string{proxyTokenHeader: ""},
			status: http.StatusUnauthorized,
			code:   codeUnauthorized,
		},
		{
			name:   "a caller with the wrong token is refused",
			method: http.MethodGet,
			target: refsPath,
			header: map[string]string{proxyTokenHeader: "not-the-token"},
			status: http.StatusUnauthorized,
			code:   codeUnauthorized,
		},
		{
			name:   "the token is not accepted in Authorization",
			method: http.MethodGet,
			target: refsPath,
			header: map[string]string{proxyTokenHeader: "", "Authorization": "Bearer test-token"},
			status: http.StatusUnauthorized,
			code:   codeUnauthorized,
		},
		{
			name:   "a foreign origin is refused",
			method: http.MethodGet,
			target: refsPath,
			header: map[string]string{"Origin": "https://evil.example"},
			status: http.StatusForbidden,
			code:   codeProxyForbidden,
		},
		{
			name:   "a request with no origin at all is refused",
			method: http.MethodGet,
			target: refsPath,
			header: map[string]string{"Origin": ""},
			status: http.StatusForbidden,
			code:   codeProxyForbidden,
		},
		{
			name:   "a path outside the git surface is refused",
			method: http.MethodGet,
			target: "/cors-proxy/" + host + "/latest/meta-data/iam/",
			status: http.StatusForbidden,
			code:   codeProxyBadTarget,
		},
		{
			name:   "info/refs without a smart service is refused",
			method: http.MethodGet,
			target: "/cors-proxy/" + host + "/acme/web.git/info/refs",
			status: http.StatusForbidden,
			code:   codeProxyBadTarget,
		},
		{
			name:   "a relative segment in the repository path is refused",
			method: http.MethodGet,
			target: "/cors-proxy/" + host + "/acme/../info/refs?service=git-upload-pack",
			status: http.StatusBadRequest,
			code:   codeProxyBadTarget,
		},
		{
			name:   "the pack endpoint refuses a GET",
			method: http.MethodGet,
			target: "/cors-proxy/" + host + "/acme/web.git/git-upload-pack",
			status: http.StatusMethodNotAllowed,
			code:   codeProxyBadTarget,
		},
		{
			name:   "an oversized request body is refused",
			method: http.MethodPost,
			target: "/cors-proxy/" + host + "/acme/web.git/git-upload-pack",
			header: map[string]string{"Content-Type": "application/x-git-upload-pack-request"},
			body:   strings.NewReader(strings.Repeat("x", 4096)),
			status: http.StatusRequestEntityTooLarge,
			code:   codeProxyTooLarge,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newProxyServer(t, host)
			trustUpstream(t, s, upstream)
			// Small enough that the oversized-body case does not have to move
			// megabytes through the recorder.
			s.proxy.maxRequest = 1024

			reached, gotHeader = 0, nil
			resp := proxyRequest(t, s, tc.method, tc.target, tc.header, tc.body)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if tc.code != "" && problemCode(t, resp) != tc.code {
				t.Errorf("code = %q, want %q", problemCode(t, resp), tc.code)
			}
			if tc.forward && reached == 0 {
				t.Error("the request never reached the upstream")
			}
			if !tc.forward && reached != 0 {
				t.Errorf("a refused request reached the upstream %d times", reached)
			}
			if tc.forward {
				if got := resp.Header.Get("Set-Cookie"); got != "" {
					t.Errorf("the upstream Set-Cookie leaked back: %q", got)
				}
				if got := gotHeader.Get(proxyTokenHeader); got != "" {
					t.Errorf("the companion token was forwarded upstream: %q", got)
				}
				if got := gotHeader.Get("Origin"); got != "" {
					t.Errorf("the browser origin was forwarded upstream: %q", got)
				}
			}
		})
	}
}

// TestCORSProxyForwardsTheGitCredential proves the direction of the two
// credentials: the browser's git token goes up, the companion's token does not.
func TestCORSProxyForwardsTheGitCredential(t *testing.T) {
	t.Parallel()

	var seen http.Header
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "https://")
	s := newProxyServer(t, host)
	trustUpstream(t, s, upstream)

	resp := proxyRequest(t, s, http.MethodGet,
		"/cors-proxy/"+host+"/acme/web.git/info/refs?service=git-upload-pack",
		map[string]string{"Authorization": "Basic dXNlcjp0b2tlbg==", "Git-Protocol": "version=2"}, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := seen.Get("Authorization"); got != "Basic dXNlcjp0b2tlbg==" {
		t.Errorf("Authorization = %q, want the git credential forwarded", got)
	}
	if got := seen.Get("Git-Protocol"); got != "version=2" {
		t.Errorf("Git-Protocol = %q", got)
	}
	if got := seen.Get(proxyTokenHeader); got != "" {
		t.Errorf("the companion token reached the git host: %q", got)
	}
	if got := seen.Get("User-Agent"); got != proxyUserAgent {
		t.Errorf("User-Agent = %q, want the proxy's own", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != proxyOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, proxyOrigin)
	}
}

// TestCORSProxyRefusesPrivateTargets covers the SSRF core: a target that
// resolves into a range the open internet cannot reach is never dialed, whether
// it is spelled as a literal or reached through a name that resolves there.
func TestCORSProxyRefusesPrivateTargets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// host is the allow-listed host the request names.
		host string
		// resolved is what the name resolves to; nil means "resolve normally".
		resolved []net.IP
	}{
		{name: "a loopback literal", host: "127.0.0.1:9418"},
		{name: "a private literal", host: "10.1.2.3:443"},
		{name: "the cloud metadata address", host: "169.254.169.254:443"},
		{name: "an IPv6 unique local literal", host: "[fd00::1]:443"},
		{
			name:     "a public name that resolves to loopback",
			host:     "git.rebind.test:443",
			resolved: []net.IP{net.ParseIP("127.0.0.1")},
		},
		{
			name:     "a public name that resolves into the LAN",
			host:     "git.rebind.test:443",
			resolved: []net.IP{net.ParseIP("192.168.1.10")},
		},
		{
			name:     "a public name that resolves to the metadata service",
			host:     "git.rebind.test:443",
			resolved: []net.IP{net.ParseIP("169.254.169.254")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newProxyServer(t, tc.host)
			// Production never allows loopback; this is the policy as shipped.
			s.proxy.allowLoopback = false
			if tc.resolved != nil {
				s.proxy.resolve = func(context.Context, string) ([]net.IP, error) { return tc.resolved, nil }
			}

			resp := proxyRequest(t, s, http.MethodGet,
				"/cors-proxy/"+tc.host+"/acme/web.git/info/refs?service=git-upload-pack", nil, nil)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
			if got := problemCode(t, resp); got != codeProxyBlocked {
				t.Errorf("code = %q, want %q", got, codeProxyBlocked)
			}
		})
	}
}

// TestCORSProxyRedirects checks that a hop is re-validated: a redirect into a
// host the allow-list does not name is refused rather than followed.
func TestCORSProxyRedirects(t *testing.T) {
	t.Parallel()

	var target *httptest.Server
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/acme/web.git/info/refs?service=git-upload-pack", http.StatusFound)
	}))
	defer redirector.Close()

	var followed int
	target = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed++
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	from := strings.TrimPrefix(redirector.URL, "https://")
	to := strings.TrimPrefix(target.URL, "https://")

	t.Run("a redirect to a disallowed host is refused", func(t *testing.T) {
		s := newProxyServer(t, from)
		trustUpstream(t, s, redirector)
		followed = 0

		resp := proxyRequest(t, s, http.MethodGet,
			"/cors-proxy/"+from+"/acme/web.git/info/refs?service=git-upload-pack", nil, nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
		if got := problemCode(t, resp); got != codeProxyHostDenied {
			t.Errorf("code = %q, want %q", got, codeProxyHostDenied)
		}
		if followed != 0 {
			t.Error("the redirect was followed into a host outside the allow-list")
		}
	})

	t.Run("a redirect to an allowed host is followed", func(t *testing.T) {
		s := newProxyServer(t, from, to)
		trustUpstream(t, s, redirector)
		followed = 0

		resp := proxyRequest(t, s, http.MethodGet,
			"/cors-proxy/"+from+"/acme/web.git/info/refs?service=git-upload-pack", nil, nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if followed != 1 {
			t.Errorf("the allowed redirect was followed %d times, want 1", followed)
		}
	})
}

// TestCORSProxyDisabled proves the endpoint answers with its own problem code
// rather than the single-page application when it is switched off.
func TestCORSProxyDisabled(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, nil)
	resp := proxyRequest(t, s, http.MethodGet,
		"/cors-proxy/git.example.test/acme/web.git/info/refs?service=git-upload-pack", nil, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
	if got := problemCode(t, resp); got != codeProxyDisabled {
		t.Errorf("code = %q, want %q", got, codeProxyDisabled)
	}
}

// TestCORSProxyInfo covers the discovery route the web app adopts the proxy
// from.
func TestCORSProxyInfo(t *testing.T) {
	t.Parallel()

	s := newProxyServer(t, "git.example.test", "GIT.OTHER.TEST:8443")
	resp := do(t, s, http.MethodGet, "/api/v1/git/cors-proxy",
		map[string]string{"Authorization": "Bearer test-token"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var info corsProxyInfo
	decodeJSON(t, resp, &info)
	if !info.Enabled {
		t.Fatal("the proxy reports itself disabled")
	}
	if !strings.HasSuffix(info.URL, corsProxyPath) {
		t.Errorf("url = %q, want it to end in %q", info.URL, corsProxyPath)
	}
	if info.TokenHeader != proxyTokenHeader {
		t.Errorf("tokenHeader = %q", info.TokenHeader)
	}
	want := []string{"git.example.test:443", "git.other.test:8443"}
	if fmt.Sprint(info.AllowedHosts) != fmt.Sprint(want) {
		t.Errorf("allowedHosts = %v, want %v", info.AllowedHosts, want)
	}
}

// TestHostOfRemoteURL covers the allow-list derivation, which is what makes the
// proxy work with no configuration and still refuse every unrelated host.
func TestHostOfRemoteURL(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{name: "https", in: "https://github.com/acme/web.git", want: "github.com:443"},
		{name: "https with a port", in: "https://git.acme.test:8443/web.git", want: "git.acme.test:8443"},
		{name: "https with a credential", in: "https://user:pw@github.com/acme/web.git", want: "github.com:443"},
		{name: "scp-like ssh", in: "git@github.com:acme/web.git", want: "github.com:443"},
		{name: "ssh url", in: "ssh://git@git.acme.test:2222/web.git", want: "git.acme.test:2222"},
		{name: "a local path", in: "/srv/git/web.git", want: ""},
		{name: "a file url", in: "file:///srv/git/web.git", want: ""},
		{name: "empty", in: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostOfRemoteURL(tc.in); got != tc.want {
				t.Errorf("hostOfRemoteURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
