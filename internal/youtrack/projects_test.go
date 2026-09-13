package youtrack

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// TestUserProjectAndFieldEndpoints is the table over the users, projects and
// custom-field endpoints: each case pins the path, the fields selector and the
// decoding of a recorded fixture.
func TestUserProjectAndFieldEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantPath  string
		wantQuery map[string]string
		call      func(t *testing.T, c *Client)
	}{
		{
			name:      "me",
			fixture:   "me.json",
			wantPath:  "/api/users/me",
			wantQuery: map[string]string{"fields": MeFields},
			call: func(t *testing.T, c *Client) {
				user, err := c.Me(context.Background())
				if err != nil {
					t.Fatalf("Me: %v", err)
				}
				if user.ID != "1-1" || user.Login != "jose" || user.FullName != "Jose F. Rives" {
					t.Fatalf("user = %+v", user)
				}
				if user.Email != "jose@example.com" || user.Type != "Me" {
					t.Fatalf("user = %+v", user)
				}
			},
		},
		{
			name:      "projects",
			fixture:   "projects.json",
			wantPath:  "/api/admin/projects",
			wantQuery: map[string]string{"fields": ProjectFields, "$top": "100", "$skip": "0", "query": "acme"},
			call: func(t *testing.T, c *Client) {
				projects, err := c.Projects(context.Background(), "acme", Page{})
				if err != nil {
					t.Fatalf("Projects: %v", err)
				}
				if len(projects) != 2 {
					t.Fatalf("got %d projects, want 2", len(projects))
				}
				if projects[0].ShortName != "ACME" || projects[0].ID != "0-1" || projects[0].Archived {
					t.Fatalf("project[0] = %+v", projects[0])
				}
				if !projects[1].Archived {
					t.Fatalf("project[1] should be archived: %+v", projects[1])
				}
			},
		},
		{
			name:      "one project",
			fixture:   "projects.json",
			wantPath:  "/api/admin/projects/ACME",
			wantQuery: map[string]string{"fields": ProjectFields},
			call: func(t *testing.T, c *Client) {
				// The fixture is an array, so only the request shape matters
				// here; decoding is covered by the list case.
				_, _ = c.Project(context.Background(), "ACME")
			},
		},
		{
			name:      "custom field settings",
			fixture:   "custom_field_settings.json",
			wantPath:  "/api/admin/projects/0-1/customFieldSettings",
			wantQuery: map[string]string{"fields": CustomFieldSettingsFields, "$top": "100"},
			call: func(t *testing.T, c *Client) {
				settings, err := c.CustomFieldSettings(context.Background(), "0-1")
				if err != nil {
					t.Fatalf("CustomFieldSettings: %v", err)
				}
				if len(settings) != 2 {
					t.Fatalf("got %d settings, want 2", len(settings))
				}
				if settings[0].Field.Name != "Type" || settings[0].Field.FieldType.ID != "enum[1]" {
					t.Fatalf("settings[0] = %+v", settings[0])
				}
				if settings[0].CanBeEmpty {
					t.Fatalf("settings[0] should not allow empty: %+v", settings[0])
				}
				if settings[1].Bundle.ID != "75-2" || settings[1].Bundle.Type != "VersionBundle" {
					t.Fatalf("settings[1].Bundle = %+v", settings[1].Bundle)
				}
			},
		},
		{
			name:      "version bundle values",
			fixture:   "version_bundle_values.json",
			wantPath:  "/api/admin/customFieldSettings/bundles/version/75-2/values",
			wantQuery: map[string]string{"fields": VersionValueFields, "$top": "100"},
			call: func(t *testing.T, c *Client) {
				versions, err := c.VersionBundleValues(context.Background(), "75-2")
				if err != nil {
					t.Fatalf("VersionBundleValues: %v", err)
				}
				if len(versions) != 2 {
					t.Fatalf("got %d versions, want 2", len(versions))
				}
				if versions[0].Name != "1.4.0" || !versions[0].Released {
					t.Fatalf("versions[0] = %+v", versions[0])
				}
				if got := versions[0].ReleaseDate.Time().Format("2006-01-02"); got != "2026-01-01" {
					t.Fatalf("release date = %s", got)
				}
				if !versions[1].ReleaseDate.IsZero() {
					t.Fatalf("a null releaseDate must decode as zero: %+v", versions[1])
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tc.fixture)
			})
			client := newTestClient(t, srv.URL, newFakeClock(), nil)
			tc.call(t, client)

			reqs := rec.all()
			if len(reqs) != 1 {
				t.Fatalf("got %d requests, want 1", len(reqs))
			}
			if reqs[0].URL.Path != tc.wantPath {
				t.Fatalf("path = %q, want %q", reqs[0].URL.Path, tc.wantPath)
			}
			query := reqs[0].URL.Query()
			for key, want := range tc.wantQuery {
				if got := query.Get(key); got != want {
					t.Errorf("query %s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

// TestMeNotFoundSuggestsContextPath pins the mapping that lets the settings UI
// tell a bad token apart from a base URL missing its context path.
func TestMeNotFoundSuggestsContextPath(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	_, err := client.Me(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) {
		t.Fatalf("a 404 must not look like an auth failure: %v", err)
	}
}

func TestEmptyIdentifiersFailLocally(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should have been made")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)
	ctx := context.Background()

	calls := map[string]func() error{
		"Project":             func() error { _, err := client.Project(ctx, ""); return err },
		"CustomFieldSettings": func() error { _, err := client.CustomFieldSettings(ctx, ""); return err },
		"VersionBundleValues": func() error { _, err := client.VersionBundleValues(ctx, ""); return err },
		"Issue":               func() error { _, err := client.Issue(ctx, ""); return err },
		"IssueLinks":          func() error { _, err := client.IssueLinks(ctx, ""); return err },
		"Comments":            func() error { _, err := client.Comments(ctx, "", Page{}); return err },
		"AddComment":          func() error { _, err := client.AddComment(ctx, "", "text"); return err },
		"Article":             func() error { _, err := client.Article(ctx, ""); return err },
		"ChildArticles":       func() error { _, err := client.ChildArticles(ctx, ""); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
		})
	}
	if rec.count() != 0 {
		t.Fatalf("%d requests were made, want none", rec.count())
	}
}
