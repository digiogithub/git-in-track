package vault

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// fixtureRoot is the vault every test in this file is built from.
const fixtureRoot = "../../testdata/fixtures/project-basic"

// envelope is the decoded form of what Vault.Call returns.
type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Path    string `json:"path"`
	} `json:"error"`
}

// call runs one method and fails the test when the envelope reports an error.
func call(t *testing.T, v *Vault, method string, params any) json.RawMessage {
	t.Helper()
	env := rawCall(t, v, method, params)
	if !env.OK {
		t.Fatalf("%s: %s: %s", method, env.Error.Code, env.Error.Message)
	}
	return env.Result
}

// rawCall runs one method and returns the envelope, error or not.
func rawCall(t *testing.T, v *Vault, method string, params any) envelope {
	t.Helper()
	encoded := "null"
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("encode params for %s: %v", method, err)
		}
		encoded = string(data)
	}
	var env envelope
	if err := json.Unmarshal([]byte(v.Call(method, encoded)), &env); err != nil {
		t.Fatalf("%s returned invalid JSON: %v", method, err)
	}
	return env
}

// decode unmarshals a result payload into v.
func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return v
}

// fixtureFiles reads the fixture vault from disk into the contract's VaultFile
// list, with vault-relative forward-slash paths.
func fixtureFiles(t *testing.T) []map[string]string {
	t.Helper()
	var files []map[string]string
	err := filepath.WalkDir(fixtureRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p) //nolint:gosec // fixture paths come from the walk itself
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(fixtureRoot, p)
		if err != nil {
			return err
		}
		files = append(files, map[string]string{
			"path": filepath.ToSlash(rel),
			"text": string(data),
		})
		return nil
	})
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("fixture %s is empty", fixtureRoot)
	}
	return files
}

// loadedVault returns a vault with the fixture files indexed and a pinned
// clock, so that created/updated stamps are reproducible.
func loadedVault(t *testing.T) (*Vault, IndexStats) {
	t.Helper()
	v := NewInMemory()
	v.SetClock(func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) })
	raw := call(t, v, "vault.load", map[string]any{"files": fixtureFiles(t)})
	return v, decode[IndexStats](t, raw)
}

func TestVaultLoad(t *testing.T) {
	v, stats := loadedVault(t)

	if stats.Projects != 1 {
		t.Errorf("projects = %d, want 1", stats.Projects)
	}
	if stats.Items != 5 {
		t.Errorf("items = %d, want 5 (1 epic, 2 stories, 1 task, 1 milestone)", stats.Items)
	}
	if stats.Comments != 1 {
		t.Errorf("comments = %d, want 1", stats.Comments)
	}
	if stats.Pages != 2 {
		t.Errorf("pages = %d, want 2", stats.Pages)
	}
	if stats.Fingerprint == "" {
		t.Error("fingerprint is empty")
	}
	if stats.Diagnostics == nil {
		t.Error("diagnostics must be an array, never null")
	}
	if stats.Errors != 0 {
		t.Errorf("errors = %d, want 0: %+v", stats.Errors, stats.Diagnostics)
	}

	t.Run("stats are stable across calls", func(t *testing.T) {
		again := decode[IndexStats](t, call(t, v, "vault.stats", nil))
		if again.Fingerprint != stats.Fingerprint {
			t.Errorf("fingerprint drifted: %s != %s", again.Fingerprint, stats.Fingerprint)
		}
	})

	t.Run("projects carry their workflow", func(t *testing.T) {
		projects := decode[[]projectSummary](t, call(t, v, "project.list", nil))
		if len(projects) != 1 {
			t.Fatalf("project.list returned %d projects, want 1", len(projects))
		}
		p := projects[0]
		if p.Key != "DEMO" || p.DocsPath != "docs" {
			t.Errorf("project = %s at %s, want DEMO at docs", p.Key, p.DocsPath)
		}
		if len(p.Statuses) != 6 {
			t.Errorf("statuses = %d, want 6", len(p.Statuses))
		}
		if p.ItemCounts["story"] != 2 || p.ItemCounts["epic"] != 1 || p.ItemCounts["comment"] != 1 {
			t.Errorf("itemCounts = %v", p.ItemCounts)
		}
		if !p.Writable {
			t.Error("a project with a valid project.yaml must be writable")
		}
	})
}

func TestVaultItemList(t *testing.T) {
	v, _ := loadedVault(t)

	t.Run("every item", func(t *testing.T) {
		page := decode[itemPage](t, call(t, v, "item.list", map[string]any{}))
		if page.Total != 5 {
			t.Errorf("total = %d, want 5", page.Total)
		}
		for _, it := range page.Items {
			if it.Body != "" {
				t.Errorf("%s: a list call must not carry the body", it.ID)
			}
			if it.Rev == "" {
				t.Errorf("%s: rev is empty", it.ID)
			}
		}
	})

	t.Run("filtered by type and status", func(t *testing.T) {
		page := decode[itemPage](t, call(t, v, "item.list", map[string]any{
			"type": "story", "status": []string{"in_progress"},
		}))
		if page.Total != 1 || page.Items[0].ID != "DEMO-US-0001" {
			t.Fatalf("got %d items %v, want DEMO-US-0001", page.Total, page.Items)
		}
	})

	t.Run("filtered by category", func(t *testing.T) {
		page := decode[itemPage](t, call(t, v, "item.list", map[string]any{
			"type": []string{"story", "task"}, "category": "in_progress",
		}))
		// The category spans both in_progress statuses of the fixture workflow:
		// `in_progress` (the story) and `in_review` (the task).
		if page.Total != 2 {
			t.Errorf("total = %d, want the in_progress story and the in_review task", page.Total)
		}
	})

	t.Run("body on request", func(t *testing.T) {
		page := decode[itemPage](t, call(t, v, "item.list", map[string]any{
			"type": "story", "fields": []string{"id", "body"},
		}))
		for _, it := range page.Items {
			if it.Body == "" {
				t.Errorf("%s: body was requested but is empty", it.ID)
			}
		}
	})

	t.Run("get and children", func(t *testing.T) {
		var item struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		}
		if err := json.Unmarshal(call(t, v, "item.get", map[string]any{"id": "DEMO-EP-0001"}), &item); err != nil {
			t.Fatalf("decode item: %v", err)
		}
		if item.ID != "DEMO-EP-0001" || !strings.Contains(item.Body, "## ") {
			t.Errorf("item.get returned %+v", item)
		}
		kids := decode[[]struct {
			ID string `json:"id"`
		}](t, call(t, v, "item.children", map[string]any{"id": "DEMO-EP-0001"}))
		if len(kids) != 2 {
			t.Errorf("children = %d, want the two stories", len(kids))
		}
	})

	t.Run("a missing item is not_found", func(t *testing.T) {
		env := rawCall(t, v, "item.get", map[string]any{"id": "DEMO-US-9999"})
		if env.OK || env.Error.Code != "not_found" {
			t.Errorf("got ok=%v code=%q, want not_found", env.OK, env.Error.Code)
		}
	})
}

func TestVaultItemCreateReturnsWriteSet(t *testing.T) {
	v, before := loadedVault(t)

	raw := call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "task", "title": "Trim pasted addresses",
		"parent": "DEMO-US-0001", "author": "claude", "labels": []string{"frontend"},
		"body": "## Description\n\nTrim the address before validating it.\n",
	})
	created := decode[struct {
		Item struct {
			ID     string `json:"id"`
			Path   string `json:"path"`
			Rev    string `json:"rev"`
			Status string `json:"status"`
		} `json:"item"`
		Writes WriteSet `json:"writes"`
	}](t, raw)

	if created.Item.ID != "DEMO-T-0002" {
		t.Errorf("allocated id = %s, want DEMO-T-0002", created.Item.ID)
	}
	if created.Item.Status != "todo" {
		t.Errorf("status = %q, want the project default todo", created.Item.Status)
	}
	want := "docs/.pmngr/tasks/DEMO-T-0002-trim-pasted-addresses.md"
	if created.Item.Path != want {
		t.Errorf("path = %s, want %s", created.Item.Path, want)
	}
	if len(created.Writes.Removed) != 0 {
		t.Errorf("removed = %v, want none", created.Writes.Removed)
	}

	written := map[string]string{}
	for _, f := range created.Writes.Written {
		written[f.Path] = f.Text
	}
	if _, ok := written[want]; !ok {
		t.Fatalf("the new file is missing from the WriteSet: %v", created.Writes.Written)
	}
	if !strings.Contains(written[want], "id: DEMO-T-0002") {
		t.Errorf("the WriteSet carries the wrong bytes:\n%s", written[want])
	}
	if _, ok := written["docs/.pmngr/project.yaml"]; !ok {
		t.Error("write_counters is on, so project.yaml must be in the WriteSet too")
	}
	for p := range written {
		if strings.HasSuffix(p, ".tmp") {
			t.Errorf("%s: the atomic write temporary must never reach the host", p)
		}
	}

	t.Run("the index sees the new item at once", func(t *testing.T) {
		after := decode[IndexStats](t, call(t, v, "vault.stats", nil))
		if after.Items != before.Items+1 {
			t.Errorf("items = %d, want %d", after.Items, before.Items+1)
		}
		if after.Fingerprint == before.Fingerprint {
			t.Error("the fingerprint must change when a file is written")
		}
		page := decode[itemPage](t, call(t, v, "item.list", map[string]any{
			"parent": "DEMO-US-0001", "sort": "id", "order": "asc",
		}))
		if page.Total != 2 || page.Items[1].ID != "DEMO-T-0002" {
			t.Errorf("the new task is not queryable: %+v", page)
		}
	})

	t.Run("a stale rev is refused", func(t *testing.T) {
		env := rawCall(t, v, "item.update", map[string]any{
			"id":    "DEMO-T-0002",
			"rev":   "sha256:0000000000000000",
			"patch": map[string]any{"set": map[string]any{"title": "Nope"}},
		})
		if env.OK {
			t.Fatal("an update with a stale rev must fail")
		}
		if env.Error.Code != "stale_revision" {
			t.Errorf("code = %q, want stale_revision", env.Error.Code)
		}
		if env.Error.Path == "" {
			t.Error("a stale revision must name the file it is about")
		}
	})

	t.Run("the current rev is accepted", func(t *testing.T) {
		updated := decode[struct {
			Item struct {
				Title string `json:"title"`
				Rev   string `json:"rev"`
			} `json:"item"`
			Writes WriteSet `json:"writes"`
		}](t, call(t, v, "item.update", map[string]any{
			"id":  "DEMO-T-0002",
			"rev": created.Item.Rev,
			"patch": map[string]any{
				"set":  map[string]any{"priority": "high"},
				"body": "## Description\n\nTrim, then validate.\n",
			},
		}))
		if updated.Item.Rev == created.Item.Rev {
			t.Error("the rev must change when the file changes")
		}
		if len(updated.Writes.Written) == 0 {
			t.Error("an update must report the file it wrote")
		}
	})

	t.Run("move honors the workflow", func(t *testing.T) {
		current := decode[struct {
			Rev string `json:"rev"`
		}](t, call(t, v, "item.get", map[string]any{"id": "DEMO-T-0002"}))
		moved := decode[struct {
			Item struct {
				Status string `json:"status"`
			} `json:"item"`
		}](t, call(t, v, "item.move", map[string]any{
			"id": "DEMO-T-0002", "status": "in_progress", "rev": current.Rev,
		}))
		if moved.Item.Status != "in_progress" {
			t.Errorf("status = %q, want in_progress", moved.Item.Status)
		}
	})

	t.Run("a hard delete removes the file", func(t *testing.T) {
		current := decode[struct {
			Rev string `json:"rev"`
		}](t, call(t, v, "item.get", map[string]any{"id": "DEMO-T-0002"}))
		deleted := decode[struct {
			Writes WriteSet `json:"writes"`
		}](t, call(t, v, "item.delete", map[string]any{
			"id": "DEMO-T-0002", "rev": current.Rev, "hard": true,
		}))
		if len(deleted.Writes.Removed) != 1 {
			t.Fatalf("removed = %v, want exactly the item file", deleted.Writes.Removed)
		}
		env := rawCall(t, v, "item.get", map[string]any{"id": "DEMO-T-0002"})
		if env.OK {
			t.Error("the deleted item is still in the index")
		}
	})
}

func TestVaultComments(t *testing.T) {
	v, _ := loadedVault(t)

	existing := decode[[]struct {
		Author string `json:"author"`
		Body   string `json:"body"`
	}](t, call(t, v, "comment.list", map[string]any{"id": "DEMO-US-0001"}))
	if len(existing) != 1 || existing[0].Author != "marta" {
		t.Fatalf("comment.list returned %+v", existing)
	}

	added := decode[struct {
		Comment struct {
			Author string `json:"author"`
			Path   string `json:"path"`
		} `json:"comment"`
		Writes WriteSet `json:"writes"`
	}](t, call(t, v, "comment.add", map[string]any{
		"id": "DEMO-US-0001", "author": "Claude", "body": "Trimming lands in DEMO-T-0001.",
	}))
	if added.Comment.Author != "claude" {
		t.Errorf("author = %q, want the sanitized handle", added.Comment.Author)
	}
	if len(added.Writes.Written) != 1 || added.Writes.Written[0].Path != added.Comment.Path {
		t.Errorf("writes = %+v, want the comment file", added.Writes.Written)
	}
	after := decode[[]json.RawMessage](t, call(t, v, "comment.list", map[string]any{"id": "DEMO-US-0001"}))
	if len(after) != 2 {
		t.Errorf("the thread has %d comments, want 2", len(after))
	}
}

func TestVaultKnowledgeBase(t *testing.T) {
	v, _ := loadedVault(t)

	t.Run("tree", func(t *testing.T) {
		tree := decode[[]kbNode](t, call(t, v, "kb.tree", map[string]any{}))
		var dirs, pages int
		var walk func(nodes []kbNode)
		walk = func(nodes []kbNode) {
			for _, n := range nodes {
				if n.Kind == "dir" {
					dirs++
				} else {
					pages++
				}
				walk(n.Children)
			}
		}
		walk(tree)
		if pages != 2 || dirs != 1 {
			t.Errorf("tree has %d pages and %d dirs, want 2 and 1: %+v", pages, dirs, tree)
		}
	})

	t.Run("page with backlinks", func(t *testing.T) {
		page := decode[kbPageResult](t, call(t, v, "kb.page", map[string]any{
			"path": "docs/architecture/overview.md",
		}))
		if page.Title == "" {
			t.Error("title is empty")
		}
		if !strings.Contains(page.Body, "three-tier") {
			t.Errorf("body does not look like the fixture page: %q", page.Body)
		}
		if page.Rev == "" {
			t.Error("rev is empty")
		}
		if len(page.Outgoing) == 0 {
			t.Errorf("outgoing = %v, want the DEMO-US-0002 wikilink", page.Outgoing)
		}
		if page.FrontMatter == nil {
			t.Error("frontMatter must be an object, never null")
		}
	})

	t.Run("write creates a page and reindexes it", func(t *testing.T) {
		result := decode[struct {
			Page   kbPageResult `json:"page"`
			Writes WriteSet     `json:"writes"`
		}](t, call(t, v, "kb.write", map[string]any{
			"path": "docs/runbooks/deploy.md",
			"text": "---\ntitle: Deploy\n---\n\nSee [[DEMO-EP-0001]].\n",
		}))
		if result.Page.Title != "Deploy" {
			t.Errorf("title = %q, want Deploy", result.Page.Title)
		}
		if len(result.Writes.Written) != 1 {
			t.Fatalf("writes = %+v, want one file", result.Writes.Written)
		}
		if result.Writes.Written[0].Path != "docs/runbooks/deploy.md" {
			t.Errorf("wrote %s", result.Writes.Written[0].Path)
		}
		stats := decode[IndexStats](t, call(t, v, "vault.stats", nil))
		if stats.Pages != 3 {
			t.Errorf("pages = %d, want 3 after the write", stats.Pages)
		}
	})

	t.Run("an unknown page is not_found", func(t *testing.T) {
		env := rawCall(t, v, "kb.page", map[string]any{"path": "docs/nope.md"})
		if env.OK || env.Error.Code != "not_found" {
			t.Errorf("got ok=%v code=%q", env.OK, env.Error.Code)
		}
	})
}

func TestVaultSearch(t *testing.T) {
	v, _ := loadedVault(t)

	hits := decode[[]searchHit](t, call(t, v, "search", map[string]any{"q": "checkout"}))
	if len(hits) == 0 {
		t.Fatal("search returned nothing for a term the fixture uses")
	}
	var kinds = map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
		if h.Path == "" || h.Title == "" {
			t.Errorf("incomplete hit: %+v", h)
		}
	}
	if !kinds["item"] {
		t.Errorf("no item hit among %+v", hits)
	}

	t.Run("an id match ranks first", func(t *testing.T) {
		byID := decode[[]searchHit](t, call(t, v, "search", map[string]any{"q": "DEMO-US-0002"}))
		if len(byID) == 0 || byID[0].ID != "DEMO-US-0002" {
			t.Errorf("got %+v, want DEMO-US-0002 first", byID)
		}
	})

	t.Run("limit is honored", func(t *testing.T) {
		limited := decode[[]searchHit](t, call(t, v, "search", map[string]any{"q": "checkout", "limit": 1}))
		if len(limited) != 1 {
			t.Errorf("got %d hits, want 1", len(limited))
		}
	})
}

func TestVaultSnapshotRoundTrip(t *testing.T) {
	source, stats := loadedVault(t)
	blob := decode[snapshotBlob](t, call(t, source, "snapshot.export", nil))
	if blob.Fingerprint != stats.Fingerprint {
		t.Errorf("snapshot fingerprint %s != index fingerprint %s", blob.Fingerprint, stats.Fingerprint)
	}
	if !strings.Contains(blob.JSON, "DEMO-US-0001") {
		t.Fatalf("the snapshot does not carry the items: %.120s", blob.JSON)
	}

	// A cold worker hydrates from the cache alone: no files, no scan.
	cold := NewInMemory()
	hydrated := decode[IndexStats](t, call(t, cold, "snapshot.load", blob))
	if hydrated.Items != stats.Items {
		t.Errorf("hydrated items = %d, want %d", hydrated.Items, stats.Items)
	}
	if hydrated.Pages != stats.Pages {
		t.Errorf("hydrated pages = %d, want %d", hydrated.Pages, stats.Pages)
	}
	if hydrated.Fingerprint != stats.Fingerprint {
		t.Errorf("hydrated fingerprint = %s, want %s", hydrated.Fingerprint, stats.Fingerprint)
	}
	page := decode[itemPage](t, call(t, cold, "item.list", map[string]any{"type": "story"}))
	if page.Total != 2 {
		t.Errorf("a hydrated index answers structural queries: total = %d, want 2", page.Total)
	}
	projects := decode[[]projectSummary](t, call(t, cold, "project.list", nil))
	if len(projects) != 1 || projects[0].Key != "DEMO" {
		t.Errorf("project.list after hydration = %+v", projects)
	}
}

func TestVaultApply(t *testing.T) {
	v, before := loadedVault(t)
	target := "docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"
	original, err := os.ReadFile(filepath.Join(fixtureRoot, filepath.FromSlash(target)))
	if err != nil {
		t.Fatalf("read fixture story: %v", err)
	}

	t.Run("a write reindexes one file", func(t *testing.T) {
		patched := strings.Replace(string(original), "title: Save payment methods", "title: Save cards", 1)
		stats := decode[IndexStats](t, call(t, v, "vault.apply", map[string]any{
			"events": []map[string]any{{"op": "write", "path": target, "text": patched}},
		}))
		if stats.Items != before.Items {
			t.Errorf("items = %d, want %d", stats.Items, before.Items)
		}
		if stats.Parsed != 1 {
			t.Errorf("parsed = %d, want exactly the changed file", stats.Parsed)
		}
		if stats.Delta == nil || len(stats.Delta.Updated) != 1 {
			t.Fatalf("delta = %+v, want one updated item", stats.Delta)
		}
		if stats.Delta.Updated[0] != "DEMO-US-0002" {
			t.Errorf("updated = %v", stats.Delta.Updated)
		}
		item := decode[struct {
			Title string `json:"title"`
		}](t, call(t, v, "item.get", map[string]any{"id": "DEMO-US-0002"}))
		if item.Title != "Save cards" {
			t.Errorf("title = %q, want the applied change", item.Title)
		}
	})

	t.Run("a removal drops the item", func(t *testing.T) {
		stats := decode[IndexStats](t, call(t, v, "vault.apply", map[string]any{
			"events": []map[string]any{{"op": "remove", "path": target}},
		}))
		if stats.Items != before.Items-1 {
			t.Errorf("items = %d, want %d", stats.Items, before.Items-1)
		}
		if stats.Delta == nil || len(stats.Delta.Removed) != 1 {
			t.Errorf("delta = %+v, want one removed item", stats.Delta)
		}
	})

	t.Run("an unknown op is rejected", func(t *testing.T) {
		env := rawCall(t, v, "vault.apply", map[string]any{
			"events": []map[string]any{{"op": "touch", "path": target}},
		})
		if env.OK || env.Error.Code != "invalid_request" {
			t.Errorf("got ok=%v code=%q, want invalid_request", env.OK, env.Error.Code)
		}
	})
}

func TestVaultParseSerializeValidate(t *testing.T) {
	v, _ := loadedVault(t)

	t.Run("parse and serialize round-trip", func(t *testing.T) {
		text := "---\nid: DEMO-US-0003\ntype: story\ntitle: Round trip\nstatus: todo\n---\n\n## Description\n\nBody.\n"
		parsed := decode[map[string]any](t, call(t, v, "item.parse", map[string]any{
			"path": "docs/.pmngr/stories/DEMO-US-0003-round-trip.md", "text": text,
		}))
		if parsed["id"] != "DEMO-US-0003" {
			t.Fatalf("parsed = %v", parsed)
		}
		serialized := decode[struct {
			Text string `json:"text"`
		}](t, call(t, v, "item.serialize", map[string]any{"item": parsed}))
		if !strings.Contains(serialized.Text, "id: DEMO-US-0003") ||
			!strings.Contains(serialized.Text, "## Description") {
			t.Errorf("serialized text lost data:\n%s", serialized.Text)
		}
	})

	t.Run("a file without front matter reports its code", func(t *testing.T) {
		env := rawCall(t, v, "item.parse", map[string]any{"path": "a.md", "text": "no front matter\n"})
		if env.OK || env.Error.Code != "invalid_front_matter" {
			t.Errorf("got ok=%v code=%q", env.OK, env.Error.Code)
		}
	})

	t.Run("validate an indexed item", func(t *testing.T) {
		diags := decode[[]map[string]any](t, call(t, v, "item.validate", map[string]any{"id": "DEMO-US-0001"}))
		if len(diags) != 0 {
			t.Errorf("the fixture must validate clean: %+v", diags)
		}
	})

	t.Run("validate rejects an unknown status", func(t *testing.T) {
		text := "---\nid: DEMO-US-0004\ntype: story\ntitle: Bad status\nstatus: reviewing\n---\n\nBody.\n"
		diags := decode[[]struct {
			Code string `json:"code"`
		}](t, call(t, v, "item.validate", map[string]any{
			"path": "docs/.pmngr/stories/DEMO-US-0004-bad-status.md", "text": text,
		}))
		if len(diags) == 0 {
			t.Fatal("an unknown status must be reported")
		}
		found := false
		for _, d := range diags {
			if d.Code == "E-STATUS-UNKNOWN" {
				found = true
			}
		}
		if !found {
			t.Errorf("diagnostics = %+v, want E-STATUS-UNKNOWN", diags)
		}
	})
}

func TestVaultLifecycleMethods(t *testing.T) {
	v := NewInMemory()

	ping := decode[struct {
		Pong bool `json:"pong"`
		WASM bool `json:"wasm"`
	}](t, call(t, v, "ping", nil))
	if !ping.Pong || !ping.WASM {
		t.Errorf("ping = %+v", ping)
	}

	build := decode[struct {
		Protocol int    `json:"protocol"`
		Core     string `json:"core"`
	}](t, call(t, v, "version", nil))
	if build.Protocol != ProtocolVersion || build.Core == "" {
		t.Errorf("version = %+v", build)
	}

	env := rawCall(t, v, "nope", nil)
	if env.OK || env.Error.Code != "unknown_method" {
		t.Errorf("got ok=%v code=%q, want unknown_method", env.OK, env.Error.Code)
	}

	t.Run("an empty vault answers without files", func(t *testing.T) {
		stats := decode[IndexStats](t, call(t, v, "vault.stats", nil))
		if stats.Items != 0 || stats.Projects != 0 {
			t.Errorf("stats = %+v, want an empty vault", stats)
		}
		page := decode[itemPage](t, call(t, v, "item.list", map[string]any{}))
		if page.Total != 0 || page.Items == nil {
			t.Errorf("item.list = %+v, want an empty array", page)
		}
	})
}

func TestVaultSetVersion(t *testing.T) {
	v := NewInMemory()
	build := func() string {
		return decode[struct {
			Core string `json:"core"`
		}](t, call(t, v, "version", nil)).Core
	}
	if got := build(); got != version {
		t.Errorf("core = %q, want the package default %q", got, version)
	}
	v.SetVersion("1.2.3")
	if got := build(); got != "1.2.3" {
		t.Errorf("core = %q, want the injected build", got)
	}
	v.SetVersion("")
	if got := build(); got != version {
		t.Errorf("core = %q, want the package default back", got)
	}
}

func TestAsError(t *testing.T) {
	v, _ := loadedVault(t)

	t.Run("nil is not an error", func(t *testing.T) {
		if e, ok := AsError(nil); ok || e != nil {
			t.Errorf("AsError(nil) = %+v, %v", e, ok)
		}
	})

	t.Run("a vault error keeps its code and path", func(t *testing.T) {
		_, err := v.Dispatch(context.Background(), "kb.page", []byte(`{"path":"docs/nope.md"}`))
		e, ok := AsError(err)
		if !ok {
			t.Fatalf("AsError(%v) reported no error", err)
		}
		if e.Code != "not_found" || e.Path != "docs/nope.md" {
			t.Errorf("error = %+v, want not_found on docs/nope.md", e)
		}
	})

	t.Run("a core error is classified", func(t *testing.T) {
		params := []byte(`{"id":"DEMO-US-0001","rev":"sha256:0000000000000000","patch":{"set":{"title":"Nope"}}}`)
		_, err := v.Dispatch(context.Background(), "item.update", params)
		e, ok := AsError(err)
		if !ok {
			t.Fatalf("AsError(%v) reported no error", err)
		}
		if e.Code != core.StaleRevisionCode {
			t.Errorf("code = %q, want %q", e.Code, core.StaleRevisionCode)
		}
		if e.Path == "" {
			t.Error("a stale revision must name the file it is about")
		}
	})

	t.Run("an unknown failure is internal", func(t *testing.T) {
		e, ok := AsError(errors.New("boom"))
		if !ok || e.Code != "internal" || e.Message != "boom" {
			t.Errorf("error = %+v, %v", e, ok)
		}
	})
}
