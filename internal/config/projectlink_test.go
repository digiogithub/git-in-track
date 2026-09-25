package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// richProjectYAML is a project.yaml the Go structs of this repository do not
// fully model: it carries comments, an unknown top-level section and key orders
// no encoder would reproduce. Everything in it must survive a link write.
const richProjectYAML = `# The backlog of the ACME API.
schema: 1
key: ACME
name: ACME API

# Where the knowledge base lives.
docs:
  path: docs
  wikilinks: true

workflow:
  initial: backlog
  statuses:
    - id: backlog
      category: todo
    - id: done
      category: done
      terminal: true

# A section no Go struct in this repository models. It must survive.
house_rules:
  review: two eyes
  deploy:
    - staging
    - production
`

// writeProject writes a project.yaml into a temporary directory.
func writeProject(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "project.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // a tracked, shared file
		t.Fatalf("write project.yaml: %v", err)
	}
	return path
}

// readProject reads a project.yaml back.
func readProject(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a temporary file this test wrote
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	return string(data)
}

// TestSaveYouTrackLinkPreservesTheFile is the round trip the story demands: a
// realistic file with comments and unmodelled sections keeps every one of them.
func TestSaveYouTrackLinkPreservesTheFile(t *testing.T) {
	t.Parallel()

	path := writeProject(t, richProjectYAML)
	link := YouTrackLink{
		URL:     "https://yt.example.com/youtrack/",
		Project: "ACME",
		FieldMap: FieldMap{
			"status":   {Field: "State"},
			"priority": {Field: "Priority"},
		},
	}
	changed, err := SaveYouTrackLink(path, link)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !changed {
		t.Fatal("the first write reported no change")
	}

	got := readProject(t, path)
	for _, keep := range []string{
		"# The backlog of the ACME API.",
		"# Where the knowledge base lives.",
		"# A section no Go struct in this repository models. It must survive.",
		"house_rules:",
		"review: two eyes",
		"- production",
		"terminal: true",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("the write lost %q:\n%s", keep, got)
		}
	}

	reloaded, err := LoadYouTrackLink(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded == nil {
		t.Fatal("the block was not written")
	}
	if reloaded.URL != "https://yt.example.com/youtrack" {
		t.Errorf("url = %q, want the trailing slash trimmed", reloaded.URL)
	}
	if reloaded.Project != "ACME" {
		t.Errorf("project = %q", reloaded.Project)
	}
	if reloaded.PushComments != PushCommentsManual || reloaded.KBSync != KBSyncManual ||
		reloaded.KBSyncDirection != KBSyncPush {
		t.Errorf("defaults were not written: %+v", reloaded)
	}
	if reloaded.FieldMap["status"].Field != "State" || reloaded.FieldMap["priority"].Field != "Priority" {
		t.Errorf("field map = %v", reloaded.FieldMap)
	}

	// Writing the same link again must not touch the file: a settings save that
	// changes nothing should not dirty a tracked file.
	before := readProject(t, path)
	changed, err = SaveYouTrackLink(path, *reloaded)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if changed {
		t.Error("an identical write reported a change")
	}
	if after := readProject(t, path); after != before {
		t.Errorf("an identical write rewrote the file:\n%s", after)
	}
}

// TestSaveYouTrackLinkUpdatesInPlace covers the update path, including dropping
// a field map that became empty.
func TestSaveYouTrackLinkUpdatesInPlace(t *testing.T) {
	t.Parallel()

	path := writeProject(t, richProjectYAML+`
integrations:
  # How this backlog reaches the tracker.
  youtrack:
    url: https://old.example.com
    project: OLD
    field_map:
      status: Stage
`)
	updated := YouTrackLink{
		URL:          "https://yt.example.com/youtrack",
		Project:      "ACME",
		PushComments: PushCommentsAuto,
		KBSync:       KBSyncOnWrite,
	}
	if _, err := SaveYouTrackLink(path, updated); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := readProject(t, path)
	if !strings.Contains(got, "# How this backlog reaches the tracker.") {
		t.Errorf("the comment inside the block was lost:\n%s", got)
	}
	if strings.Contains(got, "old.example.com") || strings.Contains(got, "OLD") {
		t.Errorf("the previous values survived:\n%s", got)
	}
	if strings.Contains(got, "field_map") {
		t.Errorf("an emptied field map should be removed, not left behind:\n%s", got)
	}

	reloaded, err := LoadYouTrackLink(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.PushComments != PushCommentsAuto || reloaded.KBSync != KBSyncOnWrite {
		t.Errorf("modes were not written: %+v", reloaded)
	}
}

// TestLoadYouTrackLink covers the absent, valid and invalid cases the story
// calls out as load errors.
func TestLoadYouTrackLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string // a substring of the expected error, empty when it must load
		nil  bool   // whether the link is expected to be absent
	}{
		{name: "no block", body: richProjectYAML, nil: true},
		{
			name: "valid",
			body: "key: ACME\nintegrations:\n  youtrack:\n    url: https://yt.example.com\n    project: ACME\n",
		},
		{
			name: "relative url",
			body: "integrations:\n  youtrack:\n    url: yt.example.com\n    project: ACME\n",
			want: "not absolute",
		},
		{
			name: "ftp url",
			body: "integrations:\n  youtrack:\n    url: ftp://yt.example.com\n    project: ACME\n",
			want: "not http or https",
		},
		{
			name: "empty project",
			body: "integrations:\n  youtrack:\n    url: https://yt.example.com\n    project: \"\"\n",
			want: "integrations.youtrack.project",
		},
		{
			name: "unknown field map key",
			body: "integrations:\n  youtrack:\n    url: https://yt.example.com\n    project: ACME\n" +
				"    field_map:\n      stat: State\n",
			want: "is not a git-in-track field",
		},
		{
			name: "unknown push mode",
			body: "integrations:\n  youtrack:\n    url: https://yt.example.com\n    project: ACME\n" +
				"    push_comments: sometimes\n",
			want: "use manual or auto",
		},
		{
			name: "unknown kb sync direction",
			body: "integrations:\n  youtrack:\n    url: https://yt.example.com\n    project: ACME\n" +
				"    kb_sync_direction: sideways\n",
			want: "use push, pull or both",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			link, err := LoadYouTrackLink(writeProject(t, tc.body))
			switch {
			case tc.want != "":
				if err == nil {
					t.Fatalf("expected an error mentioning %q", tc.want)
				}
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("error does not unwrap to ErrInvalid: %v", err)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error %q does not mention %q", err, tc.want)
				}
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.nil && link != nil:
				t.Fatalf("expected no link, got %+v", link)
			case !tc.nil && link == nil:
				t.Fatal("expected a link")
			}
		})
	}
}

// TestLoadYouTrackLinkMissingFile proves a project with no project.yaml is not
// an error: it is simply not connected.
func TestLoadYouTrackLinkMissingFile(t *testing.T) {
	t.Parallel()

	link, err := LoadYouTrackLink(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil || link != nil {
		t.Fatalf("LoadYouTrackLink = %+v, %v; want nil, nil", link, err)
	}
}

// TestSaveYouTrackLinkRefusesAnInvalidLink proves nothing is written when the
// link would not load back.
func TestSaveYouTrackLinkRefusesAnInvalidLink(t *testing.T) {
	t.Parallel()

	path := writeProject(t, richProjectYAML)
	before := readProject(t, path)
	if _, err := SaveYouTrackLink(path, YouTrackLink{URL: "nope", Project: ""}); err == nil {
		t.Fatal("expected a refusal")
	}
	if after := readProject(t, path); after != before {
		t.Error("a refused write still touched the file")
	}
}

// TestSaveYouTrackLinkRefusesAConflictingKey proves the writer never overwrites
// a key of the user's that is not a mapping.
func TestSaveYouTrackLinkRefusesAConflictingKey(t *testing.T) {
	t.Parallel()

	path := writeProject(t, "key: ACME\nintegrations: none\n")
	_, err := SaveYouTrackLink(path, YouTrackLink{URL: "https://yt.example.com", Project: "ACME"})
	if err == nil || !strings.Contains(err.Error(), "not a mapping") {
		t.Fatalf("err = %v, want a refusal naming the conflicting key", err)
	}
}

// TestLoadYouTrackLinkLandInInbox proves the option decodes, defaults to false,
// and survives a connection save — SaveYouTrackLink writes only the keys the
// settings screen owns, so a key a team set by hand is not lost to a save that
// never knew about it.
func TestLoadYouTrackLinkLandInInbox(t *testing.T) {
	t.Parallel()

	base := "key: ACME\nintegrations:\n  youtrack:\n    url: https://yt.example.com\n    project: ACME\n"

	t.Run("absent defaults to false", func(t *testing.T) {
		t.Parallel()
		link, err := LoadYouTrackLink(writeProject(t, base))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if link == nil || link.LandInInbox {
			t.Fatalf("LandInInbox = %+v, want false", link)
		}
	})

	t.Run("true decodes", func(t *testing.T) {
		t.Parallel()
		link, err := LoadYouTrackLink(writeProject(t, base+"    land_in_inbox: true\n"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if link == nil || !link.LandInInbox {
			t.Fatalf("LandInInbox = %+v, want true", link)
		}
	})

	t.Run("a connection save leaves it alone", func(t *testing.T) {
		t.Parallel()
		path := writeProject(t, base+"    land_in_inbox: true\n")
		if _, err := SaveYouTrackLink(path, YouTrackLink{
			URL: "https://yt.example.com/youtrack", Project: "OTHER",
		}); err != nil {
			t.Fatalf("save: %v", err)
		}
		link, err := LoadYouTrackLink(path)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if link == nil || !link.LandInInbox || link.Project != "OTHER" {
			t.Fatalf("reloaded = %+v, want land_in_inbox kept and project OTHER", link)
		}
	})
}

// hostileProjectYAML carries everything a whole-file re-encode rewrites:
// aligned comments, blank lines, quoting, a folded scalar and a flow mapping
// whose unquoted commas yaml.v3 already split into extra keys (GIT-US-0154).
const hostileProjectYAML = `# The backlog of the ACME API.
schema: 1
key:  ACME                 # aligned by hand
name: "ACME API"

summary: >
  A folded description
  over two lines.

labels:
  - { name: core,     color: "#4f46e5", description: Shared Go core (model, parser, index) }
  - { name: newcomer, color: '#16a34a', description: "Small, well-scoped" }
`

// TestSaveYouTrackLinkLeavesTheRestOfTheFileByteForByte is the regression test
// of GIT-US-0154: the link write edits the integrations block and no other
// byte, where it used to re-encode the whole file through yaml.v3.
func TestSaveYouTrackLinkLeavesTheRestOfTheFileByteForByte(t *testing.T) {
	t.Parallel()

	link := YouTrackLink{
		URL:      "https://yt.example.com/youtrack",
		Project:  "ACME",
		FieldMap: FieldMap{"status": {Field: "State"}},
	}
	const after = "\n# Rules no Go struct models.\nhouse_rules:\n  review:   two eyes\n"
	cases := []struct {
		name         string
		prefix, tail string // bytes that must survive around the block
		block        string // the integrations text of the input
	}{
		{
			name:   "a new integrations section is appended",
			prefix: hostileProjectYAML,
		},
		{
			name:   "a new youtrack block joins an existing integrations section",
			prefix: hostileProjectYAML + "integrations:\n  other:   { enabled: true }   # keep\n",
			tail:   after,
		},
		{
			name:   "an existing youtrack block is replaced",
			prefix: hostileProjectYAML + "integrations:\n  # How this backlog reaches the tracker.\n",
			block:  "  youtrack:\n    url: https://old.example.com\n    project: OLD\n    field_map:\n      status: Stage\n",
			tail:   "  other:   { enabled: true }   # keep\n" + after,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeProject(t, tc.prefix+tc.block+tc.tail)
			if _, err := SaveYouTrackLink(path, link); err != nil {
				t.Fatalf("save: %v", err)
			}
			got := readProject(t, path)
			if !strings.HasPrefix(got, tc.prefix) || !strings.HasSuffix(got, tc.tail) {
				t.Fatalf("bytes outside the integrations block changed:\n%s", got)
			}
			reloaded, err := LoadYouTrackLink(path)
			if err != nil || reloaded == nil {
				t.Fatalf("reload: %v, %v", reloaded, err)
			}
			if reloaded.URL != link.URL || reloaded.Project != "ACME" ||
				reloaded.FieldMap["status"].Field != "State" {
				t.Errorf("reloaded link = %+v", reloaded)
			}
		})
	}
}
