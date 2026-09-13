package youtrack

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestNewValidatesOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"empty base URL", Options{Token: testToken}},
		{"no scheme", Options{BaseURL: "yt.example.com", Token: testToken}},
		{"unsupported scheme", Options{BaseURL: "ftp://yt.example.com", Token: testToken}},
		{"no host", Options{BaseURL: "https://", Token: testToken}},
		{"empty token", Options{BaseURL: "https://yt.example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.opts); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
		})
	}
}

// TestRequestURLKeepsContextPath pins the rule that a base URL with a context
// path must survive: the request has to land on /youtrack/api/users/me, which
// url.ResolveReference would have turned into /api/users/me.
func TestRequestURLKeepsContextPath(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "me.json")
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL+"/youtrack/", clock, nil)

	if got, want := client.BaseURL(), srv.URL+"/youtrack"; got != want {
		t.Fatalf("BaseURL = %q, want %q", got, want)
	}
	if _, err := client.Me(context.Background()); err != nil {
		t.Fatalf("Me: %v", err)
	}
	req := rec.all()[0]
	if req.URL.Path != "/youtrack/api/users/me" {
		t.Fatalf("path = %q, want /youtrack/api/users/me", req.URL.Path)
	}
	if got := req.URL.Query().Get("fields"); got != MeFields {
		t.Fatalf("fields = %q, want %q", got, MeFields)
	}
}

func TestRequestHeaders(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "comments.json")
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, nil)

	if _, err := client.Comments(context.Background(), "ACME-42", Page{}); err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if _, err := client.AddComment(context.Background(), "ACME-42", "hello"); err != nil {
		// The fixture is an array, so decoding the POST reply fails; the
		// headers are what this test cares about.
		t.Logf("AddComment decode: %v", err)
	}

	reqs := rec.all()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2", len(reqs))
	}
	for i, req := range reqs {
		if got, want := req.Header.Get("Authorization"), "Bearer "+testToken; got != want {
			t.Errorf("request %d Authorization = %q, want %q", i, got, want)
		}
		if got := req.Header.Get("Accept"); got != "application/json" {
			t.Errorf("request %d Accept = %q", i, got)
		}
	}
	if got := reqs[0].Header.Get("Content-Type"); got != "" {
		t.Errorf("GET carries Content-Type %q, want none", got)
	}
	if got := reqs[1].Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("POST Content-Type = %q, want application/json", got)
	}
}

func TestStatusesMapToTypedErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized},
		{"forbidden", http.StatusForbidden, ErrForbidden},
		{"not found", http.StatusNotFound, ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "denied", tc.status)
			})
			client := newTestClient(t, srv.URL, newFakeClock(), nil)

			_, err := client.Me(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error is not an *APIError: %v", err)
			}
			if apiErr.Status != tc.status {
				t.Fatalf("APIError.Status = %d, want %d", apiErr.Status, tc.status)
			}
			if apiErr.Path != "/api/users/me" {
				t.Fatalf("APIError.Path = %q", apiErr.Path)
			}
		})
	}
}

// TestOtherStatusIsPlainAPIError proves a status without a sentinel still
// arrives as a typed APIError rather than an opaque string.
func TestOtherStatusIsPlainAPIError(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad query", http.StatusBadRequest)
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	_, err := client.Me(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("want a 400 APIError, got %v", err)
	}
	for _, sentinel := range []error{ErrUnauthorized, ErrForbidden, ErrNotFound, ErrRateLimited} {
		if errors.Is(err, sentinel) {
			t.Fatalf("400 must not unwrap to %v", sentinel)
		}
	}
}

// TestTokenNeverLeaks asserts the token is absent from every rendering the
// package can produce: errors, String output and echoed response bodies.
func TestTokenNeverLeaks(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// A hostile or careless instance echoing the credential back.
		http.Error(w, "rejected credential "+r.Header.Get("Authorization"), http.StatusUnauthorized)
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, nil)

	_, err := client.Me(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}

	renderings := []string{
		err.Error(),
		client.String(),
		Options{BaseURL: srv.URL, Token: testToken}.String(),
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		renderings = append(renderings, apiErr.Body, apiErr.Path)
	}
	secret := testToken[strings.LastIndex(testToken, ".")+1:]
	for i, text := range renderings {
		if strings.Contains(text, testToken) {
			t.Errorf("rendering %d contains the token: %s", i, text)
		}
		if strings.Contains(text, secret) {
			t.Errorf("rendering %d contains the token secret: %s", i, text)
		}
	}
	if !strings.Contains(apiErr.Body, "[redacted]") {
		t.Errorf("the echoed credential was not replaced: %q", apiErr.Body)
	}
}

func TestRedactBearer(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Bearer perm:abc.def", "Bearer [redacted]"},
		{"quoted", `header "Bearer perm:abc" rejected`, `header "Bearer [redacted]" rejected`},
		{"twice", "Bearer a and Bearer b", "Bearer [redacted] and Bearer [redacted]"},
		{"absent", "nothing here", "nothing here"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactBearer(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
