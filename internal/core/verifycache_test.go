package core

import (
	"strings"
	"testing"
	"time"
)

func verifyEntry(t *testing.T, ref string, rev Rev, hours int, result VerifyResult) VerifyEntry {
	t.Helper()
	r, err := ParseRequirementRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	return VerifyEntry{
		Ref: r, Rev: rev, Commit: strings.Repeat("a", 40), Result: result,
		Tests: []VerifyTest{{Test: "src/a_test.go#TestA", Result: string(result)}},
		At:    time.Date(2026, 9, 1, hours, 0, 0, 0, time.UTC),
	}
}

func TestDecodeVerifyCache(t *testing.T) {
	good, err := EncodeVerifyCache([]VerifyEntry{verifyEntry(t, "ACME-SP-0001.R1", "sha256:1", 1, VerifyPass)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		data        string
		wantCorrupt bool
		wantLen     int
	}{
		{"encoded document", string(good), false, 1},
		{"empty document", `{"version":1,"entries":[]}`, false, 0},
		{"not JSON", `{"version":1,`, true, 0},
		{"another version", `{"version":2,"entries":[]}`, true, 0},
		{"no version", `{"entries":[]}`, true, 0},
		{"bad ref", `{"version":1,"entries":[{"ref":"nope","rev":"sha256:1","result":"pass"}]}`, true, 0},
		{"entry without rev is dropped", `{"version":1,"entries":[{"ref":"ACME-SP-0001.R1","result":"pass"}]}`, false, 0},
		{"unknown result is dropped", `{"version":1,"entries":[{"ref":"ACME-SP-0001.R1","rev":"sha256:1","result":"green"}]}`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, corrupt := DecodeVerifyCache([]byte(tc.data))
			if corrupt != tc.wantCorrupt || len(got) != tc.wantLen || got == nil {
				t.Errorf("decode = %+v, corrupt %v; want %d entries, corrupt %v", got, corrupt, tc.wantLen, tc.wantCorrupt)
			}
		})
	}
}

func TestMergeVerifyEntries(t *testing.T) {
	cur := []VerifyEntry{
		verifyEntry(t, "ACME-SP-0001.R1", "sha256:1", 1, VerifyPass),
		verifyEntry(t, "ACME-SP-0001.R2", "sha256:2", 1, VerifyPass),
	}
	fresh := []VerifyEntry{
		verifyEntry(t, "ACME-SP-0001.R1", "sha256:1", 2, VerifyFail), // same rev: replaces
		verifyEntry(t, "ACME-SP-0001.R1", "sha256:9", 3, VerifyPass), // another text: added
	}
	got, st := MergeVerifyEntries(cur, fresh)
	if st.Added != 1 || st.Replaced != 1 || st.Total != 3 {
		t.Errorf("stats = %+v, want 1 added, 1 replaced, 3 total", st)
	}
	if e, ok := LatestVerifyEntry(got, cur[0].Ref, "sha256:1"); !ok || e.Result != VerifyFail {
		t.Errorf("latest R1 at sha256:1 = %+v, %v; want the failing run", e, ok)
	}
	if e, ok := LatestVerifyEntry(got, cur[0].Ref, ""); !ok || e.Rev != "sha256:9" {
		t.Errorf("latest R1 at any rev = %+v, %v; want sha256:9", e, ok)
	}
	if _, ok := LatestVerifyEntry(got, cur[0].Ref, "sha256:7"); ok {
		t.Error("an entry of a rev never recorded was found")
	}

	t.Run("history per requirement is bounded", func(t *testing.T) {
		var many []VerifyEntry
		for i := range 10 {
			many = append(many, verifyEntry(t, "ACME-SP-0001.R1", Rev("sha256:"+string(rune('a'+i))), i, VerifyPass))
		}
		got, st := MergeVerifyEntries(nil, many)
		if len(got) != verifyRevsPerRef || st.Total != verifyRevsPerRef {
			t.Fatalf("kept %d entries, want %d", len(got), verifyRevsPerRef)
		}
		if got[0].Rev != "sha256:j" {
			t.Errorf("newest kept = %s, want the last recorded rev", got[0].Rev)
		}
	})
}

func TestFileVerifyCache(t *testing.T) {
	fs := NewMemFS()
	c := NewFileVerifyCache(fs, "docs")
	if c.Path() != "docs/.pmngr/verify.json" {
		t.Fatalf("path = %s", c.Path())
	}

	t.Run("missing reads as empty", func(t *testing.T) {
		got, corrupt, err := c.Load()
		if err != nil || corrupt || len(got) != 0 {
			t.Errorf("load = %v, %v, %v", got, corrupt, err)
		}
	})

	t.Run("corrupt reads as empty and is rebuilt", func(t *testing.T) {
		if err := fs.WriteFile(c.Path(), []byte("{broken")); err != nil {
			t.Fatal(err)
		}
		got, corrupt, err := c.Load()
		if err != nil || !corrupt || len(got) != 0 {
			t.Fatalf("load = %v, %v, %v; want empty and corrupt", got, corrupt, err)
		}
		st, err := c.Record([]VerifyEntry{verifyEntry(t, "ACME-SP-0001.R1", "sha256:1", 1, VerifyPass)})
		if err != nil || !st.Rebuilt || st.Total != 1 {
			t.Fatalf("record = %+v, %v; want rebuilt with one entry", st, err)
		}
		got, corrupt, err = c.Load()
		if err != nil || corrupt || len(got) != 1 {
			t.Errorf("reload = %v, %v, %v", got, corrupt, err)
		}
		if _, err := fs.Stat(c.Path() + ".tmp"); err == nil {
			t.Error("the temporary file was left behind")
		}
	})
}

func TestMemVerifyCache(t *testing.T) {
	c := NewMemVerifyCache(nil)
	if got, corrupt, err := c.Load(); err != nil || corrupt || len(got) != 0 {
		t.Fatalf("empty load = %v, %v, %v", got, corrupt, err)
	}
	if _, err := c.Record([]VerifyEntry{verifyEntry(t, "ACME-SP-0001.R1", "sha256:1", 1, VerifyPass)}); err != nil {
		t.Fatal(err)
	}
	// The exported document is what the browser keeps in IndexedDB; a new
	// cache over it holds the same entries.
	again := NewMemVerifyCache(c.Export())
	if got, _, _ := again.Load(); len(got) != 1 || got[0].Rev != "sha256:1" {
		t.Errorf("reloaded = %+v", got)
	}
	if _, corrupt, _ := NewMemVerifyCache([]byte("junk")).Load(); !corrupt {
		t.Error("junk is not reported corrupt")
	}
}

// TestIndexIgnoresVerifyCache: verify.json beside index.json is part of the
// backlog layout, not a stray file (R-LOC-5, R-LOC-6).
func TestIndexIgnoresVerifyCache(t *testing.T) {
	fs := NewMemFSFromMap(map[string]string{
		"docs/.pmngr/project.yaml": "key: ACME\nname: Acme\n",
		"docs/.pmngr/verify.json":  `{"version":1,"entries":[]}`,
		"docs/.pmngr/stray.json":   `{}`,
	})
	projects, err := DiscoverProjects(fs, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(fs, projects)
	if _, err := ix.Build(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	var strays []string
	for _, d := range ix.Warnings() {
		if d.Code == idxCodeLayoutStray {
			strays = append(strays, d.Path)
		}
	}
	if strings.Join(strays, ",") != "docs/.pmngr/stray.json" {
		t.Errorf("stray files = %v, want only stray.json", strays)
	}
}
