package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corevault "github.com/digiogithub/git-in-track/internal/vault"
)

// The commands of youtracksync.go: import, push-comments and kb push|pull.

// newImportStub starts a stub instance that answers one issue to any search and
// to any issue read, which is all an import preview needs.
func newImportStub(t *testing.T) *httptest.Server {
	t.Helper()

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/users/me"):
			_, _ = w.Write([]byte(`{"id":"1-1","login":"jose","fullName":"Jose"}`))
		case strings.HasSuffix(r.URL.Path, "/api/issues"):
			_, _ = w.Write([]byte(`[{"id":"2-1","idReadable":"ACME-42","summary":"Guest checkout"}]`))
		case strings.Contains(r.URL.Path, "/api/issues/"):
			_, _ = w.Write([]byte(`{"id":"2-1","idReadable":"ACME-42","summary":"Guest checkout"}`))
		case strings.Contains(r.URL.Path, "/api/admin/projects"):
			_, _ = w.Write([]byte(`{"id":"0-1","shortName":"ACME","name":"ACME API"}`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(stub.Close)
	return stub
}

// connect links the harness repository to a stub instance, which is the
// precondition of every command in this file.
func (h *harness) connect(t *testing.T, url string) {
	t.Helper()

	h.Stdin = strings.NewReader(cliToken + "\n")
	h.mustRun("youtrack", "connect", "--url", url, "--project", "ACME")
	h.Stdin = nil
}

// TestYouTrackImportDryRunPrintsThePlan covers the preview: the plan is printed,
// the JSON payload carries the documented keys and nothing is written.
func TestYouTrackImportDryRunPrintsThePlan(t *testing.T) {
	stub := newImportStub(t)
	h := newHarness(t)
	h.register()
	h.connect(t, stub.URL)

	before := readFile(t, h.projectYAML())
	stdout := h.mustRun("youtrack", "import", "project: ACME", "--dry-run", "--json")
	payload := decode[corevault.YouTrackImportPreview](t, stdout)

	if payload.Project != cliProjectKey {
		t.Errorf("project = %q, want %s", payload.Project, cliProjectKey)
	}
	if len(payload.Issues) != 1 || payload.Issues[0].YouTrackID != "ACME-42" {
		t.Fatalf("issues = %+v", payload.Issues)
	}
	if payload.Issues[0].Action != corevault.YouTrackImportCreate {
		t.Errorf("action = %q, want create", payload.Issues[0].Action)
	}
	if after := readFile(t, h.projectYAML()); after != before {
		t.Error("a dry run wrote to project.yaml")
	}
}

// TestYouTrackImportTablePrintsIssueActionAndItem covers the text form.
func TestYouTrackImportTablePrintsIssueActionAndItem(t *testing.T) {
	stub := newImportStub(t)
	h := newHarness(t)
	h.register()
	h.connect(t, stub.URL)

	stdout := h.mustRun("youtrack", "import", "ACME-42")
	rows := lines(stdout)
	if len(rows) < 2 {
		t.Fatalf("the table has no rows:\n%s", stdout)
	}
	header := columns(rows[0])
	if len(header) < 3 || header[0] != "ISSUE" || header[1] != "ACTION" || header[2] != "ITEM" {
		t.Errorf("header = %v, want issue, action and item", header)
	}
	cells := columns(rows[1])
	if cells[0] != "ACME-42" {
		t.Errorf("row = %v, want the imported issue first", cells)
	}
	if !strings.HasPrefix(cells[2], cliProjectKey+"-") {
		t.Errorf("row = %v, want a git-in-track item id", cells)
	}
}

// TestYouTrackImportRejectsBadInvocations covers the shared validators: a bad
// invocation exits 2 and never reaches the tracker.
func TestYouTrackImportRejectsBadInvocations(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{name: "no selection at all", args: []string{"youtrack", "import"}, want: exitUsage},
		{name: "an unknown flag", args: []string{"youtrack", "import", "ACME-1", "--nope"}, want: exitUsage},
		{name: "a depth beyond the cap", args: []string{"youtrack", "import", "ACME-1", "--depth", "9"}, want: exitValidation},
		{name: "a negative depth", args: []string{"youtrack", "import", "ACME-1", "--depth", "-1"}, want: exitValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newImportStub(t)
			h := newHarness(t)
			h.register()
			h.connect(t, stub.URL)

			_, stderr, code := h.run(tc.args...)
			if code != tc.want {
				t.Fatalf("exit = %d, want %d\n%s", code, tc.want, stderr)
			}
		})
	}
}

// TestIssueIDArgs pins how one command tells a list of issue ids from a query,
// which is what lets it accept both without a second flag.
func TestIssueIDArgs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "one id", args: []string{"ACME-42"}, want: []string{"ACME-42"}},
		{name: "several ids", args: []string{"ACME-42", "ACME-43"}, want: []string{"ACME-42", "ACME-43"}},
		{name: "a query with a colon", args: []string{"project:", "ACME"}},
		{name: "a query with a hash", args: []string{"#Unresolved"}},
		{name: "a bare word", args: []string{"checkout"}},
		{name: "an id with a non-numeric tail", args: []string{"ACME-4x"}},
		{name: "a trailing dash", args: []string{"ACME-"}},
		{name: "one id and one word", args: []string{"ACME-42", "checkout"}},
		{name: "nothing", args: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := issueIDArgs(tc.args)
			if ok != (tc.want != nil) {
				t.Fatalf("issueIDArgs(%v) reported %v, want %v", tc.args, ok, tc.want != nil)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("issueIDArgs(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestYouTrackPushCommentsRefusesAnUnlinkedItem covers the precondition: an item
// mirroring no issue is a usage failure the user can fix, not a generic one, and
// nothing is queued.
func TestYouTrackPushCommentsRefusesAnUnlinkedItem(t *testing.T) {
	stub := newImportStub(t)
	h := newHarness(t)
	h.register()
	h.connect(t, stub.URL)

	_, stderr, code := h.run("youtrack", "push-comments", "DEMO-US-0001", "--all")
	if code != exitValidation {
		t.Fatalf("exit = %d, want %d\n%s", code, exitValidation, stderr)
	}
	if !strings.Contains(stderr, "import or link") {
		t.Errorf("the message does not say what to do: %s", stderr)
	}
}

// TestYouTrackPushCommentsRejectsBadInvocations covers the selection rule:
// exactly one of --all and --comment.
func TestYouTrackPushCommentsRejectsBadInvocations(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{name: "no item", args: []string{"youtrack", "push-comments"}, want: exitUsage},
		{name: "two items", args: []string{"youtrack", "push-comments", "A-1", "B-2"}, want: exitUsage},
		{
			name: "neither selection",
			args: []string{"youtrack", "push-comments", "DEMO-US-0001"},
			want: exitValidation,
		},
		{
			name: "both selections",
			args: []string{"youtrack", "push-comments", "DEMO-US-0001", "--all", "--comment", "docs/x.md"},
			want: exitValidation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newImportStub(t)
			h := newHarness(t)
			h.register()
			h.connect(t, stub.URL)

			_, stderr, code := h.run(tc.args...)
			if code != tc.want {
				t.Fatalf("exit = %d, want %d\n%s", code, tc.want, stderr)
			}
		})
	}
}

// TestYouTrackKBStatusReportsEveryPage covers the status command: it reads only
// the repository unless asked otherwise, and says so.
func TestYouTrackKBStatusReportsEveryPage(t *testing.T) {
	stub := newImportStub(t)
	h := newHarness(t)
	h.register()
	h.connect(t, stub.URL)

	stdout := h.mustRun("youtrack", "kb", "status", "docs", "--recursive", "--json")
	payload := decode[corevault.YouTrackKBStatusResult](t, stdout)
	if payload.Project != cliProjectKey {
		t.Errorf("project = %q, want %s", payload.Project, cliProjectKey)
	}
	if len(payload.Pages) == 0 {
		t.Fatal("no page was reported")
	}
	if payload.Remote {
		t.Error("the tracker was consulted without --remote")
	}
	for _, page := range payload.Pages {
		if page.State != corevault.KBStateUnlinked {
			t.Errorf("%s: state = %q, want unlinked", page.Path, page.State)
		}
	}
}

// TestYouTrackKBQueuesAJob covers both directions of the queueing form: the
// command prints the job id and the pages it selected, and writes nothing
// itself.
func TestYouTrackKBQueuesAJob(t *testing.T) {
	for _, direction := range []string{"push", "pull"} {
		t.Run(direction, func(t *testing.T) {
			stub := newImportStub(t)
			h := newHarness(t)
			h.register()
			h.connect(t, stub.URL)

			stdout := h.mustRun("youtrack", "kb", direction, "docs", "--recursive", "--json")
			payload := decode[corevault.YouTrackKBJobResult](t, stdout)
			if payload.JobID == "" {
				t.Error("no job id was printed")
			}
			if len(payload.Pages) == 0 {
				t.Error("no page was selected")
			}
		})
	}
}

// TestYouTrackKBRejectsBadInvocations covers the shared validators.
func TestYouTrackKBRejectsBadInvocations(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{name: "push with no path", args: []string{"youtrack", "kb", "push"}, want: exitUsage},
		{name: "pull with two paths", args: []string{"youtrack", "kb", "pull", "a", "b"}, want: exitUsage},
		{name: "an absolute path", args: []string{"youtrack", "kb", "push", "/etc/passwd"}, want: exitValidation},
		{name: "a path leaving the vault", args: []string{"youtrack", "kb", "push", "../outside"}, want: exitValidation},
		{name: "an unknown flag", args: []string{"youtrack", "kb", "push", "docs", "--nope"}, want: exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newImportStub(t)
			h := newHarness(t)
			h.register()
			h.connect(t, stub.URL)

			_, stderr, code := h.run(tc.args...)
			if code != tc.want {
				t.Fatalf("exit = %d, want %d\n%s", code, tc.want, stderr)
			}
		})
	}
}

// TestYouTrackSyncCommandsNeedAConnection covers the refusal every command
// shares: a project with no `integrations.youtrack` block, or with no token, is
// told what to run rather than failing obscurely.
func TestYouTrackSyncCommandsNeedAConnection(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "import", args: []string{"youtrack", "import", "ACME-1"}},
		{name: "push-comments", args: []string{"youtrack", "push-comments", "DEMO-US-0001", "--all"}},
		{name: "kb push", args: []string{"youtrack", "kb", "push", "docs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.register()

			_, stderr, code := h.run(tc.args...)
			if code == exitOK {
				t.Fatal("an unconnected project was accepted")
			}
			if !strings.Contains(strings.ToLower(stderr), "youtrack") {
				t.Errorf("the failure does not mention the connection: %s", stderr)
			}
		})
	}
}
