package vault

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// GIT-T-0226: `integrations.youtrack.land_in_inbox` decides where an imported
// issue arrives. These cases pin the four answers — created with the option on,
// created with it off, updated with it on, and a project that asks for the
// inbox without having one.

// ytLinkedVault installs a YouTrack provider on one vault, so a whole project
// configuration can be put in front of the importer without a workspace.
func ytLinkedVault(v *Vault, fake *fakeYouTrack, landInInbox bool) {
	v.SetYouTrackProvider(
		func(context.Context, string) (YouTrackSource, YouTrackLink, error) {
			return fake, YouTrackLink{
				BaseURL:     "https://yt.example.com",
				Project:     "ACME",
				LandInInbox: landInInbox,
			}, nil
		})
}

// ytStateIssue is one issue carrying a State, which is what makes the mapped
// status something other than the workflow's initial one.
func ytStateIssue(id, summary, state string) youtrack.Issue {
	issue := ytIssue(id, summary, "Task")
	issue.CustomFields = append(issue.CustomFields, ytField("State", state))
	return issue
}

// ytImportOne imports a single issue into a project and returns the item it
// became.
func ytImportOne(t *testing.T, v *Vault, project, issue string) core.Item {
	t.Helper()
	result := decode[YouTrackImportResult](t, call(t, v, "youtrack.import.run", map[string]any{
		"project": project, "ids": []string{issue}, "depth": 0,
	}))
	if result.Failed != 0 || len(result.Issues) != 1 {
		t.Fatalf("result = %+v", result)
	}
	return decode[core.Item](t, call(t, v, "item.get",
		map[string]any{"id": result.Issues[0].ItemID}))
}

func TestYouTrackImportLandsInTheInbox(t *testing.T) {
	t.Run("a created issue arrives in triage, stamped as a pending submission", func(t *testing.T) {
		v := inboxVault(t)
		ytLinkedVault(v, &fakeYouTrack{issues: map[string]youtrack.Issue{
			"ACME-31": ytStateIssue("ACME-31", "Imported for review", "In Progress"),
		}}, true)

		item := ytImportOne(t, v, "INBX", "ACME-31")
		if item.Status != "triage" {
			t.Errorf("status = %q, want triage", item.Status)
		}
		if item.Inbox == nil {
			t.Fatal("the item carries no inbox block")
		}
		if item.Inbox.Status != core.InboxPending {
			t.Errorf("inbox status = %q, want %s", item.Inbox.Status, core.InboxPending)
		}
		if item.Inbox.Source != mapping.System {
			t.Errorf("inbox source = %q, want %s", item.Inbox.Source, mapping.System)
		}
		if item.Inbox.Received.IsZero() {
			t.Error("the submission carries no received stamp")
		}
	})

	t.Run("the option off leaves arrival where it has always been", func(t *testing.T) {
		v := inboxVault(t)
		ytLinkedVault(v, &fakeYouTrack{issues: map[string]youtrack.Issue{
			// No State at all, so the item lands on the initial status.
			"ACME-32": ytIssue("ACME-32", "Imported as usual", "Task"),
		}}, false)

		item := ytImportOne(t, v, "INBX", "ACME-32")
		if item.Status != "backlog" {
			t.Errorf("status = %q, want the initial status backlog", item.Status)
		}
		if item.Inbox != nil {
			t.Errorf("inbox block = %+v, want none", item.Inbox)
		}
	})

	t.Run("an updated issue keeps the status it has", func(t *testing.T) {
		v := inboxVault(t)
		fake := &fakeYouTrack{issues: map[string]youtrack.Issue{
			"ACME-33": ytStateIssue("ACME-33", "Imported once", "In Progress"),
		}}
		ytLinkedVault(v, fake, false)
		created := ytImportOne(t, v, "INBX", "ACME-33")
		if created.Status != "in_progress" {
			t.Fatalf("status = %q, want in_progress", created.Status)
		}

		// The option is turned on between the two imports: landing is a
		// decision about arrival, so the second import must not move an item
		// that already exists into triage.
		ytLinkedVault(v, fake, true)
		updated := ytImportOne(t, v, "INBX", "ACME-33")
		if updated.ID != created.ID {
			t.Fatalf("the re-import created %s instead of updating %s", updated.ID, created.ID)
		}
		if updated.Status != "in_progress" {
			t.Errorf("status = %q, want in_progress kept", updated.Status)
		}
		if updated.Inbox != nil {
			t.Errorf("inbox block = %+v, want none", updated.Inbox)
		}
	})

	t.Run("a project with no triage status refuses the whole import", func(t *testing.T) {
		// The DEMO fixture declares no status in the triage category, so it has
		// no inbox: an import that asks for one is a configuration mistake, and
		// landing the batch in the backlog instead is the one outcome the
		// option exists to prevent.
		for _, method := range []string{"youtrack.import.run", "youtrack.import.preview"} {
			t.Run(method, func(t *testing.T) {
				v, _ := loadedVault(t)
				ytLinkedVault(v, &fakeYouTrack{issues: map[string]youtrack.Issue{
					"ACME-34": ytIssue("ACME-34", "Refused", "Task"),
				}}, true)

				env := rawCall(t, v, method, map[string]any{
					"project": "DEMO", "ids": []string{"ACME-34"}, "depth": 0,
				})
				if env.OK {
					t.Fatal("the DEMO fixture declares no triage status")
				}
				if env.Error.Code != NoTriageStatusCode {
					t.Errorf("code = %q, want %s", env.Error.Code, NoTriageStatusCode)
				}
				for _, want := range []string{"DEMO", "land_in_inbox", core.ProjectFileName} {
					if !strings.Contains(env.Error.Message, want) {
						t.Errorf("message %q does not name %q", env.Error.Message, want)
					}
				}
				// Nothing landed: the refusal is the whole import, not a
				// per-issue failure reported after a write.
				page := decode[itemPage](t, call(t, v, "item.list", map[string]any{"limit": 500}))
				for _, it := range page.Items {
					if it.Title == "Refused" {
						t.Fatalf("%s was written despite the refusal", it.ID)
					}
				}
			})
		}
	})
}
