package vault

import (
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

const memoryProjectYAML = `schema: 2
key: ACME
name: Acme
docs: {path: docs}
workflow:
  initial: backlog
  statuses:
    - {id: triage, category: triage}
    - {id: backlog, category: todo}
    - {id: done, category: done}
`

var memoryClock = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func memoryVault(t *testing.T, extra ...map[string]string) *Vault {
	t.Helper()
	v := NewInMemory()
	v.SetClock(func() time.Time { return memoryClock })
	files := []map[string]string{{"path": "docs/.pmngr/project.yaml", "text": memoryProjectYAML}}
	files = append(files, extra...)
	call(t, v, "vault.load", map[string]any{"files": files})
	return v
}

func put(t *testing.T, v *Vault, project string, items ...map[string]any) MemoryPutResult {
	t.Helper()
	return decode[MemoryPutResult](t, call(t, v, "memory.put", map[string]any{"project": project, "items": items}))
}

// memoryFiles counts the memory files a write set wrote; the allocator may
// also refresh the counter hints of project.yaml.
func memoryFiles(w WriteSet) int {
	n := 0
	for _, f := range w.Written {
		if strings.Contains(f.Path, "/memory/") || strings.HasPrefix(f.Path, ".pmngr/memory/") {
			n++
		}
	}
	return n
}

func changes(t *testing.T, v *Vault, since int64) MemoryChanges {
	t.Helper()
	return decode[MemoryChanges](t, call(t, v, "memory.changes", map[string]any{"project": "ACME", "since": since}))
}

func getMemory(t *testing.T, v *Vault, key string) core.Item {
	t.Helper()
	return decode[core.Item](t, call(t, v, "memory.get", map[string]any{"project": "ACME", "key": key}))
}

func TestMemoryPut(t *testing.T) {
	t.Run("a new key is created as a proposal with the server's hash", func(t *testing.T) {
		v := memoryVault(t)
		out := put(t, v, "ACME", map[string]any{"key": "billing", "content": "Customer X is invoiced quarterly.\n"})
		if out.Scope != "project/acme" || len(out.Results) != 1 {
			t.Fatalf("result = %+v", out)
		}
		r := out.Results[0]
		if r.Outcome != MemoryCreated || r.ID != "ACME-MEM-0001" || r.Version != 1 {
			t.Fatalf("result = %+v", r)
		}
		it := getMemory(t, v, "billing")
		want := core.MemoryContentHash("fact", "project/acme", "billing", "", "Customer X is invoiced quarterly.\n")
		if it.Status != core.MemoryProposed || it.Memory.ContentHash != want || it.Memory.LocalID != "project/acme/billing" {
			t.Errorf("memory = %s %+v", it.Status, it.Memory)
		}
		if it.Title != "Customer X is invoiced quarterly." {
			t.Errorf("title = %q, want the first line of the content", it.Title)
		}
		if !strings.HasPrefix(it.Path, "docs/.pmngr/memory/ACME-MEM-0001-") {
			t.Errorf("path = %q", it.Path)
		}
		if memoryFiles(out.Writes) != 1 {
			t.Errorf("writes = %+v", out.Writes)
		}
	})

	t.Run("the same content again is a duplicate and writes nothing", func(t *testing.T) {
		v := memoryVault(t)
		put(t, v, "ACME", map[string]any{"key": "billing", "content": "x"})
		out := put(t, v, "ACME", map[string]any{"key": "billing", "content": "x"})
		if r := out.Results[0]; r.Outcome != MemoryDuplicate || r.Version != 1 {
			t.Fatalf("result = %+v", r)
		}
		if memoryFiles(out.Writes) != 0 {
			t.Errorf("a duplicate wrote %+v", out.Writes.Written)
		}
	})

	t.Run("new content takes the next version", func(t *testing.T) {
		v := memoryVault(t)
		put(t, v, "ACME", map[string]any{"key": "billing", "content": "x"})
		out := put(t, v, "ACME", map[string]any{"key": "billing", "content": "y"})
		if r := out.Results[0]; r.Outcome != MemoryUpdated || r.Version != 2 {
			t.Fatalf("result = %+v", r)
		}
		if it := getMemory(t, v, "billing"); strings.TrimSpace(it.Body) != "y" {
			t.Errorf("body = %q", it.Body)
		}
	})

	t.Run("a stale base version is refused with the current one", func(t *testing.T) {
		v := memoryVault(t)
		put(t, v, "ACME", map[string]any{"key": "billing", "content": "x"})
		put(t, v, "ACME", map[string]any{"key": "billing", "content": "y"})
		out := put(t, v, "ACME", map[string]any{"key": "billing", "content": "z", "baseVersion": 1})
		if r := out.Results[0]; r.Outcome != MemoryRejected || r.Code != core.StaleRevisionCode || r.Current != 2 {
			t.Fatalf("result = %+v", r)
		}
	})

	t.Run("one bad entry does not stop the batch", func(t *testing.T) {
		v := memoryVault(t)
		out := put(t, v, "ACME",
			map[string]any{"key": "a", "content": "a"},
			map[string]any{"key": "Not A Key", "content": "b"},
			map[string]any{"key": "c", "content": "c", "status": "active"},
		)
		got := []string{out.Results[0].Outcome, out.Results[1].Outcome, out.Results[2].Outcome}
		if strings.Join(got, ",") != "created,rejected,created" {
			t.Fatalf("outcomes = %v", got)
		}
		if out.Results[1].Index != 1 || out.Results[2].Version != 2 {
			t.Errorf("results = %+v", out.Results)
		}
		if memoryFiles(out.Writes) != 2 {
			t.Errorf("one batch writes both files once: %+v", out.Writes.Written)
		}
	})

	t.Run("forgetting an active memory leaves a tombstone; a proposal is withdrawn", func(t *testing.T) {
		v := memoryVault(t)
		put(t, v, "ACME",
			map[string]any{"key": "kept", "content": "a", "status": "active"},
			map[string]any{"key": "draft", "content": "b"},
		)
		out := put(t, v, "ACME", map[string]any{"op": "forget", "key": "kept"}, map[string]any{"op": "forget", "key": "draft"})
		if out.Results[0].Outcome != MemoryForgotten || out.Results[1].Outcome != MemoryForgotten {
			t.Fatalf("results = %+v", out.Results)
		}
		kept, draft := getMemory(t, v, "kept"), getMemory(t, v, "draft")
		if kept.Status != core.MemoryForgotten || kept.Memory.ValidTo.IsZero() {
			t.Errorf("kept = %s %+v", kept.Status, kept.Memory)
		}
		if draft.Status != core.MemoryCancelled {
			t.Errorf("draft = %s, want cancelled", draft.Status)
		}
		feed := changes(t, v, 0)
		if len(feed.Tombstones) != 1 || feed.Tombstones[0].Key != "kept" || len(feed.Records) != 0 {
			t.Errorf("feed = %+v", feed)
		}
	})

	t.Run("supersedes moves the old memory in the same batch", func(t *testing.T) {
		v := memoryVault(t)
		put(t, v, "ACME", map[string]any{"key": "old-rate", "content": "VAT is 18%", "status": "active"})
		out := put(t, v, "ACME", map[string]any{
			"key": "rate", "content": "VAT is 21%", "status": "active", "supersedes": []string{"old-rate"},
		})
		if out.Results[0].Outcome != MemoryCreated {
			t.Fatalf("result = %+v", out.Results[0])
		}
		old := getMemory(t, v, "old-rate")
		if old.Status != core.MemorySuperseded || old.Memory.SupersededBy != out.Results[0].ID {
			t.Errorf("old = %s %+v", old.Status, old.Memory)
		}
		if memoryFiles(out.Writes) != 2 {
			t.Errorf("one batch, both files: %+v", out.Writes.Written)
		}
		newer := getMemory(t, v, "rate")
		if len(newer.Links) != 1 || newer.Links[0].Kind != core.LinkSupersedes || newer.Links[0].Target != string(old.ID) {
			t.Errorf("links = %+v", newer.Links)
		}
	})

	t.Run("an unknown scope is refused", func(t *testing.T) {
		v := memoryVault(t)
		env := rawCall(t, v, "memory.put", map[string]any{"project": "NOPE", "items": []any{}})
		if env.OK || env.Error.Code != "not_found" {
			t.Fatalf("envelope = %+v", env)
		}
	})
}

func TestMemoryChangesFeed(t *testing.T) {
	v := memoryVault(t)
	put(t, v, "ACME",
		map[string]any{"key": "a", "content": "a", "status": "active"}, // version 1
		map[string]any{"key": "p", "content": "p"},                     // version 2, proposed
		map[string]any{"key": "b", "content": "b", "status": "active"}, // version 3
	)
	feed := changes(t, v, 0)
	if len(feed.Records) != 2 || feed.Records[0].Memory.Key != "a" || feed.Records[1].Memory.Key != "b" || feed.Cursor != 3 {
		t.Fatalf("feed = %+v", feed)
	}
	if again := changes(t, v, feed.Cursor); len(again.Records)+len(again.Tombstones) != 0 || again.Cursor != 3 {
		t.Fatalf("nothing new past the cursor, got %+v", again)
	}

	// Accepting the proposal is what first shows it, past the cursor.
	p := getMemory(t, v, "p")
	triage(t, v, map[string]any{"id": p.ID, "rev": p.Rev, "action": "accept"})
	next := changes(t, v, feed.Cursor)
	if len(next.Records) != 1 || next.Records[0].Memory.Key != "p" || next.Records[0].Memory.Version != 4 {
		t.Fatalf("after accept = %+v", next)
	}

	t.Run("versions live in the files and survive a rebuild", func(t *testing.T) {
		files := []map[string]string{}
		for _, it := range []string{"a", "p", "b"} {
			m := getMemory(t, v, it)
			data, err := v.fs.ReadFile(m.Path)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, map[string]string{"path": m.Path, "text": string(data)})
		}
		fresh := memoryVault(t, files...)
		if got := changes(t, fresh, 0); got.Cursor != 4 || len(got.Records) != 3 {
			t.Fatalf("rebuilt feed = %+v", got)
		}
	})
}

func TestMemoryOrganizationScope(t *testing.T) {
	team := func(key string) *Vault {
		v := NewInMemory()
		v.SetClock(func() time.Time { return memoryClock })
		call(t, v, "vault.load", map[string]any{"files": []map[string]string{
			{"path": "team.yaml", "text": "schema: 1\nkey: " + key + "\nname: Ops\nknowledge: {path: knowledge}\n"},
			{"path": "knowledge/README.md", "text": "# Ops\n"},
		}})
		return v
	}
	t.Run("a team whose key forms ids holds organization memory", func(t *testing.T) {
		v := team("OPS")
		out := put(t, v, "OPS", map[string]any{"key": "on-call", "content": "Rotation is weekly.", "status": "active"})
		if out.Scope != "org/ops" || out.Results[0].ID != "OPS-MEM-0001" {
			t.Fatalf("result = %+v", out)
		}
		if got := out.Writes.Written[0].Path; !strings.HasPrefix(got, ".pmngr/memory/OPS-MEM-0001-") {
			t.Errorf("path = %q", got)
		}
	})
	t.Run("a team key the project grammar refuses cannot hold memory", func(t *testing.T) {
		v := team("OPS-TEAM")
		env := rawCall(t, v, "memory.put", map[string]any{"project": "OPS-TEAM", "items": []any{}})
		if env.OK || !strings.Contains(env.Error.Message, "cannot hold organization memory") {
			t.Fatalf("envelope = %+v", env)
		}
	})
}
