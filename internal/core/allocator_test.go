package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// allocTestConfig returns a minimal configuration whose key is ACME and whose
// counters and reserved ranges the caller tunes.
func allocTestConfig() *ProjectConfig {
	cfg := DefaultProjectConfig()
	cfg.Key = "ACME"
	cfg.Name = "ACME Platform"
	cfg.IDAllocation.WriteCounters = false
	return &cfg
}

// allocItemFile renders a valid item file for a fixture vault.
func allocItemFile(id ItemID, t ItemType, title, created string) string {
	return fmt.Sprintf(""+
		"---\n"+
		"id: %s\n"+
		"type: %s\n"+
		"title: %s\n"+
		"status: todo\n"+
		"author: jose\n"+
		"created: %s\n"+
		"updated: %s\n"+
		"---\n"+
		"\n## Description\n\nFixture item.\n", id, t, title, created, created)
}

// allocVault seeds an in-memory vault whose backlog holds the given files,
// keyed by their path under docs/.pmngr/.
func allocVault(files map[string]string) *MemFS {
	seed := make(map[string]string, len(files)+1)
	for p, content := range files {
		seed["docs/.pmngr/"+p] = content
	}
	return NewMemFSFromMap(seed)
}

func TestAllocatorNextUsesTheScan(t *testing.T) {
	t.Parallel()

	fs := allocVault(map[string]string{
		"stories/ACME-US-0001-first.md": allocItemFile("ACME-US-0001", TypeStory, "First", "2026-09-01T09:00:00Z"),
		"stories/ACME-US-0007-later.md": allocItemFile("ACME-US-0007", TypeStory, "Later", "2026-09-01T09:00:00Z"),
		// A soft-deleted item still owns its number (R-ID-3).
		"stories/ACME-US-0009-gone.md": strings.Replace(
			allocItemFile("ACME-US-0009", TypeStory, "Gone", "2026-09-01T09:00:00Z"),
			"updated: 2026-09-01T09:00:00Z\n", "updated: 2026-09-01T09:00:00Z\ndeleted: true\n", 1),
		"tasks/ACME-T-0107-a-task.md": allocItemFile("ACME-T-0107", TypeTask, "A task", "2026-09-01T09:00:00Z"),
	})
	alloc := NewAllocator(fs, "docs/.pmngr", allocTestConfig())

	cases := []struct {
		name string
		typ  ItemType
		want ItemID
	}{
		{"story continues after the soft-deleted one", TypeStory, "ACME-US-0010"},
		{"task continues after the highest task", TypeTask, "ACME-T-0108"},
		{"epic starts at one", TypeEpic, "ACME-EP-0001"},
		{"milestone starts at one", TypeMilestone, "ACME-M-0001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := alloc.Peek(context.Background(), tc.typ)
			if err != nil {
				t.Fatalf("Peek: %v", err)
			}
			if got != tc.want {
				t.Errorf("Peek(%s) = %s, want %s", tc.typ, got, tc.want)
			}
		})
	}
}

func TestAllocatorPeekDoesNotReserve(t *testing.T) {
	t.Parallel()

	alloc := NewAllocator(allocVault(nil), "docs", allocTestConfig())
	first, err := alloc.Peek(context.Background(), TypeStory)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	second, err := alloc.Peek(context.Background(), TypeStory)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if first != second {
		t.Fatalf("Peek reserved: %s then %s", first, second)
	}
	next, err := alloc.Next(context.Background(), TypeStory)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if next != first {
		t.Fatalf("Next = %s, want the peeked %s", next, first)
	}
	after, err := alloc.Next(context.Background(), TypeStory)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if after == next {
		t.Fatalf("Next handed out %s twice", after)
	}
}

func TestAllocatorCounterHint(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"stories/ACME-US-0005-scan.md": allocItemFile("ACME-US-0005", TypeStory, "Scan", "2026-09-01T09:00:00Z"),
	}
	cases := []struct {
		name string
		hint int
		want ItemID
	}{
		{"a hint above the scan wins", 40, "ACME-US-0041"},
		{"a hint below the scan is ignored", 2, "ACME-US-0006"},
		{"a missing hint falls back to the scan", 0, "ACME-US-0006"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := allocTestConfig()
			if tc.hint > 0 {
				cfg.IDAllocation.Counters = map[ItemType]int{TypeStory: tc.hint}
			}
			alloc := NewAllocator(allocVault(files), "docs/.pmngr", cfg)
			got, err := alloc.Next(context.Background(), TypeStory)
			if err != nil {
				t.Fatalf("Next: %v", err)
			}
			if got != tc.want {
				t.Errorf("Next = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestAllocatorSkipsReservedRanges(t *testing.T) {
	t.Parallel()

	cfg := allocTestConfig()
	cfg.IDAllocation.Reserved = map[ItemType][]IDRange{TypeTask: {{From: 200, To: 249}}}

	t.Run("an empty project allocates above the block", func(t *testing.T) {
		alloc := NewAllocator(allocVault(nil), "docs", cfg)
		got, err := alloc.Next(context.Background(), TypeTask)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if got != "ACME-T-0250" {
			t.Errorf("Next = %s, want ACME-T-0250", got)
		}
	})

	t.Run("a scan above the block wins", func(t *testing.T) {
		fs := allocVault(map[string]string{
			"tasks/ACME-T-0300-high.md": allocItemFile("ACME-T-0300", TypeTask, "High", "2026-09-01T09:00:00Z"),
		})
		alloc := NewAllocator(fs, "docs", cfg)
		got, err := alloc.Next(context.Background(), TypeTask)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if got != "ACME-T-0301" {
			t.Errorf("Next = %s, want ACME-T-0301", got)
		}
	})

	t.Run("another type is unaffected", func(t *testing.T) {
		alloc := NewAllocator(allocVault(nil), "docs", cfg)
		got, err := alloc.Next(context.Background(), TypeStory)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if got != "ACME-US-0001" {
			t.Errorf("Next = %s, want ACME-US-0001", got)
		}
	})
}

func TestAllocatorRedirectsParticipateInTheScan(t *testing.T) {
	t.Parallel()

	cfg := allocTestConfig()
	cfg.IDAllocation.Redirects = map[ItemID]ItemID{"ACME-US-0043": "ACME-US-0044"}
	alloc := NewAllocator(allocVault(nil), "docs", cfg)

	got, err := alloc.Next(context.Background(), TypeStory)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got != "ACME-US-0045" {
		t.Errorf("Next = %s, want ACME-US-0045: a renumbered id is never reused", got)
	}
}

func TestAllocatorRangesStrategy(t *testing.T) {
	t.Parallel()

	newCfg := func() *ProjectConfig {
		cfg := allocTestConfig()
		cfg.IDAllocation.Strategy = "ranges"
		cfg.IDAllocation.Ranges = map[string]map[ItemType][]IDRange{
			"jose":  {TypeTask: {{From: 1000, To: 1001}}},
			"marta": {TypeTask: {{From: 2000, To: 2999}}},
		}
		return cfg
	}

	t.Run("a user allocates inside their own block", func(t *testing.T) {
		alloc := NewAllocator(allocVault(nil), "docs", newCfg())
		alloc.SetUser("jose")
		first, err := alloc.Next(context.Background(), TypeTask)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		second, err := alloc.Next(context.Background(), TypeTask)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if first != "ACME-T-1000" || second != "ACME-T-1001" {
			t.Fatalf("got %s then %s, want ACME-T-1000 then ACME-T-1001", first, second)
		}
		if _, err := alloc.Next(context.Background(), TypeTask); !errors.Is(err, ErrIDRangeExhausted) {
			t.Fatalf("third allocation error = %v, want ErrIDRangeExhausted", err)
		}
	})

	t.Run("an existing file inside the block is skipped", func(t *testing.T) {
		fs := allocVault(map[string]string{
			"tasks/ACME-T-2000-taken.md": allocItemFile("ACME-T-2000", TypeTask, "Taken", "2026-09-01T09:00:00Z"),
		})
		alloc := NewAllocator(fs, "docs", newCfg())
		alloc.SetUser("marta")
		got, err := alloc.Next(context.Background(), TypeTask)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if got != "ACME-T-2001" {
			t.Errorf("Next = %s, want ACME-T-2001", got)
		}
	})

	t.Run("a user without a block allocates above every other block", func(t *testing.T) {
		// Somebody else's block must never be raided: reserved ranges take part
		// in max_seen (docs/03 section 4.5).
		alloc := NewAllocator(allocVault(nil), "docs", newCfg())
		alloc.SetUser("bot-ci")
		got, err := alloc.Next(context.Background(), TypeTask)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if got != "ACME-T-3000" {
			t.Errorf("Next = %s, want ACME-T-3000", got)
		}
	})
}

func TestAllocatorConcurrentNextNeverRepeats(t *testing.T) {
	t.Parallel()

	fs := NewMemFSFromMap(map[string]string{
		"docs/.pmngr/project.yaml": "schema: 1\nkey: ACME\nname: ACME\nid_allocation:\n  write_counters: true\n",
	})
	cfg := allocTestConfig()
	cfg.IDAllocation.WriteCounters = true
	alloc := NewAllocator(fs, "docs", cfg)

	const goroutines = 8
	const perGoroutine = 6
	var wg sync.WaitGroup
	results := make(chan ItemID, goroutines*perGoroutine)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				id, err := alloc.Next(context.Background(), TypeStory)
				if err != nil {
					t.Errorf("Next: %v", err)
					return
				}
				results <- id
			}
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[ItemID]bool, goroutines*perGoroutine)
	for id := range results {
		if seen[id] {
			t.Fatalf("%s was handed out twice", id)
		}
		seen[id] = true
	}
	if len(seen) != goroutines*perGoroutine {
		t.Fatalf("got %d ids, want %d", len(seen), goroutines*perGoroutine)
	}
	if _, ok := seen[FormatItemID("ACME", CodeStory, goroutines*perGoroutine)]; !ok {
		t.Errorf("the sequence has a gap: %v", seen)
	}
}

func TestAllocatorWritesCountersPreservingComments(t *testing.T) {
	t.Parallel()

	fs := storeFixtureFS(t, "duplicates")
	cfg := storeFixtureConfig(t, fs)
	if !cfg.IDAllocation.WriteCounters {
		t.Fatal("the fixture must enable write_counters")
	}
	alloc := NewAllocator(fs, "docs/.pmngr", cfg)

	if _, err := alloc.Next(context.Background(), TypeTask); err != nil {
		t.Fatalf("Next: %v", err)
	}
	data, err := fs.ReadFile("docs/.pmngr/project.yaml")
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	out := string(data)
	for _, want := range []string{
		"# Fixture project for the store, allocator and doctor tests.",
		"# hints only, NOT authoritative",
		"task: 108",
		"key: ACME",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("project.yaml lost %q:\n%s", want, out)
		}
	}
	// The rewritten file must still parse into the same configuration.
	reloaded, err := LoadProjectConfig(data)
	if err != nil {
		t.Fatalf("reload project.yaml: %v", err)
	}
	if reloaded.IDAllocation.Counters[TypeTask] != 108 {
		t.Errorf("counter = %d, want 108", reloaded.IDAllocation.Counters[TypeTask])
	}
	if reloaded.Key != "ACME" || len(reloaded.Workflow.Statuses) != 6 {
		t.Errorf("the rewrite damaged the configuration: %+v", reloaded)
	}
	if len(fs.Paths()) != len(storeFixtureFS(t, "duplicates").Paths()) {
		t.Errorf("an atomic write left a temporary file behind: %v", fs.Paths())
	}
}

func TestAllocatorLeavesCountersAloneWhenDisabled(t *testing.T) {
	t.Parallel()

	fs := storeFixtureFS(t, "duplicates")
	cfg := storeFixtureConfig(t, fs)
	cfg.IDAllocation.WriteCounters = false
	before, err := fs.ReadFile("docs/.pmngr/project.yaml")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	alloc := NewAllocator(fs, "docs/.pmngr", cfg)
	if _, err := alloc.Next(context.Background(), TypeTask); err != nil {
		t.Fatalf("Next: %v", err)
	}
	after, err := fs.ReadFile("docs/.pmngr/project.yaml")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("project.yaml was rewritten with write_counters disabled")
	}
}

func TestAllocatorReconcile(t *testing.T) {
	t.Parallel()

	fs := storeFixtureFS(t, "duplicates")
	cfg := storeFixtureConfig(t, fs)
	cfg.IDAllocation.Counters[TypeEpic] = 0 // a counter someone deleted by hand
	alloc := NewAllocator(fs, "docs/.pmngr", cfg)

	rec, err := alloc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rec.Scanned[TypeEpic] != 1 || rec.Counters[TypeEpic] != 1 {
		t.Errorf("epic scanned/counter = %d/%d, want 1/1", rec.Scanned[TypeEpic], rec.Counters[TypeEpic])
	}
	if len(rec.Stale) != 1 || rec.Stale[0] != TypeEpic {
		t.Errorf("stale = %v, want [epic]", rec.Stale)
	}
	if !rec.Written {
		t.Error("the repaired counter was not written")
	}
	if len(rec.Duplicates) != 1 || rec.Duplicates[0].ID != "ACME-US-0043" {
		t.Fatalf("duplicates = %+v, want one for ACME-US-0043", rec.Duplicates)
	}
	data, err := fs.ReadFile("docs/.pmngr/project.yaml")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "epic: 1") {
		t.Errorf("project.yaml keeps a stale counter:\n%s", data)
	}
}

func TestAllocatorRejectsTypesWithoutACode(t *testing.T) {
	t.Parallel()

	alloc := NewAllocator(allocVault(nil), "docs", allocTestConfig())
	if _, err := alloc.Next(context.Background(), TypeComment); !errors.Is(err, ErrNoTypeCode) {
		t.Fatalf("Next(comment) error = %v, want ErrNoTypeCode", err)
	}
}

func TestAllocatorCountsBrokenFiles(t *testing.T) {
	t.Parallel()

	// A file whose front matter does not parse still owns the id its name
	// claims: allocating over it would create a duplicate.
	fs := allocVault(map[string]string{
		"stories/ACME-US-0012-broken.md": "---\nid: ACME-US-0012\ntype: story\ntitle: [unterminated\n",
	})
	alloc := NewAllocator(fs, "docs", allocTestConfig())
	got, err := alloc.Next(context.Background(), TypeStory)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got != "ACME-US-0013" {
		t.Errorf("Next = %s, want ACME-US-0013", got)
	}
}

func TestBacklogDirAcceptsBothForms(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"docs", "docs/", "docs/.pmngr", "./docs/.pmngr"} {
		if got := BacklogDir(in); got != "docs/.pmngr" {
			t.Errorf("BacklogDir(%q) = %q, want docs/.pmngr", in, got)
		}
	}
}

func TestAllocatorParsesRangesFromYAML(t *testing.T) {
	t.Parallel()

	const yaml = `schema: 1
key: ACME
name: ACME Platform
workflow:
  statuses:
    - { id: todo, name: To Do, category: todo }
    - { id: done, name: Done, category: done, terminal: true }
id_allocation:
  strategy: ranges
  write_counters: false
  ranges:
    jose:  { task: [[1000, 1999]] }
    marta: { task: [[2000, 2999]] }
`
	cfg, err := LoadProjectConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	got := cfg.IDAllocation.Ranges["jose"][TypeTask]
	if len(got) != 1 || got[0].From != 1000 || got[0].To != 1999 {
		t.Fatalf("ranges = %+v, want [{1000 1999}]", got)
	}
	alloc := NewAllocator(allocVault(nil), "docs", cfg)
	alloc.SetUser("jose")
	id, err := alloc.Next(context.Background(), TypeTask)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if id != "ACME-T-1000" {
		t.Errorf("Next = %s, want ACME-T-1000", id)
	}
}
