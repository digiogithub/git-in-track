package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// POST /api/v1/youtrack/import and /import/preview.

// importJobBody is the documented shape of a queued import.
type importJobBody struct {
	JobID      string `json:"jobId"`
	ProjectKey string `json:"projectKey"`
	Repo       string `json:"repo"`
	Queued     bool   `json:"queued"`
}

// importPreviewBody is the documented shape of a preview.
type importPreviewBody struct {
	Project string `json:"project"`
	Issues  []struct {
		YouTrackID string `json:"youtrackId"`
		Title      string `json:"title"`
		MappedType string `json:"mappedType"`
		Action     string `json:"action"`
		TargetID   string `json:"targetId"`
	} `json:"issues"`
}

// newImportAPIServer mounts the fixture linked to a fake instance installed
// through the job-client seam, which is what both import routes resolve their
// client through.
func newImportAPIServer(t *testing.T, fake *fakeYouTrack, stubURL string) *Server {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	link := &config.YouTrackLink{URL: stubURL, Project: "ACME"}
	if _, err := config.SaveYouTrackLink(ytProjectYAML(root), *link); err != nil {
		t.Fatalf("write the link: %v", err)
	}
	cfg := config.Default()
	cfg.SetYouTrackToken(ytProjectKey, ytToken)
	s, err := New(Options{
		Token:      "test-token",
		Version:    "0.0.1-test",
		Workspace:  "test",
		Repos:      []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		YouTrack:   cfg.YouTrackTokens(),
		Now:        func() time.Time { return jobClock },
		SyncEngine: SyncEngine{Debounce: -1},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if fake != nil {
		s.youtrack.mu.Lock()
		s.youtrack.jobClient = func(string) (youtrackJobClient, vault.YouTrackLink, error) {
			return fake, vault.YouTrackLink{BaseURL: stubURL, Project: "ACME"}, nil
		}
		s.youtrack.mu.Unlock()
	}
	return s
}

// TestImportPreviewIsSynchronous covers the read half: the plan comes back in
// the response, and nothing is written.
func TestImportPreviewIsSynchronous(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	fake := newFakeYouTrack()
	s := newImportAPIServer(t, fake, stub.server.URL)

	var body importPreviewBody
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/import/preview",
		body:   map[string]any{"ids": []string{"ACME-1", "ACME-2"}, "depth": 0},
	}), http.StatusOK, &body)

	if len(body.Issues) != 2 {
		t.Fatalf("the preview planned %d issues, want 2: %+v", len(body.Issues), body)
	}
	for _, issue := range body.Issues {
		if issue.Action != "create" {
			t.Errorf("issue %s = %q, want create against an empty backlog", issue.YouTrackID, issue.Action)
		}
	}
}

// TestImportQueuesAJob covers the write half: the run answers with a job id and
// does the work on the engine rather than on the request.
func TestImportQueuesAJob(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	fake := newFakeYouTrack()
	s := newImportAPIServer(t, fake, stub.server.URL)

	var body importJobBody
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/import",
		body:   map[string]any{"query": "#Unresolved", "includeComments": true},
	}), http.StatusAccepted, &body)

	if body.JobID == "" || !body.Queued {
		t.Fatalf("the import was not queued: %+v", body)
	}
	if body.ProjectKey != ytProjectKey || body.Repo != testRepoID {
		t.Errorf("the answer names %q/%q", body.ProjectKey, body.Repo)
	}
	job, ok := s.sync.engine.Job(body.JobID)
	if !ok {
		t.Fatalf("job %s is not in the queue", body.JobID)
	}
	if string(job.Kind) != "youtrack.import" {
		t.Errorf("kind = %q, want youtrack.import", job.Kind)
	}
	if !strings.Contains(string(job.Payload), "#Unresolved") {
		t.Errorf("the payload lost the query: %s", job.Payload)
	}
	if strings.Contains(string(job.Payload), ytToken) {
		t.Fatalf("the token reached the journalled payload: %s", job.Payload)
	}
}

// TestImportRoutesNeedAConnection is the gating both routes share.
func TestImportRoutesNeedAConnection(t *testing.T) {
	t.Parallel()

	s, _, _ := newYouTrackServer(t, nil, "", false)
	for _, target := range []string{"/api/v1/youtrack/import", "/api/v1/youtrack/import/preview"} {
		rec := send(t, s, request{method: http.MethodPost, target: target, body: map[string]any{"ids": []string{"A-1"}}})
		if rec.Code == http.StatusOK || rec.Code == http.StatusAccepted {
			t.Errorf("POST %s answered %d on an unconfigured project", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), ytToken) {
			t.Errorf("POST %s echoed the token", target)
		}
	}
}

// TestImportRoutesRequireTheBearerToken keeps both routes inside the
// authenticated group.
func TestImportRoutesRequireTheBearerToken(t *testing.T) {
	t.Parallel()

	s, _, _ := newYouTrackServer(t, nil, "", false)
	for _, target := range []string{"/api/v1/youtrack/import", "/api/v1/youtrack/import/preview"} {
		resp := do(t, s, http.MethodPost, target, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("POST %s without a token = %d, want 401", target, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestImportRefusesABadRequest proves a parameter the vault refuses is a
// field-level 400 rather than a queued job that fails later.
func TestImportRefusesABadRequest(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	s := newImportAPIServer(t, newFakeYouTrack(), stub.server.URL)

	rec := send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/import/preview",
		body:   map[string]any{"query": "#Unresolved", "ids": []string{"ACME-1"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
