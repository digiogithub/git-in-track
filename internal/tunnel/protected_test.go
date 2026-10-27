package tunnel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/cloudflare/cloudflared/connection"
)

// bodyRecordingBroker is a fakeBroker that also records every provisioning
// request body, so a test can tell a public request from a protected one.
// hostname is what it hands back; protected tunnels need a real-looking
// <label>.trycloudflare.com name or the authorizer refuses to build.
func bodyRecordingBroker(t *testing.T, hostname string) (*httptest.Server, func() []string) {
	t.Helper()
	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"success":true,"result":{
			"id":"3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"hostname":%q,
			"account_tag":"acct",
			"secret":"c2VjcmV0"
		}}`, hostname)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

func TestValidateAllowedEmails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rules   []string
		want    Recipients
		wantErr string
	}{
		{name: "an empty list is a public tunnel", rules: nil},
		{
			name:  "exact addresses and domain rules are counted apart",
			rules: []string{"ana@example.com", "bob@example.com", "*@corp.example"},
			want:  Recipients{Emails: 2, Domains: 1},
		},
		{
			name:  "one element may carry a comma-separated list",
			rules: []string{"ana@example.com,*@corp.example"},
			want:  Recipients{Emails: 1, Domains: 1},
		},
		{
			name:  "duplicates collapse after normalization",
			rules: []string{"Ana@Example.com", "ana@example.com"},
			want:  Recipients{Emails: 1},
		},
		{name: "an empty entry is rejected", rules: []string{"ana@example.com,"}, wantErr: "rule 2 is empty"},
		{name: "a non-address is rejected", rules: []string{"not-an-address"}, wantErr: "not a valid email address"},
		{name: "a bad wildcard domain is rejected", rules: []string{"*@"}, wantErr: "invalid wildcard domain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ValidateAllowedEmails(tt.rules)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateAllowedEmails: %v", err)
			}
			if got != tt.want {
				t.Fatalf("recipients = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestProvisionRequestBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		protected bool
		wantBody  string
	}{
		{name: "a public tunnel sends an empty body", protected: false, wantBody: ""},
		{name: "a protected tunnel asks for the OTP auth mode", protected: true, wantBody: `{"auth_mode":"otp"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broker, bodies := bodyRecordingBroker(t, "witty-fox.trycloudflare.com")
			if _, _, err := provision(t.Context(), broker.URL, "cloudflared/0.0.0-test", tt.protected); err != nil {
				t.Fatalf("provision: %v", err)
			}
			got := bodies()
			if len(got) != 1 || got[0] != tt.wantBody {
				t.Fatalf("bodies = %q, want exactly [%q]", got, tt.wantBody)
			}
		})
	}
}

func TestManagerProtection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		allowed        []string
		wantBody       string
		wantProtected  bool
		wantRecipients Recipients
	}{
		{name: "no allow-list opens a public tunnel", wantBody: ""},
		{
			name:           "an allow-list opens a protected tunnel",
			allowed:        []string{"ana@example.com", "bob@example.com", "*@corp.example"},
			wantBody:       `{"auth_mode":"otp"}`,
			wantProtected:  true,
			wantRecipients: Recipients{Emails: 2, Domains: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broker, bodies := bodyRecordingBroker(t, "witty-fox.trycloudflare.com")
			m := NewManager(Options{
				Origin:        "127.0.0.1:7317",
				Version:       "0.0.0-test",
				Logger:        discardLogger(),
				AllowedEmails: tt.allowed,
				endpoint:      broker.URL,
			})
			auths := make(chan connection.HTTPRequestAuthorizer, 1)
			m.runTunnel = func(ctx context.Context, _ connection.Credentials, _ string, auth connection.HTTPRequestAuthorizer) error {
				auths <- auth
				<-ctx.Done()
				return nil
			}

			want := Status{State: StateOff, Protected: tt.wantProtected, Recipients: tt.wantRecipients}
			if got := m.Status(); got != want {
				t.Fatalf("status before start = %+v, want %+v", got, want)
			}

			status, err := m.Start(t.Context())
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			auth := <-auths

			if status.Protected != tt.wantProtected || status.Recipients != tt.wantRecipients {
				t.Fatalf("status = %+v, want protected=%v recipients=%+v", status, tt.wantProtected, tt.wantRecipients)
			}
			if got := bodies(); len(got) != 1 || got[0] != tt.wantBody {
				t.Fatalf("broker bodies = %q, want [%q]", got, tt.wantBody)
			}
			if (auth != nil) != tt.wantProtected {
				t.Fatalf("authorizer = %v, want one only for a protected tunnel", auth)
			}
			if auth != nil {
				assertGuardsTheOrigin(t, auth, "witty-fox.trycloudflare.com")
			}

			if err := m.Stop(t.Context()); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			// The protection fields survive the reset to off: they describe
			// the manager, not the round.
			if got := m.Status(); got != want {
				t.Fatalf("status after stop = %+v, want %+v", got, want)
			}
		})
	}
}

// assertGuardsTheOrigin drives the authorizer the way cloudflared does for a
// visitor without a session: GET is sent to the Cloudflare Access login on a
// host other than the tunnel's, and a write is refused outright. Neither may
// be allowed through to ingress.
func assertGuardsTheOrigin(t *testing.T, auth connection.HTTPRequestAuthorizer, hostname string) {
	t.Helper()

	get := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+hostname+"/board", nil)
	rec := httptest.NewRecorder()
	decision, _, _ := auth.AuthorizeHTTP(rec, get)
	if decision == connection.HTTPRequestAuthorizationAllowed {
		t.Fatal("an unauthenticated GET was allowed through to the origin")
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("unauthenticated GET status = %d, want %d", rec.Code, http.StatusFound)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || location.Host == "" || location.Host == hostname {
		t.Fatalf("redirect Location = %q, want the Cloudflare Access login on another host", rec.Header().Get("Location"))
	}

	post := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://"+hostname+"/api/v1/items", nil)
	rec = httptest.NewRecorder()
	decision, _, _ = auth.AuthorizeHTTP(rec, post)
	if decision == connection.HTTPRequestAuthorizationAllowed || rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated POST: decision=%v status=%d, want handled with %d", decision, rec.Code, http.StatusUnauthorized)
	}
}

func TestManagerRefusesAnInvalidAllowList(t *testing.T) {
	t.Parallel()

	broker, bodies := bodyRecordingBroker(t, "witty-fox.trycloudflare.com")
	m := NewManager(Options{
		Origin:        "127.0.0.1:7317",
		Version:       "0.0.0-test",
		Logger:        discardLogger(),
		AllowedEmails: []string{"not-an-address"},
		endpoint:      broker.URL,
	})

	if _, err := m.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "not a valid email address") {
		t.Fatalf("Start: err = %v, want the allow-list rejected", err)
	}
	// Never falls back to a public tunnel: the broker was not even asked.
	if got := bodies(); len(got) != 0 {
		t.Fatalf("broker was called %d times, want none", len(got))
	}
	if got := m.Status(); !got.Protected || got.State != StateOff {
		t.Fatalf("status = %+v, want an off, protected manager", got)
	}
}

func TestManagerProtectedRefusesANonQuickTunnelHostname(t *testing.T) {
	t.Parallel()

	broker, _ := bodyRecordingBroker(t, "evil.example.com")
	m := NewManager(Options{
		Origin:        "127.0.0.1:7317",
		Version:       "0.0.0-test",
		Logger:        discardLogger(),
		AllowedEmails: []string{"ana@example.com"},
		endpoint:      broker.URL,
	})
	m.runTunnel = func(context.Context, connection.Credentials, string, connection.HTTPRequestAuthorizer) error {
		t.Error("the tunnel ran although its authorizer could not be built")
		return nil
	}

	if _, err := m.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "authentication state") {
		t.Fatalf("Start: err = %v, want the hostname rejected", err)
	}
	if got := m.Status(); got.State != StateError {
		t.Fatalf("state = %q, want %q", got.State, StateError)
	}
	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop after a failed start: %v", err)
	}
}
