package server

import (
	"os"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
)

// The host half of GIT-T-0226: `integrations.youtrack.land_in_inbox` is read
// from project.yaml and carried across the seam into vault.YouTrackLink, which
// is where the importer reads it back to decide whether an issue it creates
// arrives in the project's triage queue or in its backlog (R-INT-7).

// TestLandInInboxReachesTheVaultLink covers both settings end to end: the key
// as a project wrote it by hand, through the client resolution the jobs use, to
// the plain struct internal/vault takes.
func TestLandInInboxReachesTheVaultLink(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		want bool
	}{
		{name: "a project that said nothing lands in the backlog", want: false},
		{name: "a project that asked for triage carries the setting", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := copyTree(t, fixtureRoot)
			link := &config.YouTrackLink{URL: "https://yt.example.com/youtrack", Project: "ACME"}
			if _, err := config.SaveYouTrackLink(ytProjectYAML(root), *link); err != nil {
				t.Fatalf("write the link: %v", err)
			}
			if tc.want {
				writeLandInInbox(t, ytProjectYAML(root))
			}

			// The second save the helper performs must leave the hand-written
			// key alone, which is the rule SaveYouTrackLink already follows.
			s := newLinkedYouTrackServer(t, root, link)
			_, carried, err := s.youtrack.jobClientFor(ytProjectKey)
			if err != nil {
				t.Fatalf("resolve the client: %v", err)
			}
			if carried.LandInInbox != tc.want {
				t.Errorf("landInInbox = %v, want %v", carried.LandInInbox, tc.want)
			}
		})
	}
}

// writeLandInInbox adds `land_in_inbox: true` to the `integrations.youtrack`
// block of a project.yaml, the way a team sets the option today: by hand, since
// the settings screen does not own the key.
func writeLandInInbox(t *testing.T, path string) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	const block = "\n  youtrack:\n"
	if !strings.Contains(string(raw), block) {
		t.Fatalf("project.yaml holds no youtrack block:\n%s", raw)
	}
	edited := strings.Replace(string(raw), block, block+"    land_in_inbox: true\n", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatalf("write project.yaml: %v", err)
	}
	reloaded, err := config.LoadYouTrackLink(path)
	if err != nil || reloaded == nil || !reloaded.LandInInbox {
		t.Fatalf("the option did not land in the block (%v):\n%s", err, edited)
	}
}
