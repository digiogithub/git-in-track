package core

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExternalRefNormalisation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		in     External
		want   ExternalRef
		valid  bool
		render string
	}{
		{
			name:   "system is lower-cased and both fields trimmed",
			in:     External{System: "  YouTrack ", ID: " PRJ-42 "},
			want:   ExternalRef{System: "youtrack", ID: "PRJ-42"},
			valid:  true,
			render: "youtrack:PRJ-42",
		},
		{
			name:   "the external id keeps its case",
			in:     External{System: "plane", ID: "AbCd"},
			want:   ExternalRef{System: "plane", ID: "AbCd"},
			valid:  true,
			render: "plane:AbCd",
		},
		{
			name:   "an unknown system is a valid system",
			in:     External{System: "some-future-tracker.v2", ID: "1"},
			want:   ExternalRef{System: "some-future-tracker.v2", ID: "1"},
			valid:  true,
			render: "some-future-tracker.v2:1",
		},
		{
			name:  "without an id it is not usable",
			in:    External{System: "youtrack"},
			want:  ExternalRef{System: "youtrack"},
			valid: false,
		},
		{
			name:  "without a system it is not usable",
			in:    External{ID: "PRJ-42"},
			want:  ExternalRef{ID: "PRJ-42"},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.in.Ref(); got != tt.want {
				t.Errorf("Ref() = %#v, want %#v", got, tt.want)
			}
			if got := tt.in.Valid(); got != tt.valid {
				t.Errorf("Valid() = %t, want %t", got, tt.valid)
			}
			if tt.render != "" && tt.in.String() != tt.render {
				t.Errorf("String() = %q, want %q", tt.in.String(), tt.render)
			}
		})
	}
}

func TestAddAndRemoveExternals(t *testing.T) {
	t.Parallel()

	youtrack := External{System: "youtrack", ID: "PRJ-42", URL: "https://yt.example.com/PRJ-42"}
	plane := External{System: "plane", ID: "9f2b"}

	tests := []struct {
		name   string
		start  []External
		add    []External
		remove []External
		want   []External
	}{
		{
			name: "adding to an empty list",
			add:  []External{youtrack},
			want: []External{youtrack},
		},
		{
			name:  "adding a second system does not touch the first",
			start: []External{youtrack},
			add:   []External{plane},
			want:  []External{youtrack, plane},
		},
		{
			name:  "re-adding the same pair updates in place, never duplicates",
			start: []External{youtrack, plane},
			add: []External{{
				System: "YOUTRACK", ID: "PRJ-42",
				URL:      "https://yt.example.com/issue/PRJ-42",
				SyncedAt: NewTimestamp(time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)),
			}},
			want: []External{
				{
					System: "youtrack", ID: "PRJ-42",
					URL:      "https://yt.example.com/issue/PRJ-42",
					SyncedAt: NewTimestamp(time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)),
				},
				plane,
			},
		},
		{
			name:  "re-adding without a url keeps the url somebody else recorded",
			start: []External{youtrack},
			add:   []External{{System: "youtrack", ID: "PRJ-42", Key: "PRJ"}},
			want:  []External{{System: "youtrack", ID: "PRJ-42", URL: youtrack.URL, Key: "PRJ"}},
		},
		{
			name:  "an entry without an id is ignored rather than appended",
			start: []External{youtrack},
			add:   []External{{System: "jira"}},
			want:  []External{youtrack},
		},
		{
			name:   "removing one pair leaves the others",
			start:  []External{youtrack, plane},
			remove: []External{{System: "YouTrack", ID: "PRJ-42"}},
			want:   []External{plane},
		},
		{
			name:   "removing a system without an id unlinks every reference of it",
			start:  []External{youtrack, {System: "youtrack", ID: "PRJ-43"}, plane},
			remove: []External{{System: "youtrack"}},
			want:   []External{plane},
		},
		{
			name:   "removing the last entry leaves nil, not an empty list",
			start:  []External{plane},
			remove: []External{plane},
			want:   nil,
		},
		{
			name:   "add and remove in the same patch are applied in that order",
			start:  []External{youtrack},
			add:    []External{plane},
			remove: []External{{System: "youtrack", ID: "PRJ-42"}},
			want:   []External{plane},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := addExternals(append([]External(nil), tt.start...), tt.add)
			got = removeExternals(got, tt.remove)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestValidateExternal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		list  []External
		codes []Code
	}{
		{
			name: "a well-formed reference produces nothing",
			list: []External{{System: "youtrack", ID: "PRJ-42", URL: "https://yt.example.com/PRJ-42"}},
		},
		{
			name: "an unknown system is accepted",
			list: []External{{System: "some-future-tracker", ID: "1"}},
		},
		{
			name:  "a missing system is an error",
			list:  []External{{ID: "PRJ-42"}},
			codes: []Code{CodeExternalFields},
		},
		{
			name:  "a missing id is an error",
			list:  []External{{System: "youtrack"}},
			codes: []Code{CodeExternalFields},
		},
		{
			name:  "an empty entry is one error, not two",
			list:  []External{{}},
			codes: []Code{CodeExternalFields},
		},
		{
			name:  "a system that is not a short token is an error",
			list:  []External{{System: "You Track!", ID: "1"}},
			codes: []Code{CodeExternalFields},
		},
		{
			name:  "an id longer than the bound is an error",
			list:  []External{{System: "youtrack", ID: strings.Repeat("x", maxExternalIDBytes+1)}},
			codes: []Code{CodeExternalFields},
		},
		{
			name:  "a url that is not http(s) is a warning",
			list:  []External{{System: "youtrack", ID: "PRJ-42", URL: "ftp://yt.example.com/PRJ-42"}},
			codes: []Code{CodeWarnExternalURL},
		},
		{
			name: "the same pair twice is a warning",
			list: []External{
				{System: "youtrack", ID: "PRJ-42"},
				{System: "YouTrack", ID: "PRJ-42"},
			},
			codes: []Code{CodeWarnExternalDup},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &diagSet{path: "stories/ACME-US-0001-x.md"}
			validateExternal(d, "external", tt.list)
			got := make([]Code, 0, len(d.out))
			for _, diag := range d.out {
				got = append(got, diag.Code)
			}
			if !reflect.DeepEqual(got, tt.codes) && (len(got) != 0 || len(tt.codes) != 0) {
				t.Errorf("codes = %v, want %v (%v)", got, tt.codes, d.out)
			}
		})
	}
}

func TestParseItemExternal(t *testing.T) {
	t.Parallel()

	src := []byte(`---
id: ACME-US-0042
type: story
title: Mirror
status: todo
external:
  - { system: YouTrack, id: PRJ-42, url: "https://yt.example.com/PRJ-42", key: PRJ, synced_at: 2026-09-02T10:00:00Z }
  - { system: plane, id: 9f2b }
---

Body.
`)
	it, err := ParseItem("stories/ACME-US-0042-mirror.md", src)
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	if len(it.External) != 2 {
		t.Fatalf("external = %#v", it.External)
	}
	if it.External[0].Ref() != (ExternalRef{System: "youtrack", ID: "PRJ-42"}) {
		t.Errorf("first ref = %#v", it.External[0].Ref())
	}
	if it.External[0].Key != "PRJ" || it.External[0].SyncedAt.IsZero() {
		t.Errorf("optional fields lost: %#v", it.External[0])
	}
	if _, ok := it.Extra["external"]; ok {
		t.Error("external must be a first-class field, not an unknown key")
	}

	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	again, err := ParseItem("stories/ACME-US-0042-mirror.md", out)
	if err != nil {
		t.Fatalf("re-parse:\n%s\n%v", out, err)
	}
	twice, err := SerializeItem(again)
	if err != nil {
		t.Fatalf("re-serialize: %v", err)
	}
	if string(twice) != string(out) {
		t.Errorf("not idempotent\n--- once ---\n%s\n--- twice ---\n%s", out, twice)
	}
}

func TestParseItemExternalRejectsHalfEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
		code Code
	}{
		{"no id", "external:\n  - { system: youtrack }\n", CodeExternalFields},
		{"no system", "external:\n  - { id: PRJ-42 }\n", CodeExternalFields},
		{"not a list", "external: youtrack\n", CodeFieldType},
		{"element is not a mapping", "external:\n  - youtrack\n", CodeFieldType},
		{"bad synced_at", "external:\n  - { system: youtrack, id: X, synced_at: yesterday }\n", CodeDateFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := []byte("---\nid: ACME-US-0042\ntype: story\ntitle: Mirror\nstatus: todo\n" + tt.yaml + "---\n")
			_, err := ParseItem("stories/ACME-US-0042-mirror.md", src)
			if err == nil {
				t.Fatalf("want a parse error for %s", tt.yaml)
			}
			if !strings.Contains(err.Error(), string(tt.code)) {
				t.Errorf("error = %v, want code %s", err, tt.code)
			}
		})
	}
}

func TestParseCommentExternalRoundTrip(t *testing.T) {
	t.Parallel()

	src := []byte(`---
type: comment
item: ACME-US-0042
author: marta
created: 2026-09-01T10:45:12Z
external:
  - { system: youtrack, id: "4-1234", url: https://yt.example.com/issue/PRJ-42#comment-4-1234 }
---

Mirrored comment.
`)
	c, err := ParseComment("comments/ACME-US-0042/20260901T104512Z-marta.md", src)
	if err != nil {
		t.Fatalf("ParseComment(): %v", err)
	}
	if len(c.External) != 1 || c.External[0].ID != "4-1234" {
		t.Fatalf("external = %#v", c.External)
	}
	out, err := SerializeComment(c)
	if err != nil {
		t.Fatalf("SerializeComment(): %v", err)
	}
	if !strings.Contains(string(out), "external:\n  - { system: youtrack, id: 4-1234, url: ") {
		t.Errorf("external not emitted in canonical form:\n%s", out)
	}
	again, err := ParseComment("comments/ACME-US-0042/20260901T104512Z-marta.md", out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if !reflect.DeepEqual(again.External, c.External) {
		t.Errorf("round trip lost data: %#v vs %#v", again.External, c.External)
	}
}

func TestPageExternalRefs(t *testing.T) {
	t.Parallel()

	page := ParsePage("docs/architecture/sso.md", "architecture/sso.md", []byte(`---
title: SSO
external:
  - { system: confluence, id: "12345", url: https://wiki.example.com/12345 }
  - { system: broken }
---

See <https://example.com/spec>.
`))
	if len(page.ExternalRefs) != 1 {
		t.Fatalf("externalRefs = %#v", page.ExternalRefs)
	}
	if page.ExternalRefs[0].Ref() != (ExternalRef{System: "confluence", ID: "12345"}) {
		t.Errorf("ref = %#v", page.ExternalRefs[0])
	}
	// The body scan is a different field and must be unaffected.
	if len(page.External) != 1 || page.External[0] != "https://example.com/spec" {
		t.Errorf("body external urls = %#v", page.External)
	}
}

func TestExternalCanonicalKeyOrder(t *testing.T) {
	t.Parallel()

	// The full order is pinned here so that adding a key is a deliberate act.
	want := []string{
		"id", "type", "item", "title", "status", "priority",
		"parent", "epic", "milestone", "sprint",
		"assignees", "author", "owner", "labels",
		"estimate", "effort", "spent",
		"created", "updated", "started", "closed", "start", "due",
		"links", "blocks", "depends_on", "in_reply_to", "kind", "reactions",
		"external", "attachments", "custom", "inbox", "deleted",
	}
	if !reflect.DeepEqual(canonicalKeyOrder, want) {
		t.Errorf("canonicalKeyOrder = %v\nwant %v", canonicalKeyOrder, want)
	}
	for _, key := range want {
		if !knownKeys[key] {
			t.Errorf("%q is in the canonical order but not in knownKeys", key)
		}
	}
}

func TestStoreExternalPatchSemantics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _, _ := newTestStore(t)

	created, err := store.Create(ctx, ItemDraft{
		Type: TypeStory, Title: "Mirror the tracker", Status: "todo",
		External: []External{{System: "YouTrack", ID: " PRJ-42 ", URL: "https://yt.example.com/PRJ-42"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(created.External) != 1 || created.External[0].Ref().ID != "PRJ-42" {
		t.Fatalf("draft external = %#v", created.External)
	}

	// A patch that only adds a reference must leave everything else alone.
	added, err := store.Update(ctx, created.ID, ItemPatch{
		AddExternal: []External{{System: "plane", ID: "9f2b"}},
	}, created.Rev)
	if err != nil {
		t.Fatalf("Update(add): %v", err)
	}
	if len(added.External) != 2 {
		t.Fatalf("external = %#v", added.External)
	}
	if added.Title != created.Title || added.Status != created.Status {
		t.Errorf("an external-only patch touched another field: %#v", added)
	}

	// Re-adding the same pair refreshes it instead of appending.
	refreshed, err := store.Update(ctx, added.ID, ItemPatch{
		AddExternal: []External{{System: "youtrack", ID: "PRJ-42", Key: "PRJ"}},
	}, added.Rev)
	if err != nil {
		t.Fatalf("Update(re-add): %v", err)
	}
	if len(refreshed.External) != 2 {
		t.Fatalf("re-adding duplicated: %#v", refreshed.External)
	}
	if refreshed.External[0].Key != "PRJ" || refreshed.External[0].URL == "" {
		t.Errorf("re-add lost or failed to merge fields: %#v", refreshed.External[0])
	}

	removed, err := store.Update(ctx, refreshed.ID, ItemPatch{
		RemoveExternal: []External{{System: "youtrack", ID: "PRJ-42"}},
	}, refreshed.Rev)
	if err != nil {
		t.Fatalf("Update(remove): %v", err)
	}
	if len(removed.External) != 1 || removed.External[0].Ref().System != "plane" {
		t.Errorf("external = %#v", removed.External)
	}

	cleared, err := store.Update(ctx, removed.ID, ItemPatch{Unset: []string{"external"}}, removed.Rev)
	if err != nil {
		t.Fatalf("Update(unset): %v", err)
	}
	if cleared.External != nil {
		t.Errorf("unset external = %#v", cleared.External)
	}
}

func TestStoreExternalConcurrentPatchesDoNotClobber(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _, _ := newTestStore(t)
	created, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Mirror", Status: "todo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := store.Update(ctx, created.ID,
		ItemPatch{AddExternal: []External{{System: "youtrack", ID: "PRJ-42"}}}, created.Rev)
	if err != nil {
		t.Fatalf("Update(first): %v", err)
	}
	// The second writer read the item before the first wrote, but only touches
	// its own system, so after re-reading the rev both references survive.
	second, err := store.Update(ctx, created.ID,
		ItemPatch{AddExternal: []External{{System: "plane", ID: "9f2b"}}}, first.Rev)
	if err != nil {
		t.Fatalf("Update(second): %v", err)
	}
	systems := ExternalSystems(second.External)
	if !reflect.DeepEqual(systems, []string{"plane", "youtrack"}) {
		t.Errorf("systems = %v, want both", systems)
	}
}

func TestIndexLookupByExternal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fsys := NewMemFS()
	ref, err := CreateProject(fsys, "docs", NewProject{Key: "ACME"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	store := NewStore(fsys, ref.BacklogPath, ref.Config)
	created, err := store.Create(ctx, ItemDraft{
		Type: TypeStory, Title: "Mirror", Status: "backlog",
		External: []External{{System: "youtrack", ID: "PRJ-42"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ix := NewIndex(fsys, []ProjectRef{ref})
	if _, err := ix.Build(ctx, true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got, ok := ix.ItemIDByExternal("YouTrack", "PRJ-42"); !ok || got != created.ID {
		t.Fatalf("ItemIDByExternal = %q, %t", got, ok)
	}
	if _, ok := ix.ItemIDByExternal("youtrack", "PRJ-99"); ok {
		t.Error("an unknown pair must not resolve")
	}
	if item, ok := ix.ItemByExternal("youtrack", "PRJ-42"); !ok || item.Title != "Mirror" {
		t.Errorf("ItemByExternal = %#v, %t", item, ok)
	}

	page, err := ix.Items(ctx, Filter{ExternalSystem: "youtrack"})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if page.Total != 1 || page.Items[0].ID != created.ID {
		t.Errorf("filter by system = %#v", page.Items)
	}
	page, err = ix.Items(ctx, Filter{ExternalSystem: "youtrack", ExternalID: "PRJ-99"})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if page.Total != 0 {
		t.Errorf("filter by an unknown id must match nothing, got %d", page.Total)
	}

	// Incremental updates: change the reference, then delete the file.
	updated, err := store.Update(ctx, created.ID, ItemPatch{
		External: &[]External{{System: "youtrack", ID: "PRJ-77"}},
	}, created.Rev)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := ix.ApplyFileEvents(ctx, []FileEvent{{Kind: FileModified, Path: updated.Path}}); err != nil {
		t.Fatalf("ApplyFileEvents(modify): %v", err)
	}
	if _, ok := ix.ItemIDByExternal("youtrack", "PRJ-42"); ok {
		t.Error("the old pair must be gone after an incremental update")
	}
	if got, ok := ix.ItemIDByExternal("youtrack", "PRJ-77"); !ok || got != created.ID {
		t.Errorf("the new pair must resolve, got %q %t", got, ok)
	}

	if err := fsys.Remove(updated.Path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := ix.ApplyFileEvents(ctx, []FileEvent{{Kind: FileRemoved, Path: updated.Path}}); err != nil {
		t.Fatalf("ApplyFileEvents(remove): %v", err)
	}
	if _, ok := ix.ItemIDByExternal("youtrack", "PRJ-77"); ok {
		t.Error("a deleted item must not keep its external reference in the index")
	}
}

func TestExternalGoldenFixtureIsCanonical(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"external-story.md", "inbox-item.md"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", "golden", name))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			it, err := ParseItem("stories/"+name, data)
			if err != nil {
				t.Fatalf("ParseItem(): %v", err)
			}
			out, err := SerializeItem(it)
			if err != nil {
				t.Fatalf("SerializeItem(): %v", err)
			}
			if string(out) != string(data) {
				t.Errorf("the golden file is not a fixed point\n--- got ---\n%s\n--- want ---\n%s", out, data)
			}
		})
	}
}
