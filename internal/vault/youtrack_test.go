package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// The import half of GIT-US-0047, over a fake YouTrack. The point of these
// cases is idempotence: the same issue imported twice must produce one item and
// one copy of its thread, whatever else changes around it.

// fakeYouTrack answers the import from a fixed set of issues, so the whole
// resolution runs with no network and no credentials.
type fakeYouTrack struct {
	issues      map[string]youtrack.Issue
	articles    map[string]youtrack.Article
	comments    map[string][]youtrack.Comment
	attachments map[string][]youtrack.Attachment
	search      []youtrack.Issue
	queries     []string
	reads       map[string]int
	// articleReads counts the article requests, which is what proves a status
	// call without the remote flag stays offline.
	articleReads int
}

func (f *fakeYouTrack) Article(_ context.Context, id string) (youtrack.Article, error) {
	f.articleReads++
	article, ok := f.articles[id]
	if !ok {
		return youtrack.Article{}, fmt.Errorf("no article %s", id)
	}
	return article, nil
}

func (f *fakeYouTrack) Issue(_ context.Context, id string) (youtrack.Issue, error) {
	if f.reads == nil {
		f.reads = map[string]int{}
	}
	f.reads[id]++
	issue, ok := f.issues[id]
	if !ok {
		return youtrack.Issue{}, fmt.Errorf("no issue %s", id)
	}
	return issue, nil
}

func (f *fakeYouTrack) IssueLinks(_ context.Context, id string) ([]youtrack.IssueLink, error) {
	return f.issues[id].Links, nil
}

func (f *fakeYouTrack) SearchAllIssues(
	_ context.Context, query string, _ youtrack.Page,
) ([]youtrack.Issue, error) {
	f.queries = append(f.queries, query)
	return f.search, nil
}

func (f *fakeYouTrack) AllComments(_ context.Context, id string) ([]youtrack.Comment, error) {
	return f.comments[id], nil
}

func (f *fakeYouTrack) Attachments(_ context.Context, id string) ([]youtrack.Attachment, error) {
	return f.attachments[id], nil
}

// ytField builds one enum-shaped custom field, which is how YouTrack sends a
// type, a state or a priority.
func ytField(name, value string) youtrack.CustomField {
	return youtrack.CustomField{
		Name:  name,
		Value: youtrack.CustomFieldValue{Raw: []byte(`{"name":` + quoteJSON(value) + `}`)},
	}
}

func quoteJSON(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

// ytSubtasks builds the outward half of the Subtask link: the children of an
// issue.
func ytSubtasks(children ...string) youtrack.IssueLink {
	link := youtrack.IssueLink{
		Direction: youtrack.DirectionOutward,
		LinkType:  youtrack.LinkType{Name: mapping.SubtaskLinkType, Aggregation: true},
	}
	for _, child := range children {
		link.Issues = append(link.Issues, youtrack.IssueRef{IDReadable: child})
	}
	return link
}

// ytParent builds the inward half of the Subtask link: the parent of an issue.
func ytParent(parent string) youtrack.IssueLink {
	return youtrack.IssueLink{
		Direction: youtrack.DirectionInward,
		LinkType:  youtrack.LinkType{Name: mapping.SubtaskLinkType, Aggregation: true},
		Issues:    []youtrack.IssueRef{{IDReadable: parent}},
	}
}

// ytIssue is the shorthand the fixtures below are written in.
func ytIssue(id, summary, kind string, links ...youtrack.IssueLink) youtrack.Issue {
	return youtrack.Issue{
		ID:           "2-" + id,
		IDReadable:   id,
		Summary:      summary,
		Description:  "Imported from YouTrack.",
		Reporter:     youtrack.User{Login: "alice", FullName: "Alice", Email: "alice@example.com"},
		CustomFields: []youtrack.CustomField{ytField("Type", kind)},
		Links:        links,
	}
}

// importFixture attaches the fake to the DEMO repository of a writable
// workspace and returns both.
func importFixture(t *testing.T, w *Workspace, fake *fakeYouTrack) {
	t.Helper()
	mount, ok := w.MountForProject("DEMO")
	if !ok {
		t.Fatal("the fixture workspace serves no DEMO project")
	}
	mount.Vault.SetYouTrackProvider(
		func(context.Context, string) (YouTrackSource, YouTrackLink, error) {
			return fake, YouTrackLink{BaseURL: "https://yt.example.com", Project: "ACME"}, nil
		})
}

// runImport runs one import and decodes the result.
func runImport(t *testing.T, w *Workspace, params map[string]any) YouTrackImportResult {
	t.Helper()
	return decode[YouTrackImportResult](t, wsCall(t, w, "youtrack.import.run", params))
}

// previewImport runs one preview and decodes the plan.
func previewImport(t *testing.T, w *Workspace, params map[string]any) YouTrackImportPreview {
	t.Helper()
	return decode[YouTrackImportPreview](t, wsCall(t, w, "youtrack.import.preview", params))
}

func TestYouTrackImportValidation(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		importFixture(t, w, &fakeYouTrack{issues: map[string]youtrack.Issue{}})

		cases := []struct {
			name   string
			params map[string]any
			want   string
		}{
			{"neither a query nor ids", map[string]any{"project": "DEMO"}, `"query" or "ids"`},
			{"both a query and ids", map[string]any{
				"project": "DEMO", "query": "State: Open", "ids": []string{"ACME-1"},
			}, `"query" and "ids"`},
			{"a depth beyond the bound", map[string]any{
				"project": "DEMO", "ids": []string{"ACME-1"}, "depth": YouTrackMaxDepth + 1,
			}, `"depth"`},
			{"a negative depth", map[string]any{
				"project": "DEMO", "ids": []string{"ACME-1"}, "depth": -1,
			}, `"depth"`},
		}
		for _, method := range []string{"youtrack.import.preview", "youtrack.import.run"} {
			for _, tc := range cases {
				t.Run(method+": "+tc.name, func(t *testing.T) {
					code, message := wsFail(t, w, method, tc.params)
					if code != "invalid_request" {
						t.Fatalf("code = %q (%s)", code, message)
					}
					if !strings.Contains(message, tc.want) {
						t.Fatalf("message = %q, want it to name %s", message, tc.want)
					}
				})
			}
		}

		t.Run("a host with no client says so", func(t *testing.T) {
			mount, _ := w.MountForProject("DEMO")
			mount.Vault.SetYouTrackProvider(nil)
			code, _ := wsFail(t, w, "youtrack.import.run",
				map[string]any{"project": "DEMO", "ids": []string{"ACME-1"}})
			if code != "unavailable" {
				t.Fatalf("code = %q", code)
			}
		})
	})
}

func TestYouTrackImportIsIdempotent(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		fake := &fakeYouTrack{
			issues: map[string]youtrack.Issue{
				"ACME-1": ytIssue("ACME-1", "Guest checkout", "User Story"),
			},
			comments: map[string][]youtrack.Comment{"ACME-1": {
				{ID: "4-1", Text: "First thought.", Created: 1756000000000,
					Author: youtrack.User{Login: "bob", FullName: "Bob"}},
				{ID: "4-2", Text: "Second thought.", Created: 1756000100000,
					Author: youtrack.User{Login: "bob", FullName: "Bob"}},
			}},
		}
		importFixture(t, w, fake)
		params := map[string]any{
			"project": "DEMO", "ids": []string{"ACME-1"}, "includeComments": true,
		}

		var itemID string
		t.Run("an unknown issue creates an item", func(t *testing.T) {
			result := runImport(t, w, params)
			if result.Created != 1 || result.Updated != 0 || result.Failed != 0 {
				t.Fatalf("result = %+v", result)
			}
			issue := result.Issues[0]
			if issue.Action != YouTrackImportCreate || issue.ItemID == "" || issue.Error != "" {
				t.Fatalf("issue = %+v", issue)
			}
			if issue.Comments != 2 {
				t.Fatalf("comments = %d, want 2", issue.Comments)
			}
			itemID = issue.ItemID

			item := decode[core.Item](t, wsCall(t, w, "item.get", map[string]any{"id": itemID}))
			if item.Type != core.TypeStory || item.Title != "Guest checkout" {
				t.Fatalf("item = %+v", item)
			}
			if len(item.External) != 1 || item.External[0].System != mapping.System ||
				item.External[0].ID != "ACME-1" {
				t.Fatalf("external = %+v", item.External)
			}
			// The item file and the two comment files are one write set.
			if len(result.Writes.Written) < 3 {
				t.Fatalf("writes = %+v", result.Writes.Written)
			}
		})

		t.Run("a re-import updates in place and duplicates nothing", func(t *testing.T) {
			fake.issues["ACME-1"] = ytIssue("ACME-1", "Guest checkout, end to end", "User Story")
			result := runImport(t, w, params)
			if result.Created != 0 || result.Updated != 1 || result.Failed != 0 {
				t.Fatalf("result = %+v", result)
			}
			if result.Issues[0].ItemID != itemID {
				t.Fatalf("the item id moved: %q, want %q", result.Issues[0].ItemID, itemID)
			}
			if result.Issues[0].Comments != 0 {
				t.Fatalf("a re-import wrote %d comments again", result.Issues[0].Comments)
			}
			item := decode[core.Item](t, wsCall(t, w, "item.get", map[string]any{"id": itemID}))
			if item.Title != "Guest checkout, end to end" {
				t.Fatalf("title = %q", item.Title)
			}
			comments := decode[[]core.Comment](t, wsCall(t, w, "comment.list",
				map[string]any{"id": itemID}))
			if len(comments) != 2 {
				t.Fatalf("comments = %d, want the same two", len(comments))
			}
			if comments[0].Author != "bob" || len(comments[0].External) != 1 {
				t.Fatalf("comment = %+v", comments[0])
			}
		})

		t.Run("preview writes nothing and decides the same actions", func(t *testing.T) {
			plan := previewImport(t, w, params)
			if len(plan.Issues) != 1 {
				t.Fatalf("plan = %+v", plan.Issues)
			}
			entry := plan.Issues[0]
			if entry.Action != YouTrackImportUpdate || entry.TargetID != itemID {
				t.Fatalf("plan entry = %+v", entry)
			}
			if entry.MappedType != core.TypeStory || entry.Comments != 0 {
				t.Fatalf("plan entry = %+v", entry)
			}
			// Nothing was written: the item is still the one the run left.
			item := decode[core.Item](t, wsCall(t, w, "item.get", map[string]any{"id": itemID}))
			if item.Title != "Guest checkout, end to end" {
				t.Fatalf("the preview changed the item: %+v", item)
			}
		})
	})
}

func TestYouTrackImportRecursion(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		fake := &fakeYouTrack{issues: map[string]youtrack.Issue{
			"ACME-1": ytIssue("ACME-1", "Checkout epic", "Epic", ytSubtasks("ACME-2")),
			"ACME-2": ytIssue("ACME-2", "Guest checkout", "User Story",
				ytParent("ACME-1"), ytSubtasks("ACME-3")),
			"ACME-3": ytIssue("ACME-3", "Session handling", "Task", ytParent("ACME-2")),
		}}
		importFixture(t, w, fake)

		t.Run("depth 0 imports the selected issue only", func(t *testing.T) {
			plan := previewImport(t, w, map[string]any{
				"project": "DEMO", "ids": []string{"ACME-1"}, "depth": 0,
			})
			if len(plan.Issues) != 1 || plan.Issues[0].YouTrackID != "ACME-1" {
				t.Fatalf("plan = %+v", plan.Issues)
			}
		})

		t.Run("depth 1 adds the children", func(t *testing.T) {
			plan := previewImport(t, w, map[string]any{
				"project": "DEMO", "ids": []string{"ACME-1"}, "depth": 1,
			})
			if len(plan.Issues) != 2 {
				t.Fatalf("plan = %+v", plan.Issues)
			}
			if plan.Issues[1].Depth != 1 {
				t.Fatalf("depth = %d", plan.Issues[1].Depth)
			}
		})

		t.Run("depth 2 adds the grandchildren and resolves the parents", func(t *testing.T) {
			result := runImport(t, w, map[string]any{
				"project": "DEMO", "ids": []string{"ACME-1"}, "depth": 2,
			})
			if result.Created != 3 || result.Failed != 0 {
				t.Fatalf("result = %+v", result)
			}
			byIssue := map[string]string{}
			for _, issue := range result.Issues {
				byIssue[issue.YouTrackID] = issue.ItemID
			}
			story := decode[core.Item](t, wsCall(t, w, "item.get",
				map[string]any{"id": byIssue["ACME-2"]}))
			if string(story.Parent) != byIssue["ACME-1"] {
				t.Fatalf("parent = %q, want %q", story.Parent, byIssue["ACME-1"])
			}
			task := decode[core.Item](t, wsCall(t, w, "item.get",
				map[string]any{"id": byIssue["ACME-3"]}))
			if string(task.Parent) != byIssue["ACME-2"] {
				t.Fatalf("parent = %q, want %q", task.Parent, byIssue["ACME-2"])
			}
		})

		t.Run("an issue the tracker does not have is reported, not fatal", func(t *testing.T) {
			result := runImport(t, w, map[string]any{
				"project": "DEMO", "ids": []string{"ACME-1", "ACME-404"}, "depth": 0,
			})
			if result.Failed != 1 {
				t.Fatalf("result = %+v", result)
			}
			found := false
			for _, issue := range result.Issues {
				if issue.YouTrackID == "ACME-404" && issue.Error != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("issues = %+v", result.Issues)
			}
		})
	})
}

func TestYouTrackImportCycleTerminates(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		// ACME-1 is the parent of ACME-2, which YouTrack also reports as the
		// parent of ACME-1: a graph no walk may follow twice.
		fake := &fakeYouTrack{issues: map[string]youtrack.Issue{
			"ACME-1": ytIssue("ACME-1", "One", "User Story", ytSubtasks("ACME-2"), ytParent("ACME-2")),
			"ACME-2": ytIssue("ACME-2", "Two", "Task", ytSubtasks("ACME-1"), ytParent("ACME-1")),
		}}
		importFixture(t, w, fake)

		plan := previewImport(t, w, map[string]any{
			"project": "DEMO", "ids": []string{"ACME-1"}, "depth": YouTrackMaxDepth,
		})
		if len(plan.Issues) != 2 {
			t.Fatalf("a cycle must be visited once per issue: %+v", plan.Issues)
		}
	})
}

func TestYouTrackImportOutOfSetReferences(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		fake := &fakeYouTrack{issues: map[string]youtrack.Issue{
			"ACME-7": ytIssue("ACME-7", "Orphan", "Task", ytParent("ACME-1")),
		}}
		importFixture(t, w, fake)

		result := runImport(t, w, map[string]any{
			"project": "DEMO", "ids": []string{"ACME-7"}, "depth": 0,
		})
		if result.Created != 1 || result.Failed != 0 {
			t.Fatalf("result = %+v", result)
		}
		warned := false
		for _, warning := range result.Issues[0].Warnings {
			if warning.Field == "parent" && warning.Value == "ACME-1" {
				warned = true
			}
		}
		if !warned {
			t.Fatalf("warnings = %+v", result.Issues[0].Warnings)
		}
		item := decode[core.Item](t, wsCall(t, w, "item.get",
			map[string]any{"id": result.Issues[0].ItemID}))
		if item.Parent != "" {
			t.Fatalf("parent = %q, want none", item.Parent)
		}
	})
}

func TestYouTrackImportReportsOneFailureAndContinues(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		// An issue with no summary has no title, and an item without a title
		// fails validation: the rest of the batch must still land.
		broken := ytIssue("ACME-5", "", "Task")
		fake := &fakeYouTrack{issues: map[string]youtrack.Issue{
			"ACME-4": ytIssue("ACME-4", "Sound", "Task"),
			"ACME-5": broken,
			"ACME-6": ytIssue("ACME-6", "Also sound", "Task"),
		}}
		importFixture(t, w, fake)

		result := runImport(t, w, map[string]any{
			"project": "DEMO", "ids": []string{"ACME-4", "ACME-5", "ACME-6"},
		})
		if result.Created != 2 || result.Failed != 1 {
			t.Fatalf("result = %+v", result)
		}
		for _, issue := range result.Issues {
			switch issue.YouTrackID {
			case "ACME-5":
				if issue.Error == "" {
					t.Fatal("the broken issue must report its failure")
				}
			default:
				if issue.Error != "" || issue.ItemID == "" {
					t.Fatalf("issue = %+v", issue)
				}
			}
		}
	})
}

func TestYouTrackImportByQuery(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		fake := &fakeYouTrack{
			issues: map[string]youtrack.Issue{
				"ACME-8": ytIssue("ACME-8", "From a query", "Task"),
			},
			search: []youtrack.Issue{{IDReadable: "ACME-8"}},
		}
		importFixture(t, w, fake)

		result := runImport(t, w, map[string]any{"project": "DEMO", "query": "State: Open"})
		if result.Created != 1 {
			t.Fatalf("result = %+v", result)
		}
		if len(fake.queries) != 1 || !strings.Contains(fake.queries[0], "project: ACME") {
			t.Fatalf("queries = %v: a bare query is scoped to the linked project", fake.queries)
		}
	})
}

func TestYouTrackImportLinksAndAttachments(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		relates := func(target string) youtrack.IssueLink {
			return youtrack.IssueLink{
				Direction: youtrack.DirectionBoth,
				LinkType:  youtrack.LinkType{Name: "Relates"},
				Issues:    []youtrack.IssueRef{{IDReadable: target}},
			}
		}
		fake := &fakeYouTrack{
			issues: map[string]youtrack.Issue{
				"ACME-10": ytIssue("ACME-10", "One side", "Task", relates("ACME-11"), relates("ACME-99")),
				"ACME-11": ytIssue("ACME-11", "Other side", "Task", relates("ACME-10")),
			},
			attachments: map[string][]youtrack.Attachment{
				"ACME-10": {{ID: "8-1", Name: "trace.log"}},
			},
		}
		importFixture(t, w, fake)

		result := runImport(t, w, map[string]any{
			"project": "DEMO", "ids": []string{"ACME-10", "ACME-11"},
			"includeLinks": true, "includeAttachments": true,
		})
		if result.Created != 2 || result.Failed != 0 {
			t.Fatalf("result = %+v", result)
		}
		byIssue := map[string]string{}
		for _, issue := range result.Issues {
			byIssue[issue.YouTrackID] = issue.ItemID
		}
		item := decode[core.Item](t, wsCall(t, w, "item.get",
			map[string]any{"id": byIssue["ACME-10"]}))
		if len(item.Links) != 1 || item.Links[0].Target != byIssue["ACME-11"] {
			t.Fatalf("links = %+v: only the resolved half is written", item.Links)
		}
		if item.Links[0].Kind != core.LinkRelatesTo {
			t.Fatalf("kind = %q", item.Links[0].Kind)
		}
		// A bare file name, as docs/03 §13.4 specifies for every writer: the
		// folder is `.pmngr/attachments/<ITEM-ID>/` by convention and is never
		// repeated inside the entry (R-ATT-4, R-YT-7).
		if len(item.Attachments) != 1 || item.Attachments[0] != "trace.log" {
			t.Fatalf("attachments = %v, want the bare file name", item.Attachments)
		}
		// The unresolvable half is a warning, never a dangling link.
		warned := false
		for _, issue := range result.Issues {
			for _, warning := range issue.Warnings {
				if warning.Field == "links" && warning.Value == "ACME-99" {
					warned = true
				}
			}
		}
		if !warned {
			t.Fatalf("issues = %+v", result.Issues)
		}
	})
}

// ------------------------------------------------- the configured values ---

// importFixtureLinked is importFixture with a link the caller wrote, which is
// how a project's configured `field_map` is put in front of the importer.
func importFixtureLinked(t *testing.T, w *Workspace, fake *fakeYouTrack, link YouTrackLink) {
	t.Helper()
	mount, ok := w.MountForProject("DEMO")
	if !ok {
		t.Fatal("the fixture workspace serves no DEMO project")
	}
	mount.Vault.SetYouTrackProvider(
		func(context.Context, string) (YouTrackSource, YouTrackLink, error) {
			return fake, link, nil
		})
}

// TestYouTrackImportAppliesConfiguredValueMaps covers GIT-T-0131: a project
// that configured what its YouTrack states mean must have an actual import
// honor it, not only the preview.
//
// The link below renames the state field *and* maps three of its values, so one
// test fails on either half going missing: before the field map crossed the
// seam whole, only the names arrived and every value fell back to the default.
func TestYouTrackImportAppliesConfiguredValueMaps(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		state := func(id, summary, value string) youtrack.Issue {
			issue := ytIssue(id, summary, "Task")
			issue.CustomFields = append(issue.CustomFields, ytField("Estado", value))
			return issue
		}
		fake := &fakeYouTrack{issues: map[string]youtrack.Issue{
			// A value the shipped defaults do not know at all.
			"ACME-21": state("ACME-21", "Parked work", "Parked"),
			// A value the defaults do know, mapped somewhere else: the
			// project's answer must win over the shipped one.
			"ACME-22": state("ACME-22", "Remapped work", "In Progress"),
			// A value the project said nothing about: the defaults must
			// survive, because a configured map is overlaid and not a
			// replacement.
			"ACME-23": state("ACME-23", "Finished work", "Fixed"),
		}}
		importFixtureLinked(t, w, fake, YouTrackLink{
			BaseURL: "https://yt.example.com",
			Project: "ACME",
			FieldMap: map[string]mapping.FieldSpec{
				"status": {Field: "Estado", Values: map[string]string{
					"Parked":      "backlog",
					"In Progress": "in_review",
				}},
			},
		})

		result := runImport(t, w, map[string]any{
			"project": "DEMO",
			"ids":     []string{"ACME-21", "ACME-22", "ACME-23"},
			"depth":   0,
		})
		if result.Created != 3 || result.Failed != 0 {
			t.Fatalf("result = %+v", result)
		}

		want := map[string]core.Status{
			"ACME-21": core.Status("backlog"),
			"ACME-22": core.Status("in_review"),
			"ACME-23": core.Status("done"),
		}
		for _, imported := range result.Issues {
			item := decode[core.Item](t, wsCall(t, w, "item.get",
				map[string]any{"id": imported.ItemID}))
			if got := item.Status; got != want[imported.YouTrackID] {
				t.Errorf("%s landed on status %q, want %q",
					imported.YouTrackID, got, want[imported.YouTrackID])
			}
		}
	})
}

// TestYouTrackAttachmentNameIsABareFileName covers the reduction that makes a
// bare `attachments[]` entry safe: whatever a tracker calls a file, the entry
// is a plain name that resolves inside the item's own attachment folder
// (R-ATT-4, R-YT-7).
func TestYouTrackAttachmentNameIsABareFileName(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"a plain name is kept", "trace.log", "trace.log"},
		{"surrounding space is trimmed", "  trace.log  ", "trace.log"},
		{"a path is reduced to its base", "reports/2026/trace.log", "trace.log"},
		{"a traversal is reduced too", "../../etc/passwd", "passwd"},
		{"a Windows path is reduced", `C:\temp\trace.log`, "trace.log"},
		{"a directory name maps to nothing", "..", ""},
		{"an empty name maps to nothing", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := youtrackAttachmentName(tc.raw); got != tc.want {
				t.Errorf("youtrackAttachmentName(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
