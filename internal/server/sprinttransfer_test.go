package server

import (
	"net/http"
	"testing"
)

// The HTTP half of the sprint transfer, task GIT-T-0146 of story GIT-US-0085.

// sprintTransferBody is enough of a vault.SprintResult for these assertions.
type sprintTransferBody struct {
	Sprint struct {
		Sprint struct {
			ID    string   `json:"id"`
			State string   `json:"state"`
			Items []string `json:"items"`
		} `json:"sprint"`
	} `json:"sprint"`
	Report *struct {
		Incomplete []struct{ Ref string } `json:"incomplete"`
		Carried    []struct {
			Ref    string `json:"ref"`
			Action string `json:"action"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"carried"`
	} `json:"report"`
	Writes []struct {
		VaultID string `json:"vaultId"`
	} `json:"writes"`
	DryRun bool `json:"dryRun"`
}

func TestSprintTransferRequiresThePrecondition(t *testing.T) {
	t.Parallel()

	s := newTeamServer(t)
	var doc problemBody
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/sprints/DEMO-TEAM-S-0001/transfer",
		body: map[string]any{"mode": "backlog"},
	}), http.StatusPreconditionRequired, &doc)
	if doc.Code != codePreconditionRequired {
		t.Fatalf("code = %q", doc.Code)
	}

	t.Run("a stale revision is refused with the current one", func(t *testing.T) {
		var stale problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints/DEMO-TEAM-S-0001/transfer",
			body:   map[string]any{"mode": "backlog"},
			header: map[string]string{"If-Match": "sha256:0000000000000000"},
		}), http.StatusPreconditionFailed, &stale)
		if stale.Code != "stale_revision" || stale.CurrentRev == "" {
			t.Fatalf("problem = %+v", stale)
		}
	})
}

func TestSprintTransferDryRunWritesAndPublishesNothing(t *testing.T) {
	t.Parallel()

	s := newTeamServer(t)
	_, rev := readSprint(t, s, "DEMO-TEAM-S-0001")
	client := newHubClient()
	s.hub.register(client)

	var out sprintTransferBody
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/sprints/DEMO-TEAM-S-0001/transfer",
		body:   map[string]any{"mode": "backlog", "dryRun": true},
		header: map[string]string{"If-Match": rev},
	}), http.StatusOK, &out)

	if !out.DryRun {
		t.Fatal("a dry run must say so, so that no caller mistakes a preview for a commitment")
	}
	if len(out.Writes) != 0 {
		t.Fatalf("a dry run wrote %d repositories", len(out.Writes))
	}
	if out.Report == nil {
		t.Fatal("a dry run still computes the report; that is what it is for")
	}
	select {
	case ev := <-client.events:
		t.Fatalf("a dry run published %q", ev.Type)
	default:
	}
	// The source sprint is untouched: a transfer never closes anything, and a
	// dry run never writes anything.
	after, afterRev := readSprint(t, s, "DEMO-TEAM-S-0001")
	if afterRev != rev {
		t.Fatalf("the sprint file moved: %s -> %s", rev, afterRev)
	}
	if after.Sprint.State != "active" {
		t.Fatalf("state = %q, want the sprint left running", after.Sprint.State)
	}
}

func TestSprintTransferMovesTheScopeAndPublishes(t *testing.T) {
	t.Parallel()

	s := newTeamServer(t)
	before, rev := readSprint(t, s, "DEMO-TEAM-S-0001")
	client := newHubClient()
	client.subscribe([]string{eventSprintChanged, eventItemChanged})
	s.hub.register(client)

	var out sprintTransferBody
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/sprints/DEMO-TEAM-S-0001/transfer",
		body:   map[string]any{"mode": "backlog"},
		header: map[string]string{"If-Match": rev},
	}), http.StatusOK, &out)

	if out.DryRun {
		t.Fatal("a real transfer is not a dry run")
	}
	if out.Report == nil || len(out.Report.Carried) == 0 {
		t.Fatalf("the report carried nothing: %+v", out.Report)
	}
	// A transfer moves the work, not the sprint: the source sprint's own scope
	// comes back untouched, which is what distinguishes it from a close.
	if len(out.Sprint.Sprint.Items) != len(before.Sprint.Items) {
		t.Fatalf("the source sprint's scope changed: %d -> %d",
			len(before.Sprint.Items), len(out.Sprint.Sprint.Items))
	}
	if out.Sprint.Sprint.State != "active" {
		t.Fatalf("state = %q: a transfer closes nothing", out.Sprint.Sprint.State)
	}

	seen := map[string]bool{}
	for {
		select {
		case ev := <-client.events:
			seen[ev.Type] = true
			continue
		default:
		}
		break
	}
	if !seen[eventSprintChanged] {
		t.Fatalf("topics = %v, want sprint.changed", seen)
	}

	t.Run("a per-item failure is reported, not an error", func(t *testing.T) {
		// A reference whose project this machine has not cloned cannot be
		// written, and comes back on its own carried line with a 200.
		for _, carried := range out.Report.Carried {
			if carried.Error != "" && carried.Ref == "" {
				t.Fatalf("an error line names no reference: %+v", carried)
			}
		}
	})
}

func TestSprintCloseAcceptsABulkTransfer(t *testing.T) {
	t.Parallel()

	s := newTeamServer(t)
	_, rev := readSprint(t, s, "DEMO-TEAM-S-0001")

	var out sprintTransferBody
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/sprints/DEMO-TEAM-S-0001/close",
		body: map[string]any{
			"transfer": map[string]any{"mode": "backlog"},
			"dryRun":   true,
		},
		header: map[string]string{"If-Match": rev},
	}), http.StatusOK, &out)

	if !out.DryRun || len(out.Writes) != 0 {
		t.Fatalf("a dry-run close wrote %d repositories, dryRun=%v", len(out.Writes), out.DryRun)
	}
	if out.Report == nil {
		t.Fatal("a close always reports what it graded")
	}
	if out.Sprint.Sprint.State == "completed" {
		t.Fatal("a dry run must not complete the sprint")
	}
}
