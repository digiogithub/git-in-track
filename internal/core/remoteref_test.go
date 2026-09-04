package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// snapshotFixture is the committed snapshot of the project the fixture team
// declares but nobody clones.
const snapshotFixture = "../../testdata/fixtures/team-basic/.pmngr/index/WEB.json"

// fixtureNow is a fixed instant a few hours after the fixture snapshot was
// generated, so that staleness is a decision of the test and not of the clock.
var fixtureNow = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// snapshotFS mounts the fixture team repository in memory, so that a test can
// break one file without touching the fixture on disk.
func snapshotFS(t *testing.T, files map[string]string) *MemFS {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(snapshotFixture))
	if err != nil {
		t.Fatalf("read the fixture snapshot: %v", err)
	}
	seed := map[string]string{".pmngr/index/WEB.json": string(data)}
	for p, content := range files {
		seed[p] = content
	}
	return NewMemFSFromMap(seed)
}

// freshPolicy is the snapshot policy of the fixture team.
func freshPolicy() SnapshotPolicy { return SnapshotPolicy{Enabled: true, MaxAgeDays: 7} }

func TestReadSnapshots(t *testing.T) {
	tests := []struct {
		name   string
		files  map[string]string
		policy SnapshotPolicy
		now    time.Time
		check  func(t *testing.T, set *SnapshotSet)
	}{
		{
			name:   "a committed snapshot resolves the items of an uncloned project",
			policy: freshPolicy(),
			now:    fixtureNow,
			check: func(t *testing.T, set *SnapshotSet) {
				info := set.Info("WEB")
				if !info.Present || info.Items != 4 || info.GeneratedBy != "marta" {
					t.Fatalf("info = %+v", info)
				}
				if info.Freshness != FreshnessFresh || info.Stale {
					t.Fatalf("freshness = %+v", info)
				}
				it, ok := set.Item("WEB", "WEB-US-0031")
				if !ok || it.Title != "Rewrite the hero section" || it.Status != "next" {
					t.Fatalf("item = %+v (ok=%v)", it, ok)
				}
				if got := set.Config("WEB").CategoryOf("shipped"); got != CategoryDone {
					t.Errorf("category of shipped = %q", got)
				}
			},
		},
		{
			name:   "a snapshot older than the policy is stale but still readable",
			policy: SnapshotPolicy{Enabled: true, MaxAgeDays: 2},
			now:    fixtureNow.Add(72 * time.Hour),
			check: func(t *testing.T, set *SnapshotSet) {
				info := set.Info("WEB")
				if !info.Present || !info.Stale || info.Freshness != FreshnessStale {
					t.Fatalf("info = %+v", info)
				}
				if _, ok := set.Item("WEB", "WEB-T-0007"); !ok {
					t.Error("a stale snapshot still resolves its items")
				}
				if len(set.Diagnostics()) == 0 {
					t.Error("a stale snapshot must be reported")
				}
			},
		},
		{
			name:   "a project with no snapshot degrades to an empty answer",
			files:  map[string]string{},
			policy: freshPolicy(),
			now:    fixtureNow,
			check: func(t *testing.T, set *SnapshotSet) {
				info := set.Info("NONE")
				if info.Present || info.Error != "" {
					t.Fatalf("info = %+v", info)
				}
				if _, ok := set.Snapshot("NONE"); ok {
					t.Error("a missing snapshot must not resolve")
				}
			},
		},
		{
			name:   "a malformed snapshot is an error on that project alone",
			files:  map[string]string{".pmngr/index/BAD.json": "{ this is not json"},
			policy: freshPolicy(),
			now:    fixtureNow,
			check: func(t *testing.T, set *SnapshotSet) {
				info := set.Info("BAD")
				if info.Present || info.Error == "" {
					t.Fatalf("info = %+v", info)
				}
				if !set.Info("WEB").Present {
					t.Error("one broken snapshot must not hide the others")
				}
				diags := set.Diagnostics()
				if len(diags) != 1 || diags[0].Code != CodeSnapMalformed {
					t.Fatalf("diagnostics = %+v", diags)
				}
			},
		},
		{
			name:   "a snapshot of another schema is refused with a clear reason",
			files:  map[string]string{".pmngr/index/BAD.json": `{"schema": 99, "items": []}`},
			policy: freshPolicy(),
			now:    fixtureNow,
			check: func(t *testing.T, set *SnapshotSet) {
				if got := set.Info("BAD").Error; !strings.Contains(got, "schema 99") {
					t.Fatalf("error = %q", got)
				}
			},
		},
		{
			name:   "a snapshot whose key disagrees with its file name is reported",
			files:  map[string]string{".pmngr/index/OTHER.json": `{"schema": 1, "project": {"key": "WEB"}, "items": []}`},
			policy: freshPolicy(),
			now:    fixtureNow,
			check: func(t *testing.T, set *SnapshotSet) {
				codes := map[Code]bool{}
				for _, d := range set.Diagnostics() {
					codes[d.Code] = true
				}
				if !codes[CodeSnapKeyMismatch] {
					t.Fatalf("diagnostics = %+v", set.Diagnostics())
				}
			},
		},
		{
			name:   "snapshots turned off leave every remote card bare",
			policy: SnapshotPolicy{Enabled: false},
			now:    fixtureNow,
			check: func(t *testing.T, set *SnapshotSet) {
				if _, ok := set.Snapshot("WEB"); ok {
					t.Error("no file is read when the team disables snapshots (R-SNAP-10)")
				}
				if info := set.Info("WEB"); info.Enabled || info.Present {
					t.Fatalf("info = %+v", info)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsys := snapshotFS(t, tc.files)
			keys := []ProjectKey{"WEB", "BAD", "OTHER", "NONE"}
			set := ReadSnapshots(fsys, ".pmngr", keys, tc.policy, tc.now)
			tc.check(t, set)
		})
	}
}

func TestSnapshotFixtureIsStable(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash(snapshotFixture))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	snap, err := DecodeProjectSnapshot(data)
	if err != nil {
		t.Fatalf("DecodeProjectSnapshot: %v", err)
	}
	again, err := EncodeProjectSnapshot(*snap)
	if err != nil {
		t.Fatalf("EncodeProjectSnapshot: %v", err)
	}
	if *update {
		if err := os.WriteFile(filepath.FromSlash(snapshotFixture), again, 0o644); err != nil {
			t.Fatalf("rewrite the fixture: %v", err)
		}
		t.Logf("fixture %s rewritten", snapshotFixture)
		data = again
	}
	if !bytes.Equal(data, again) {
		t.Errorf("the committed snapshot is not what this build writes\n--- re-encoded ---\n%s", again)
	}
	t.Run("regeneration with a new timestamp is not a content change", func(t *testing.T) {
		other := *snap
		other.Items = append([]ProjectSnapshotItem(nil), snap.Items...)
		other.Generated = NewTimestamp(fixtureNow)
		other.GeneratedBy = "someone-else"
		other.Generator = "gintrack/9.9.9"
		other.Source = &SnapshotSource{Commit: "0000", Dirty: true}
		if !SameSnapshotContent(*snap, other) {
			t.Error("only the volatile fields changed; the file must not be rewritten")
		}
		other.Items[0].Title = "Something else"
		if SameSnapshotContent(*snap, other) {
			t.Error("a changed title is a content change")
		}
	})
}

func TestTeamProjectFileURL(t *testing.T) {
	const item = "documentation/.pmngr/stories/WEB-US-0031-rewrite-the-hero-section.md"

	tests := []struct {
		name    string
		project TeamProject
		path    string
		want    string
	}{
		{
			name:    "github",
			project: TeamProject{Repo: "https://github.com/acme/platform.git", WebURL: "https://github.com/acme/platform"},
			path:    "docs/.pmngr/stories/ACME-US-0042-login-with-sso.md",
			want:    "https://github.com/acme/platform/blob/main/docs/.pmngr/stories/ACME-US-0042-login-with-sso.md",
		},
		{
			name: "gitlab with a branch that needs escaping",
			project: TeamProject{
				Host: "gitlab", WebURL: "https://gitlab.com/example/website", DefaultBranch: "feature/x",
			},
			path: item,
			want: "https://gitlab.com/example/website/-/blob/feature%2Fx/" + item,
		},
		{
			name:    "gitea",
			project: TeamProject{Host: "gitea", WebURL: "https://git.acme.example/mobile/app", DefaultBranch: "develop"},
			path:    "docs/.pmngr/tasks/MOB-T-0012-crash.md",
			want:    "https://git.acme.example/mobile/app/src/branch/develop/docs/.pmngr/tasks/MOB-T-0012-crash.md",
		},
		{
			name:    "bitbucket cloud",
			project: TeamProject{Repo: "https://bitbucket.org/acme/legacy.git", DefaultBranch: "master"},
			path:    "docs/.pmngr/stories/LEGACY-US-0004-invoice-pdf.md",
			want:    "https://bitbucket.org/acme/legacy/src/master/docs/.pmngr/stories/LEGACY-US-0004-invoice-pdf.md",
		},
		{
			name:    "bitbucket data center",
			project: TeamProject{Host: "bitbucket-server", WebURL: "https://git.acme.example/projects/ACME/repos/legacy", DefaultBranch: "master"},
			path:    "docs/a.md",
			want:    "https://git.acme.example/projects/ACME/repos/legacy/browse/docs/a.md?at=refs%2Fheads%2Fmaster",
		},
		{
			name:    "an ssh remote with no web_url builds no link (R-URL-3)",
			project: TeamProject{Repo: "git@git.acme.example:acme/platform.git"},
			path:    item,
			want:    "",
		},
		{
			name:    "a generic host shows the path as text instead of guessing",
			project: TeamProject{Host: "generic", WebURL: "https://git.acme.example/acme/platform"},
			path:    item,
			want:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.project.FileURL(tc.path); got != tc.want {
				t.Errorf("FileURL = %q, want %q", got, tc.want)
			}
			if got := tc.project.FileURL(""); got != "" {
				t.Errorf("an empty path must build no link, got %q", got)
			}
		})
	}
}
