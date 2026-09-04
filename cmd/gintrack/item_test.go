package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

func TestItemListTable(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("item", "list")
	rows := lines(stdout)
	if len(rows) != 6 {
		t.Fatalf("rows = %q", rows)
	}
	if got := columns(rows[0]); strings.Join(got, "|") != "ID|TYPE|TITLE|STATUS|ASSIGNEE|PRIORITY|UPDATED" {
		t.Errorf("headers = %v", got)
	}
	if !strings.Contains(stdout, "DEMO-US-0001") || !strings.Contains(stdout, "Guest checkout") {
		t.Errorf("stdout = %q", stdout)
	}
	// The default sort is -updated, and the task was touched last.
	if first := columns(rows[1]); first[0] != "DEMO-T-0001" {
		t.Errorf("first row = %v, want the most recently updated item", first)
	}
}

func TestItemListFilters(t *testing.T) {
	h := newHarness(t)
	h.register()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "by status", args: []string{"--status", "todo"}, want: []string{"DEMO-US-0002"}},
		{name: "by type", args: []string{"--type", "epic"}, want: []string{"DEMO-EP-0001"}},
		{name: "by label", args: []string{"--label", "payments"}, want: []string{"DEMO-EP-0001", "DEMO-US-0002", "DEMO-M-0001"}},
		{name: "by assignee", args: []string{"--assignee", "marta"}, want: []string{"DEMO-US-0001", "DEMO-US-0002"}},
		{name: "by parent", args: []string{"--parent", "DEMO-US-0001"}, want: []string{"DEMO-T-0001"}},
		{name: "by text", args: []string{"--text", "northwind"}, want: []string{"DEMO-US-0001"}},
		{name: "by project", args: []string{"--project", "DEMO", "--type", "milestone"}, want: []string{"DEMO-M-0001"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"item", "list", "--json"}, tt.args...)
			payload := decode[struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
				Total int `json:"total"`
			}](t, h.mustRun(args...))
			got := make([]string, 0, len(payload.Items))
			for _, it := range payload.Items {
				got = append(got, it.ID)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ids = %v, want %v", got, tt.want)
			}
			for _, want := range tt.want {
				if !contains(got, want) {
					t.Errorf("ids = %v, want %q among them", got, want)
				}
			}
		})
	}
}

func TestItemListJSONShape(t *testing.T) {
	h := newHarness(t)
	h.register()

	payload := decode[itemListPayload](t, h.mustRun("item", "list", "--json", "--limit", "2", "--sort", "id"))
	if payload.Total != 5 || payload.Limit != 2 || len(payload.Items) != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	first := payload.Items[0].(map[string]any)
	if first["id"] != "DEMO-EP-0001" {
		t.Errorf("first item = %v", first["id"])
	}
	if first["repo"] != "acme-api" {
		t.Errorf("repo = %v, want the registration id", first["repo"])
	}
	if got, want := first["path"], "docs/.pmngr/epics/DEMO-EP-0001-checkout-revamp.md"; got != want {
		t.Errorf("path = %v, want the repository-relative %q", got, want)
	}
	if first["rev"] == "" || first["rev"] == nil {
		t.Error("the rev is missing from the payload")
	}
}

func TestItemListPagination(t *testing.T) {
	h := newHarness(t)
	h.register()

	payload := decode[itemListPayload](t, h.mustRun("item", "list", "--json", "--sort", "id", "--limit", "2", "--offset", "2"))
	if payload.Offset != 2 || len(payload.Items) != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	first := payload.Items[0].(map[string]any)
	if first["id"] != "DEMO-T-0001" {
		t.Errorf("first item = %v, want the third item by id", first["id"])
	}
}

func TestItemListFields(t *testing.T) {
	h := newHarness(t)
	h.register()

	payload := decode[itemListPayload](t, h.mustRun("item", "list", "--json", "--fields", "id,title,path", "--limit", "1", "--sort", "id"))
	item := payload.Items[0].(map[string]any)
	if len(item) != 4 {
		t.Errorf("item = %v, want id, title, path and repo", item)
	}
	if _, ok := item["status"]; ok {
		t.Errorf("item = %v, want only the requested fields", item)
	}
}

func TestItemListRejectsBadFlags(t *testing.T) {
	h := newHarness(t)
	h.register()

	if _, _, code := h.run("item", "list", "--type", "bug"); code != exitUsage {
		t.Errorf("unknown type: exit %d, want %d", code, exitUsage)
	}
	if _, _, code := h.run("item", "list", "--priority", "urgent"); code != exitUsage {
		t.Errorf("unknown priority: exit %d, want %d", code, exitUsage)
	}
	if _, _, code := h.run("item", "list", "--updated-since", "yesterday"); code != exitUsage {
		t.Errorf("bad duration: exit %d, want %d", code, exitUsage)
	}
}

func TestItemGet(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("item", "get", "DEMO-US-0001")
	for _, want := range []string{"id:", "DEMO-US-0001", "status:", "in_progress", "Guest checkout", "## Description", "rev:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lost %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "acme-api:docs/.pmngr/stories/") {
		t.Errorf("the path is not reported per repository:\n%s", stdout)
	}
}

func TestItemGetJSON(t *testing.T) {
	h := newHarness(t)
	h.register()

	payload := decode[itemGetPayload](t, h.mustRun("item", "get", "DEMO-US-0001", "--json", "--comments"))
	if payload.Item.ID != "DEMO-US-0001" || payload.Item.Repo != "acme-api" {
		t.Fatalf("item = %+v", payload.Item)
	}
	if payload.Item.Path != "docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md" {
		t.Errorf("path = %q", payload.Item.Path)
	}
	if len(payload.Children) != 1 || payload.Children[0] != "DEMO-T-0001" {
		t.Errorf("children = %v", payload.Children)
	}
	if len(payload.Comments) != 1 || payload.Comments[0].Author != "marta" {
		t.Errorf("comments = %+v", payload.Comments)
	}
	if payload.Item.Body == "" {
		t.Error("the body is missing")
	}

	payload = decode[itemGetPayload](t, h.mustRun("item", "get", "DEMO-US-0001", "--json", "--body=false"))
	if payload.Item.Body != "" {
		t.Error("--body=false still returned the body")
	}
}

func TestItemGetExitCodes(t *testing.T) {
	h := newHarness(t)
	h.register()

	if _, _, code := h.run("item", "get", "DEMO-US-9999"); code != exitNotFound {
		t.Errorf("unknown item: exit %d, want %d", code, exitNotFound)
	}
	if _, _, code := h.run("item", "get", "not-an-id"); code != exitValidation {
		t.Errorf("malformed id: exit %d, want %d", code, exitValidation)
	}
	if _, _, code := h.run("item", "get", "OTHER-US-0001"); code != exitNotFound {
		t.Errorf("unknown project: exit %d, want %d", code, exitNotFound)
	}
	if _, _, code := h.run("item", "get"); code != exitUsage {
		t.Errorf("missing argument: exit %d, want %d", code, exitUsage)
	}
}

func TestItemNew(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("item", "new", "--type", "task", "--title", "Wire OIDC discovery endpoint",
		"--parent", "DEMO-US-0001", "--assignee", "marta", "--label", "backend", "--priority", "high", "--effort", "6")
	if !strings.Contains(stdout, "created DEMO-T-0002") {
		t.Errorf("stdout = %q", stdout)
	}
	written := h.readFile("docs/.pmngr/tasks/DEMO-T-0002-wire-oidc-discovery-endpoint.md")
	for _, want := range []string{"id: DEMO-T-0002", "type: task", "parent: DEMO-US-0001", "priority: high", "effort: 6"} {
		if !strings.Contains(written, want) {
			t.Errorf("the file lost %q:\n%s", want, written)
		}
	}
	payload := decode[itemListPayload](t, h.mustRun("item", "list", "--json", "--type", "task"))
	if payload.Total != 2 {
		t.Errorf("tasks = %d, want the new one indexed", payload.Total)
	}
}

func TestItemNewReadsTheBodyFromStdin(t *testing.T) {
	h := newHarness(t)
	h.register()
	h.Stdin = strings.NewReader("## Description\nSee RFC 8414.\n")

	payload := decode[itemWritePayload](t, h.mustRun("item", "new", "--type", "task", "--title", "Read RFC 8414", "--body", "-", "--json"))
	if payload.ID != "DEMO-T-0002" || payload.Repo != "acme-api" {
		t.Fatalf("payload = %+v", payload)
	}
	if !strings.Contains(h.readFile(payload.Path), "See RFC 8414.") {
		t.Error("the body did not reach the file")
	}
	if payload.Rev == "" {
		t.Error("no rev was reported")
	}
}

func TestItemNewDryRun(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("item", "new", "--type", "story", "--title", "Never written", "--dry-run")
	if !strings.Contains(stdout, "would write acme-api:docs/.pmngr/stories/DEMO-US-0003-never-written.md") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "nothing was changed (--dry-run)") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "title: Never written") {
		t.Errorf("the preview does not show the file:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(h.Repo, "docs/.pmngr/stories/DEMO-US-0003-never-written.md")); err == nil {
		t.Error("--dry-run wrote the file")
	}
	counter := h.readFile("docs/.pmngr/project.yaml")
	if !strings.Contains(counter, "story: 2") {
		t.Error("--dry-run advanced the id counter on disk")
	}
}

func TestItemNewValidation(t *testing.T) {
	h := newHarness(t)
	h.register()

	if _, _, code := h.run("item", "new", "--type", "task"); code != exitUsage {
		t.Errorf("missing title: exit %d, want %d", code, exitUsage)
	}
	if _, _, code := h.run("item", "new", "--title", "No type"); code != exitUsage {
		t.Errorf("missing type: exit %d, want %d", code, exitUsage)
	}
	if _, _, code := h.run("item", "new", "--type", "task", "--title", "Bad status", "--status", "shipped"); code != exitValidation {
		t.Errorf("unknown status: exit %d, want %d", code, exitValidation)
	}
	if _, _, code := h.run("item", "new", "--type", "task", "--title", "Bad date", "--due", "31/12/2026"); code != exitValidation {
		t.Errorf("bad date: exit %d, want %d", code, exitValidation)
	}
}

func TestItemEdit(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("item", "edit", "DEMO-US-0002", "--title", "Save payment methods safely",
		"--add-label", "security", "--priority", "high")
	if !strings.Contains(stdout, "updated DEMO-US-0002") {
		t.Errorf("stdout = %q", stdout)
	}
	written := h.readFile("docs/.pmngr/stories/DEMO-US-0002-save-payment-methods-safely.md")
	for _, want := range []string{"title: Save payment methods safely", "priority: high", "security"} {
		if !strings.Contains(written, want) {
			t.Errorf("the file lost %q:\n%s", want, written)
		}
	}
	if _, err := os.Stat(filepath.Join(h.Repo, "docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md")); err == nil {
		t.Error("the old file survived the rename")
	}
}

func TestItemEditRevMismatch(t *testing.T) {
	h := newHarness(t)
	h.register()

	_, _, code := h.run("item", "edit", "DEMO-US-0002", "--title", "Changed", "--rev", "sha256:deadbeef")
	if code != exitConflict {
		t.Fatalf("exit = %d, want %d for a stale revision", code, exitConflict)
	}
	if strings.Contains(h.readFile("docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"), "Changed") {
		t.Error("a refused edit still wrote the file")
	}

	current := decode[itemGetPayload](t, h.mustRun("item", "get", "DEMO-US-0002", "--json"))
	if _, _, code := h.run("item", "edit", "DEMO-US-0002", "--priority", "low", "--rev", string(current.Item.Rev)); code != exitOK {
		t.Errorf("the matching revision was refused: exit %d", code)
	}
}

func TestItemEditNeedsAChange(t *testing.T) {
	h := newHarness(t)
	h.register()

	if _, _, code := h.run("item", "edit", "DEMO-US-0002"); code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

func TestItemMove(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("item", "move", "DEMO-US-0002", "in_progress", "--comment", "starting")
	if !strings.Contains(stdout, "DEMO-US-0002  todo -> in_progress") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(h.readFile("docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"), "status: in_progress") {
		t.Error("the status was not written")
	}
	comments := decode[itemGetPayload](t, h.mustRun("item", "get", "DEMO-US-0002", "--json", "--comments"))
	if len(comments.Comments) != 1 || !strings.Contains(comments.Comments[0].Body, "starting") {
		t.Errorf("comments = %+v", comments.Comments)
	}
	if comments.Comments[0].Kind != core.CommentKindStatusChange {
		t.Errorf("kind = %q, want a status change", comments.Comments[0].Kind)
	}
}

func TestItemMoveRefusesAnIllegalTransition(t *testing.T) {
	h := newHarness(t)
	h.register()

	_, _, code := h.run("item", "move", "DEMO-US-0002", "done")
	if code != exitConflict {
		t.Fatalf("exit = %d, want %d for a transition the workflow forbids", code, exitConflict)
	}
	if _, _, code := h.run("item", "move", "DEMO-US-0002", "done", "--force"); code != exitOK {
		t.Errorf("--force was refused: exit %d", code)
	}
	if _, _, code := h.run("item", "move", "DEMO-US-0002", "shipped", "--force"); code != exitValidation {
		t.Errorf("unknown status: exit %d, want %d", code, exitValidation)
	}
}

func TestItemMoveJSON(t *testing.T) {
	h := newHarness(t)
	h.register()

	payload := decode[itemWritePayload](t, h.mustRun("item", "move", "DEMO-US-0002", "in_progress", "--json"))
	if payload.From != "todo" || payload.Status != "in_progress" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestItemComment(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("item", "comment", "DEMO-US-0002", "--body", "Looks good", "--author", "jose")
	if !strings.Contains(stdout, "commented on DEMO-US-0002") {
		t.Errorf("stdout = %q", stdout)
	}
	payload := decode[itemGetPayload](t, h.mustRun("item", "get", "DEMO-US-0002", "--json", "--comments"))
	if len(payload.Comments) != 1 || payload.Comments[0].Author != "jose" {
		t.Fatalf("comments = %+v", payload.Comments)
	}
	if !strings.HasPrefix(payload.Comments[0].Path, "docs/.pmngr/comments/DEMO-US-0002/") {
		t.Errorf("path = %q", payload.Comments[0].Path)
	}
	if payload.Comments[0].Ref == "" {
		t.Error("the comment reference is missing")
	}
	if _, _, code := h.run("item", "comment", "DEMO-US-0002"); code != exitUsage {
		t.Error("an empty comment was accepted")
	}
}

func TestItemLinkWritesTheInverse(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("item", "link", "DEMO-US-0002", "blocked_by", "DEMO-T-0001")
	if !strings.Contains(stdout, "linked DEMO-US-0002  blocked_by DEMO-T-0001") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "linked DEMO-T-0001  blocks DEMO-US-0002") {
		t.Errorf("the inverse was not written: %q", stdout)
	}
	story := h.readFile("docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md")
	if !strings.Contains(story, "blocked_by") {
		t.Errorf("the relation is missing:\n%s", story)
	}

	stdout = h.mustRun("item", "link", "DEMO-US-0002", "blocked_by", "DEMO-T-0001", "--remove")
	if !strings.Contains(stdout, "unlinked DEMO-US-0002") {
		t.Errorf("stdout = %q", stdout)
	}
	if strings.Contains(h.readFile("docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"), "blocked_by") {
		t.Error("the relation survived --remove")
	}
}

func TestItemLinkJSON(t *testing.T) {
	h := newHarness(t)
	h.register()

	payload := decode[linkPayload](t, h.mustRun("item", "link", "DEMO-US-0002", "relates_to", "DEMO-EP-0001", "--json"))
	if payload.Kind != "relates_to" || payload.Target != "DEMO-EP-0001" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Inverse == nil || payload.Inverse.ID != "DEMO-EP-0001" {
		t.Errorf("inverse = %+v", payload.Inverse)
	}
}

func TestItemLinkRejectsAnUnknownRelation(t *testing.T) {
	h := newHarness(t)
	h.register()

	if _, _, code := h.run("item", "link", "DEMO-US-0002", "mentions", "DEMO-EP-0001"); code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

func TestItemWithoutARegisteredRepository(t *testing.T) {
	h := newHarness(t)

	if _, _, code := h.run("item", "list"); code != exitNotFound {
		t.Errorf("exit = %d, want %d", code, exitNotFound)
	}
}

// contains reports whether a list holds a value.
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
