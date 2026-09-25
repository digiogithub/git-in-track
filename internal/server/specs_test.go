package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/watcher"
)

// specsBase is the spec subtree of the fixture project.
const specsBase = "/api/v1/projects/DEMO/specs"

// requirementBody is one requirement as the routes answer it.
type requirementBody struct {
	Ref      string `json:"ref"`
	Spec     string `json:"spec"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Text     string `json:"text"`
	Rev      string `json:"rev"`
	BlockRev string `json:"blockRev"`
}

// requirementResult is the body of GET, POST and PATCH on a requirement.
type requirementResult struct {
	Requirement requirementBody `json:"requirement"`
	SpecRev     string          `json:"specRev"`
	Writes      any             `json:"writes"`
}

// specServer mounts the fixture with DEMO-SP-0001 in it.
func specServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, root := newAPIServer(t)
	withSpec(t, s, root)
	return s, root
}

// itemChangedIDs returns the ids of the item.changed events published after seq.
func itemChangedIDs(t *testing.T, s *Server, seq uint64) []string {
	t.Helper()
	events, ok := s.hub.since(seq)
	if !ok {
		t.Fatal("the event ring lost its position")
	}
	var ids []string
	for _, ev := range events {
		if data, ok := ev.Data.(itemChangedData); ok && ev.Type == eventItemChanged {
			ids = append(ids, data.ID)
		}
	}
	return ids
}

func TestSpecRoutesRequireTheToken(t *testing.T) {
	t.Parallel()
	s, _ := specServer(t)
	for _, target := range []string{
		specsBase,
		specsBase + "/DEMO-SP-0001",
		specsBase + "/DEMO-SP-0001/requirements/R1",
		specsBase + "/coverage",
		specsBase + "/impact?base=HEAD",
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var doc problemBody
		decode(t, rec, http.StatusUnauthorized, &doc)
		if doc.Code != "unauthorized" {
			t.Errorf("%s: code = %q", target, doc.Code)
		}
	}
}

func TestSpecListAndGet(t *testing.T) {
	t.Parallel()
	s, _ := specServer(t)

	t.Run("the list holds only specs", func(t *testing.T) {
		rec := send(t, s, request{method: http.MethodGet, target: specsBase})
		var page itemPageBody
		decode(t, rec, http.StatusOK, &page)
		if len(page.Items) != 1 || page.Items[0].ID != "DEMO-SP-0001" || page.Items[0].Type != "spec" {
			t.Errorf("items = %+v, want DEMO-SP-0001 alone", page.Items)
		}
		if got := rec.Header().Get("X-Total-Count"); got != "1" {
			t.Errorf("X-Total-Count = %q", got)
		}
	})

	t.Run("get carries the rev as the ETag", func(t *testing.T) {
		rec := send(t, s, request{method: http.MethodGet, target: specsBase + "/DEMO-SP-0001"})
		var item struct {
			ID  string `json:"id"`
			Rev string `json:"rev"`
		}
		decode(t, rec, http.StatusOK, &item)
		if item.ID != "DEMO-SP-0001" || rec.Header().Get("ETag") != `"`+item.Rev+`"` {
			t.Errorf("item = %+v, ETag = %q", item, rec.Header().Get("ETag"))
		}
	})

	tests := []struct {
		name   string
		target string
		status int
		code   string
	}{
		{"an unknown project", "/api/v1/projects/NOPE/specs", http.StatusNotFound, "not_found"},
		{"a spec of another project", specsBase + "/ACME-SP-0001", http.StatusNotFound, "not_found"},
		{"an id that is not a spec", specsBase + "/DEMO-US-0001", http.StatusBadRequest, "invalid_request"},
		{"an unknown spec", specsBase + "/DEMO-SP-0042", http.StatusNotFound, "not_found"},
		{"a requirement of another spec", specsBase + "/DEMO-SP-0001/requirements/DEMO-SP-0002.R1",
			http.StatusBadRequest, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc problemBody
			decode(t, send(t, s, request{method: http.MethodGet, target: tt.target}), tt.status, &doc)
			if doc.Code != tt.code {
				t.Errorf("code = %q, want %q", doc.Code, tt.code)
			}
		})
	}
}

func TestRequirementReads(t *testing.T) {
	t.Parallel()
	s, _ := specServer(t)

	for _, target := range []string{specsBase + "/requirements", specsBase + "/DEMO-SP-0001/requirements"} {
		rec := send(t, s, request{method: http.MethodGet, target: target})
		var list struct {
			Requirements []requirementBody `json:"requirements"`
			Total        int               `json:"total"`
		}
		decode(t, rec, http.StatusOK, &list)
		if list.Total != 2 || len(list.Requirements) != 2 || list.Requirements[0].Ref != "DEMO-SP-0001.R1" {
			t.Errorf("%s = %+v", target, list)
		}
		if list.Requirements[0].Text != "" {
			t.Errorf("%s: a list row carries its text without text=true", target)
		}
		if rec.Header().Get("X-Total-Count") != "2" {
			t.Errorf("%s: X-Total-Count = %q", target, rec.Header().Get("X-Total-Count"))
		}
	}

	var withText struct {
		Requirements []requirementBody `json:"requirements"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: specsBase + "/requirements?text=true&q=empty"}),
		http.StatusOK, &withText)
	if len(withText.Requirements) != 1 || !strings.Contains(withText.Requirements[0].Text, "SHALL refuse") {
		t.Errorf("filtered list with text = %+v", withText.Requirements)
	}

	for _, req := range []string{"R1", "DEMO-SP-0001.R1"} {
		rec := send(t, s, request{method: http.MethodGet, target: specsBase + "/DEMO-SP-0001/requirements/" + req})
		var got requirementResult
		decode(t, rec, http.StatusOK, &got)
		if got.Requirement.Ref != "DEMO-SP-0001.R1" || got.SpecRev == "" {
			t.Errorf("%s = %+v", req, got)
		}
		if rec.Header().Get("ETag") != `"`+got.Requirement.Rev+`"` {
			t.Errorf("%s: ETag = %q, want the requirement rev", req, rec.Header().Get("ETag"))
		}
	}

	var missing problemBody
	decode(t, send(t, s, request{method: http.MethodGet, target: specsBase + "/DEMO-SP-0001/requirements/R9"}),
		http.StatusNotFound, &missing)
}

func TestRequirementWrites(t *testing.T) {
	t.Parallel()
	s, root := specServer(t)
	target := specsBase + "/DEMO-SP-0001/requirements"

	seq := s.hub.lastSeq()
	rec := send(t, s, request{method: http.MethodPost, target: target,
		body: map[string]any{"title": "Keep the postcode", "text": "The checkout SHALL keep the postcode."}})
	var created requirementResult
	decode(t, rec, http.StatusCreated, &created)
	if created.Requirement.Ref != "DEMO-SP-0001.R3" || created.Writes != nil {
		t.Errorf("created = %+v, want R3 and no writes", created)
	}
	if loc := rec.Header().Get("Location"); loc != target+"/DEMO-SP-0001.R3" {
		t.Errorf("Location = %q", loc)
	}
	if ids := itemChangedIDs(t, s, seq); len(ids) != 1 || ids[0] != "DEMO-SP-0001" {
		t.Errorf("item.changed after a create = %v, want the spec", ids)
	}
	matches, _ := globSpec(root)
	data, err := os.ReadFile(matches)
	if err != nil || !strings.Contains(string(data), "### DEMO-SP-0001.R3 — Keep the postcode") {
		t.Fatalf("the block is not on disk: %v\n%s", err, data)
	}

	var current requirementResult
	decode(t, send(t, s, request{method: http.MethodGet, target: target + "/R1"}), http.StatusOK, &current)

	t.Run("a write without If-Match is refused", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodPatch, target: target + "/R1",
			body: map[string]any{"title": "Trim"}}), http.StatusPreconditionRequired, &doc)
		if doc.Code != "precondition_required" {
			t.Errorf("code = %q", doc.Code)
		}
	})

	t.Run("a stale rev is a 412 with the current rev", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodPatch, target: target + "/R1",
			header: map[string]string{"If-Match": `"sha256:0000"`},
			body:   map[string]any{"title": "Trim"}}), http.StatusPreconditionFailed, &doc)
		if doc.Code != "stale_revision" || doc.CurrentRev != current.Requirement.Rev {
			t.Errorf("problem = %+v", doc)
		}
	})

	t.Run("the requirement rev updates only that block", func(t *testing.T) {
		seq := s.hub.lastSeq()
		rec := send(t, s, request{method: http.MethodPatch, target: target + "/R1",
			header: map[string]string{"If-Match": current.Requirement.Rev},
			body:   map[string]any{"patch": map[string]any{"title": "Trim pasted input"}}})
		var got requirementResult
		decode(t, rec, http.StatusOK, &got)
		if got.Requirement.Title != "Trim pasted input" || got.Requirement.Rev == current.Requirement.Rev {
			t.Errorf("updated = %+v", got.Requirement)
		}
		if rec.Header().Get("ETag") != `"`+got.Requirement.Rev+`"` {
			t.Errorf("ETag = %q", rec.Header().Get("ETag"))
		}
		if ids := itemChangedIDs(t, s, seq); len(ids) != 1 || ids[0] != "DEMO-SP-0001" {
			t.Errorf("item.changed after an update = %v", ids)
		}
		// The other requirement's rev did not move.
		var r2 requirementResult
		decode(t, send(t, s, request{method: http.MethodGet, target: target + "/R2"}), http.StatusOK, &r2)
		var after requirementResult
		decode(t, send(t, s, request{method: http.MethodPatch, target: target + "/R2",
			header: map[string]string{"If-Match": r2.Requirement.Rev},
			body:   map[string]any{"status": "todo"}}), http.StatusOK, &after)
		if after.Requirement.Status != "todo" {
			t.Errorf("flat patch: status = %q", after.Requirement.Status)
		}
	})

	t.Run("If-Match: * waives the lock", func(t *testing.T) {
		var got requirementResult
		decode(t, send(t, s, request{method: http.MethodPatch, target: target + "/R1",
			header: map[string]string{"If-Match": "*"},
			body:   map[string]any{"title": "Trim input"}}), http.StatusOK, &got)
		if got.Requirement.Title != "Trim input" {
			t.Errorf("title = %q", got.Requirement.Title)
		}
	})
}

// globSpec returns the path of DEMO-SP-0001 on disk.
func globSpec(root string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "docs", ".pmngr", "specs", "DEMO-SP-0001-*.md"))
	if err != nil || len(matches) == 0 {
		return "", err
	}
	return matches[0], nil
}

func TestSpecTraceAndCoverage(t *testing.T) {
	t.Parallel()
	s, _ := specServer(t)

	var trace struct {
		Trace struct {
			Ref  string `json:"ref"`
			Code []any  `json:"code"`
		} `json:"trace"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: specsBase + "/DEMO-SP-0001/requirements/R2/trace"}),
		http.StatusOK, &trace)
	if trace.Trace.Ref != "DEMO-SP-0001.R2" {
		t.Errorf("trace = %+v", trace)
	}

	type coverage struct {
		Coverage []struct {
			Ref    string `json:"ref"`
			Status string `json:"status"`
		} `json:"coverage"`
		Total int `json:"total"`
	}
	for target, want := range map[string]int{
		specsBase + "/coverage":                               2,
		specsBase + "/DEMO-SP-0001/coverage":                  2,
		specsBase + "/coverage?ref=DEMO-SP-0001.R2":           1,
		specsBase + "/coverage?status=untested,passing":       2,
		specsBase + "/coverage?status=failing&status=suspect": 0,
	} {
		var got coverage
		decode(t, send(t, s, request{method: http.MethodGet, target: target}), http.StatusOK, &got)
		if got.Total != want {
			t.Errorf("%s: total = %d, want %d", target, got.Total, want)
		}
		for _, row := range got.Coverage {
			if row.Status != "untested" {
				t.Errorf("%s: %s is %s, want untested without results", target, row.Ref, row.Status)
			}
		}
	}

	var doc problemBody
	decode(t, send(t, s, request{method: http.MethodGet, target: specsBase + "/coverage?status=green"}),
		http.StatusBadRequest, &doc)
}

func TestSpecTraceAndCoverageUnavailable(t *testing.T) {
	t.Parallel()
	s, _ := specServer(t)
	for _, m := range s.repos.ready() {
		m.vlt.SetRequirementTracer(nil)
		m.vlt.SetRequirementCoverage(nil)
	}
	for _, target := range []string{
		specsBase + "/coverage",
		specsBase + "/DEMO-SP-0001/requirements/R1/trace",
	} {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodGet, target: target}), http.StatusServiceUnavailable, &doc)
		if doc.Code != "unavailable" {
			t.Errorf("%s: code = %q", target, doc.Code)
		}
	}
	// Requirement reads never depend on a seam.
	decode(t, send(t, s, request{method: http.MethodGet, target: specsBase + "/DEMO-SP-0001/requirements/R1"}),
		http.StatusOK, nil)
}

func TestSpecImpact(t *testing.T) {
	t.Parallel()

	t.Run("without history the answer is unavailable", func(t *testing.T) {
		s, _ := specServer(t)
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodGet, target: specsBase + "/impact?base=HEAD"}),
			http.StatusServiceUnavailable, &doc)
		if doc.Code != "unavailable" {
			t.Errorf("code = %q", doc.Code)
		}
	})

	s, _ := newGitServer(t, config.Git{})
	tests := []struct {
		name   string
		target string
		status int
		want   string
	}{
		{"the working tree against HEAD", specsBase + "/impact?base=HEAD", http.StatusOK, `"tier":1,"status":"ok"`},
		{"tier 1 alone", specsBase + "/impact?tiers=1", http.StatusOK, `{"tier":2,"status":"skipped","hits":0}`},
		{"an unknown revision", specsBase + "/impact?base=nope-nope", http.StatusBadRequest, `"invalid_request"`},
		{"a tier that is not a number", specsBase + "/impact?tier=x", http.StatusBadRequest, `"invalid_request"`},
		{"a depth out of range", specsBase + "/impact?depth=9", http.StatusBadRequest, `"invalid_request"`},
		{"the report", specsBase + "/impact/report?base=HEAD&budget=500", http.StatusOK, `"budget":500`},
		{"the text report", specsBase + "/impact/report?format=text", http.StatusOK, `"text":`},
		{"a foreign cursor", specsBase + "/impact/report?cursor=zzz", http.StatusBadRequest, `"invalid_request"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(t, s, request{method: http.MethodGet, target: tt.target})
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("%s = %d %s, want %d with %s", tt.target, rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}

// TestSpecFileEditsReachTheEventStream pins the refresh half of GIT-US-0127:
// a spec edited on disk is announced like any other item, so an open spec or
// requirement view refetches.
func TestSpecFileEditsReachTheEventStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	fake := newFakeWatcher()
	s, _, root := watchingServer(t, func(watcher.Options) (FileWatcher, error) { return fake, nil })
	specPath, _ := withSpec(t, s, root)
	s.startWatch(ctx)
	t.Cleanup(s.stopWatch)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "— Trim input", "— Trim every input", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, specPath)
	if err != nil {
		t.Fatal(err)
	}
	rel = filepath.ToSlash(rel)
	seq := s.hub.lastSeq()
	fake.events <- []watcher.Event{{Repo: testRepoID, Path: rel, Op: watcher.Write, Time: time.Now()}}

	for {
		if ids := itemChangedIDs(t, s, seq); len(ids) > 0 {
			if ids[0] != "DEMO-SP-0001" {
				t.Errorf("item.changed = %v, want the spec", ids)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("no item.changed for the edited spec")
		case <-time.After(10 * time.Millisecond):
		}
	}
	var got requirementResult
	decode(t, send(t, s, request{method: http.MethodGet, target: specsBase + "/DEMO-SP-0001/requirements/R1"}),
		http.StatusOK, &got)
	if got.Requirement.Title != "Trim every input" {
		t.Errorf("title = %q, want the edit the watcher folded in", got.Requirement.Title)
	}
}

func TestSpecLiveRoutes(t *testing.T) {
	t.Parallel()
	s, _ := specServer(t)

	story := "## Spec Delta\n\n### MODIFIED DEMO-SP-0001.R1 — Trim\n\nThe checkout SHALL trim input fast.\n\n" +
		"#### Scenario: spaces\n- **WHEN** it ends in spaces\n- **THEN** they go\n\n" +
		"### REMOVED DEMO-SP-0001.R9 — Gone\n\nReason: unused.\n"

	var lint struct {
		Findings []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Line     int    `json:"line"`
		} `json:"findings"`
	}
	decode(t, send(t, s, request{method: http.MethodPost, target: specsBase + "/lint",
		body: map[string]any{"type": "story", "body": story}}), http.StatusOK, &lint)
	if len(lint.Findings) != 2 || lint.Findings[0].Code != "LINT-REQ-VAGUE" || lint.Findings[0].Line != 5 ||
		lint.Findings[1].Code != "W-DELTA-DANGLING" || lint.Findings[1].Line != 11 {
		t.Errorf("findings = %+v", lint.Findings)
	}

	var preview struct {
		Operations []struct {
			Op       string `json:"op"`
			Target   string `json:"target"`
			Dangling string `json:"dangling"`
			Current  *struct {
				Text string `json:"text"`
			} `json:"current"`
		} `json:"operations"`
	}
	decode(t, send(t, s, request{method: http.MethodPost, target: specsBase + "/delta/preview",
		body: map[string]any{"body": story}}), http.StatusOK, &preview)
	if len(preview.Operations) != 2 || preview.Operations[0].Current == nil || preview.Operations[1].Dangling == "" {
		t.Errorf("operations = %+v", preview.Operations)
	}

	var doc problemBody
	decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/projects/NOPE/specs/lint",
		body: map[string]any{"type": "spec", "body": ""}}), http.StatusNotFound, &doc)
}

// TestRequirementStaleConflicts pins the conflicts a PATCH refused for a stale
// rev reports: none when the change is already on disk, only the fields that
// still differ otherwise (GIT-US-0151).
func TestRequirementStaleConflicts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		first, second map[string]any
		want          []string
	}{
		{
			name:   "already applied",
			first:  map[string]any{"title": "Trim everything", "status": "todo"},
			second: map[string]any{"title": "Trim everything", "status": "todo"},
		},
		{
			name:   "partial overlap",
			first:  map[string]any{"title": "Trim everything", "status": "todo"},
			second: map[string]any{"title": "Trim everything", "status": "in_progress"},
			want:   []string{"status"},
		},
		{
			name:   "real conflict",
			first:  map[string]any{"title": "Trim everything"},
			second: map[string]any{"title": "Trim nothing", "text": "The checkout SHALL NOT trim."},
			want:   []string{"text", "title"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := specServer(t)
			target := specsBase + "/DEMO-SP-0001/requirements/R1"
			var base requirementResult
			decode(t, send(t, s, request{method: http.MethodGet, target: target}), http.StatusOK, &base)
			var wrote requirementResult
			decode(t, send(t, s, request{method: http.MethodPatch, target: target,
				header: map[string]string{"If-Match": base.Requirement.Rev}, body: tc.first}), http.StatusOK, &wrote)

			var doc problemBody
			decode(t, send(t, s, request{method: http.MethodPatch, target: target,
				header: map[string]string{"If-Match": base.Requirement.Rev}, body: tc.second}),
				http.StatusPreconditionFailed, &doc)
			if doc.Code != "stale_revision" || doc.CurrentRev != wrote.Requirement.Rev {
				t.Fatalf("problem = %+v", doc)
			}
			var got []string
			for _, c := range doc.Conflicts {
				got = append(got, c.Field)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("conflicts = %v, want %v", got, tc.want)
			}
		})
	}
}
