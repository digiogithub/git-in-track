package vault

import (
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// snapshotClock is a fixed instant a few hours after the fixture snapshot was
// generated, so that a test decides the staleness rather than the wall clock.
var snapshotClock = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// pinned returns a workspace whose vaults share a fixed clock.
func pinned(w *Workspace, at time.Time) *Workspace {
	w.SetClock(func() time.Time { return at })
	return w
}

func TestBoardRendersRemoteCardsFromTheSnapshot(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		pinned(w, snapshotClock)
		view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "delivery"}))

		card, ok := cardByRef(view, "WEB/WEB-US-0031")
		if !ok {
			t.Fatal("the remote story is missing from the board")
		}

		tests := []struct {
			name  string
			check func(t *testing.T)
		}{
			{
				name: "the card carries the fields the snapshot published",
				check: func(t *testing.T) {
					if !card.Remote || card.Source != core.CardSourceSnapshot {
						t.Fatalf("card = %+v", card)
					}
					if card.Title != "Rewrite the hero section" || card.Status != "next" {
						t.Fatalf("card = %+v", card)
					}
					if len(card.Assignees) != 2 || len(card.Labels) != 1 {
						t.Fatalf("card = %+v", card)
					}
				},
			},
			{
				name: "the card is dated, fresh and linked to the git host",
				check: func(t *testing.T) {
					if card.SnapshotAt.IsZero() || card.Stale {
						t.Fatalf("card = %+v", card)
					}
					if !strings.HasPrefix(card.RemoteURL, "https://gitlab.com/example/website/-/blob/main/") {
						t.Fatalf("remote url = %q", card.RemoteURL)
					}
				},
			},
			{
				name: "a live card of the cloned project is untouched",
				check: func(t *testing.T) {
					live, ok := cardByRef(view, "DEMO/DEMO-US-0001")
					if !ok || live.Remote || live.Source != core.CardSourceLive {
						t.Fatalf("card = %+v (ok=%v)", live, ok)
					}
				},
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) { tc.check(t) })
		}
	})
}

func TestRemoteCardWritesAreRefused(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		pinned(w, snapshotClock)

		t.Run("a move between columns is refused with the reason", func(t *testing.T) {
			code, message := wsFail(t, w, "board.move", map[string]any{
				"board": "delivery", "ref": "WEB/WEB-US-0031",
				"toColumn": "done", "position": 0, "force": true,
			})
			if code != RemoteCardCode {
				t.Fatalf("code = %q, want %q (%s)", code, RemoteCardCode, message)
			}
			if !strings.Contains(message, "not cloned") {
				t.Fatalf("message = %q", message)
			}
		})

		t.Run("a re-order inside one column writes the team repository only", func(t *testing.T) {
			result := decode[BoardMoveResult](t, wsCall(t, w, "board.move", map[string]any{
				"board": "delivery", "ref": "WEB/WEB-US-0031",
				"toColumn": "todo", "position": 0,
			}))
			if result.Item != nil || result.Move.StatusChanged {
				t.Fatalf("a remote re-order must not touch an item: %+v", result.Move)
			}
			if len(result.Writes) != 1 || result.Writes[0].VaultID != "demo-team" {
				t.Fatalf("writes = %+v", result.Writes)
			}
			if got := columnByID(t, result.Board, "todo").Cards[0].Ref; got != "WEB/WEB-US-0031" {
				t.Fatalf("todo starts with %q", got)
			}
		})
	})
}

func TestTeamSummaryDescribesTheSnapshots(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		pinned(w, snapshotClock)
		summary := decode[teamSummary](t, wsCall(t, w, "team.get", nil))

		byKey := map[string]teamProjectSummary{}
		for _, p := range summary.Projects {
			byKey[string(p.Key)] = p
		}
		web := byKey["WEB"]
		if web.Cloned {
			t.Fatal("the fixture never clones WEB")
		}
		if !web.Snapshot.Present || web.Snapshot.Items != 4 || web.Snapshot.Stale {
			t.Fatalf("snapshot = %+v", web.Snapshot)
		}
		if web.Snapshot.Freshness != core.FreshnessFresh {
			t.Errorf("freshness = %q", web.Snapshot.Freshness)
		}
		if web.BrowseURL != "https://gitlab.com/example/website" {
			t.Errorf("browse url = %q", web.BrowseURL)
		}
		demo := byKey["DEMO"]
		if !demo.Cloned || demo.Snapshot.Present {
			t.Errorf("the cloned project has no snapshot in the fixture: %+v", demo.Snapshot)
		}
	})
}

func TestResolveRefFallsBackToTheSnapshot(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		pinned(w, snapshotClock)
		res := decode[refResolution](t, wsCall(t, w, "ref.resolve", map[string]any{"ref": "WEB/WEB-T-0007"}))

		if res.Cloned || !res.Declared || res.Found != nil {
			t.Fatalf("resolution = %+v", res)
		}
		if res.Snapshot == nil || res.Snapshot.Title != "Compress the hero video" {
			t.Fatalf("snapshot = %+v", res.Snapshot)
		}
		if res.SnapshotInfo == nil || !res.SnapshotInfo.Present {
			t.Fatalf("info = %+v", res.SnapshotInfo)
		}
		if !strings.Contains(res.URL, "/-/blob/main/documentation/") {
			t.Errorf("url = %q", res.URL)
		}
		if !strings.Contains(res.Reason, "read-only summary") {
			t.Errorf("reason = %q", res.Reason)
		}

		t.Run("an item the snapshot does not carry says so", func(t *testing.T) {
			missing := decode[refResolution](t, wsCall(t, w, "ref.resolve", map[string]any{"ref": "WEB/WEB-T-0099"}))
			if missing.Snapshot != nil {
				t.Fatalf("snapshot = %+v", missing.Snapshot)
			}
			if !strings.Contains(missing.Reason, "not in its index snapshot") {
				t.Errorf("reason = %q", missing.Reason)
			}
		})
	})
}

func TestRefreshSnapshots(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		pinned(w, snapshotClock)

		t.Run("a dry run writes nothing", func(t *testing.T) {
			result := decode[SnapshotRefreshResult](t, wsCall(t, w, "snapshot.refresh",
				map[string]any{"dryRun": true, "generatedBy": "jose"}))
			if len(result.Writes) != 0 {
				t.Fatalf("writes = %+v", result.Writes)
			}
			row := rowOf(t, result, "DEMO")
			if row.Status != SnapshotWritten || row.Snapshot == nil {
				t.Fatalf("row = %+v", row)
			}
			if row.Snapshot.Project.Repo != "https://github.com/example/demo-shop.git" {
				t.Errorf("the snapshot must carry the team.yaml declaration: %+v", row.Snapshot.Project)
			}
		})

		t.Run("the first run writes the snapshot of the cloned project only", func(t *testing.T) {
			result := decode[SnapshotRefreshResult](t, wsCall(t, w, "snapshot.refresh",
				map[string]any{"generatedBy": "jose"}))
			demo := rowOf(t, result, "DEMO")
			if demo.Status != SnapshotWritten || demo.Path != ".pmngr/index/DEMO.json" {
				t.Fatalf("row = %+v", demo)
			}
			if !demo.Info.Present || demo.Info.GeneratedBy != "jose" {
				t.Fatalf("info = %+v", demo.Info)
			}
			web := rowOf(t, result, "WEB")
			if web.Status != SnapshotSkipped || !strings.Contains(web.Reason, "clone it") {
				t.Fatalf("row = %+v", web)
			}
			if len(result.Writes) != 1 || result.Writes[0].VaultID != "demo-team" {
				t.Fatalf("writes = %+v", result.Writes)
			}
		})

		t.Run("a second run changes nothing and writes nothing", func(t *testing.T) {
			result := decode[SnapshotRefreshResult](t, wsCall(t, w, "snapshot.refresh",
				map[string]any{"generatedBy": "someone-else"}))
			if row := rowOf(t, result, "DEMO"); row.Status != SnapshotUnchanged {
				t.Fatalf("row = %+v", row)
			}
			if len(result.Writes) != 0 {
				t.Fatalf("a stable snapshot must not churn the history: %+v", result.Writes)
			}
		})

		t.Run("the board of the refreshed project is still live, not snapshot-backed", func(t *testing.T) {
			view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "delivery"}))
			card, ok := cardByRef(view, "DEMO/DEMO-US-0001")
			if !ok || card.Remote || card.Source != core.CardSourceLive {
				t.Fatalf("card = %+v (ok=%v)", card, ok)
			}
		})

		t.Run("the listing reports every declared project", func(t *testing.T) {
			result := decode[SnapshotRefreshResult](t, wsCall(t, w, "snapshot.list", nil))
			if len(result.Snapshots) != 2 {
				t.Fatalf("snapshots = %+v", result.Snapshots)
			}
			if !rowOf(t, result, "WEB").Info.Present {
				t.Error("the committed WEB snapshot must be listed")
			}
		})
	})
}

// rowOf returns the result row of a project key.
func rowOf(t *testing.T, result SnapshotRefreshResult, key string) SnapshotResult {
	t.Helper()
	for _, row := range result.Snapshots {
		if row.Project == key {
			return row
		}
	}
	t.Fatalf("no row for %s in %+v", key, result.Snapshots)
	return SnapshotResult{}
}

// cardByRef returns a rendered card by reference, from any column.
func cardByRef(view core.BoardView, ref string) (core.BoardCard, bool) {
	for _, c := range view.Columns {
		for _, card := range c.Cards {
			if card.Ref == ref {
				return card, true
			}
		}
	}
	for _, card := range view.Unmapped {
		if card.Ref == ref {
			return card, true
		}
	}
	return core.BoardCard{}, false
}
