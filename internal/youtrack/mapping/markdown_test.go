package mapping

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/youtrack"
)

func TestNormalizeDescription(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		itemID   string
		want     string
		warnings []string
	}{
		{
			name:   "an attachment embed is rewritten under the item's folder",
			body:   "![shot](screenshot.png)",
			itemID: "ACME-US-0042",
			want:   "![shot](.pmngr/attachments/ACME-US-0042/screenshot.png)",
		},
		{
			name:   "the title of an embed survives",
			body:   `![shot](screenshot.png "the failing page")`,
			itemID: "ACME-US-0042",
			want:   `![shot](.pmngr/attachments/ACME-US-0042/screenshot.png "the failing page")`,
		},
		{
			name:   "an absolute url is left alone",
			body:   "![shot](https://cdn.example.com/screenshot.png)",
			itemID: "ACME-US-0042",
			want:   "![shot](https://cdn.example.com/screenshot.png)",
		},
		{
			name:   "a protocol-relative url is left alone",
			body:   "![shot](//cdn.example.com/screenshot.png)",
			itemID: "ACME-US-0042",
			want:   "![shot](//cdn.example.com/screenshot.png)",
		},
		{
			name:   "a data uri is left alone",
			body:   "![dot](data:image/png;base64,iVBORw0KGgo=)",
			itemID: "ACME-US-0042",
			want:   "![dot](data:image/png;base64,iVBORw0KGgo=)",
		},
		{
			name:   "a target with a path is left alone",
			body:   "![shot](docs/screenshot.png)",
			itemID: "ACME-US-0042",
			want:   "![shot](docs/screenshot.png)",
		},
		{
			name:   "a plain link is not an embed",
			body:   "[the file](screenshot.png)",
			itemID: "ACME-US-0042",
			want:   "[the file](screenshot.png)",
		},
		{
			name:     "without an item id nothing is rewritten and it is reported",
			body:     "![shot](screenshot.png)",
			want:     "![shot](screenshot.png)",
			warnings: []string{"description: screenshot.png: an attachment embed could not be rewritten because the item id is not known yet"},
		},
		{
			name:     "the color extension loses its markers and keeps the text",
			body:     "{color:red}Blocks the release.{color}",
			itemID:   "ACME-US-0042",
			want:     "Blocks the release.",
			warnings: []string{"description: {color:…}: the YouTrack {color:…} extension is not portable Markdown; the markers were removed and the text kept"},
		},
		{
			name:     "the width extension is removed",
			body:     "![shot](https://x/y.png){width=300px}",
			itemID:   "ACME-US-0042",
			want:     "![shot](https://x/y.png)",
			warnings: []string{"description: {width=…}: the YouTrack {width=…} image sizing extension is not portable Markdown and was removed"},
		},
		{
			name:   "a bare issue id is never wrapped in a link",
			body:   "Caused by ACME-42, see also ACME-100 and ACME-7.",
			itemID: "ACME-US-0042",
			want:   "Caused by ACME-42, see also ACME-100 and ACME-7.",
		},
		{
			name:   "a checkbox list survives untouched",
			body:   "- [ ] confirm on 17.4\n- [x] reproduce",
			itemID: "ACME-US-0042",
			want:   "- [ ] confirm on 17.4\n- [x] reproduce",
		},
		{
			name:   "a fenced code block is exempt",
			body:   "```md\n{color:red}x{color}\n![shot](screenshot.png)\n```",
			itemID: "ACME-US-0042",
			want:   "```md\n{color:red}x{color}\n![shot](screenshot.png)\n```",
		},
		{
			name:   "a tilde fence is exempt too",
			body:   "~~~\n{width=1px}\n~~~",
			itemID: "ACME-US-0042",
			want:   "~~~\n{width=1px}\n~~~",
		},
		{
			name:   "an inline code span is exempt",
			body:   "Write `{color:red}` to tint text.",
			itemID: "ACME-US-0042",
			want:   "Write `{color:red}` to tint text.",
		},
		{
			name:   "a double-backtick span is exempt",
			body:   "Write ``![x](y.png)`` verbatim.",
			itemID: "ACME-US-0042",
			want:   "Write ``![x](y.png)`` verbatim.",
		},
		{
			name:   "an unterminated backtick is prose",
			body:   "A ` stray tick and ![shot](screenshot.png)",
			itemID: "ACME-US-0042",
			want:   "A ` stray tick and ![shot](.pmngr/attachments/ACME-US-0042/screenshot.png)",
		},
		{
			name:   "a body with nothing to do comes back byte for byte",
			body:   "## Steps\n\nOpen the app, then sign in.\n",
			itemID: "ACME-US-0042",
			want:   "## Steps\n\nOpen the app, then sign in.\n",
		},
		{name: "an empty body stays empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings := NormalizeDescription(tc.body, tc.itemID)
			if got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
			if !equalJSON(t, warningLines(warnings), tc.warnings) {
				t.Errorf("got warnings %v, want %v", warningLines(warnings), tc.warnings)
			}
		})
	}
}

// TestNormalizeDescriptionReportsOncePerBody keeps an import report readable
// when a long description uses the same extension twenty times.
func TestNormalizeDescriptionReportsOncePerBody(t *testing.T) {
	body := strings.Repeat("{color:red}x{color}\n", 20)
	got, warnings := NormalizeDescription(body, "ACME-US-0042")
	if strings.Contains(got, "{color") {
		t.Errorf("a marker survived: %q", got)
	}
	if len(warnings) != 1 {
		t.Errorf("got warnings %v, want exactly one", warningLines(warnings))
	}
}

// TestNormalizeDescriptionPrefixIsConfigurable checks the seam Options exposes.
func TestNormalizeDescriptionPrefixIsConfigurable(t *testing.T) {
	issue := youtrack.Issue{IDReadable: "ACME-42", Description: "![shot](screenshot.png)"}
	draft, _, _ := IssueToDraft(issue, Options{ItemID: "ACME-US-0042", AttachmentPrefix: "assets/"})
	if draft.Body != "![shot](assets/ACME-US-0042/screenshot.png)" {
		t.Errorf("got %q", draft.Body)
	}
}
