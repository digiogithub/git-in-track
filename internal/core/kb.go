package core

import (
	"bytes"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// KBPage is one knowledge-base page: a Markdown file of the documentation folder
// that lives outside .pmngr/. Pages are free Markdown; their front matter is
// optional and never validated (docs/03 section 14).
//
// The index stores metadata only. Rendering is the caller's job: this package
// extracts the title, the front matter, the headings and the links, and nothing
// else.
type KBPage struct {
	// Path is the vault-relative path of the file, forward slashes.
	Path string `json:"path"`
	// RelPath is the path relative to the documentation folder of the project,
	// which is how wikilinks address a page.
	RelPath string `json:"rel_path"`
	// Project is the key of the project whose docs folder holds the page.
	Project ProjectKey `json:"project,omitempty"`

	Title       string         `json:"title"`
	Tags        []string       `json:"tags,omitempty"`
	FrontMatter map[string]any `json:"frontmatter,omitempty"`
	Headings    []Heading      `json:"headings,omitempty"`
	Links       []Wikilink     `json:"links,omitempty"`
	External    []string       `json:"external,omitempty"`
	Updated     Timestamp      `json:"updated,omitempty"`
	Size        int64          `json:"size,omitempty"`
	Rev         Rev            `json:"rev"`

	// Body is the Markdown after the front matter. It is kept in memory so that
	// text filters and Search work without re-reading the file; it is never part
	// of a snapshot (R-IDX-2).
	Body string `json:"-"`
}

// Slug returns the page path without its .md extension, which is the form a
// wikilink uses to address it.
func (p *KBPage) Slug() string { return strings.TrimSuffix(p.RelPath, ".md") }

// Heading is one ATX heading of a Markdown document.
type Heading struct {
	Level  int    `json:"level"`
	Text   string `json:"text"`
	Anchor string `json:"anchor"`
}

// Wikilink is one `[[…]]` reference found in a page or in an item body. The
// three addressing forms of docs/03 section 14.1 and docs/04 section 4 are all
// decoded into this struct.
type Wikilink struct {
	// Raw is the text between the brackets, exactly as written.
	Raw string `json:"raw"`
	// Target is the address with the project prefix, the anchor and the alias
	// removed: a page path without ".md", or an item id.
	Target string `json:"target"`
	// Project is the key of the project the link points into, empty for a link
	// that stays inside the current project.
	Project ProjectKey `json:"project,omitempty"`
	// Item is set when Target matches the id grammar (R-WIKI-1).
	Item ItemID `json:"item,omitempty"`
	// Anchor is the part after "#": a heading on a page, a comment stem on an item.
	Anchor string `json:"anchor,omitempty"`
	// Text is the alias after "|", empty when the link has none.
	Text string `json:"text,omitempty"`
	// Embed reports the "![[…]]" transclusion form.
	Embed bool `json:"embed,omitempty"`
}

// IsItem reports whether the link addresses a backlog item rather than a page.
func (w Wikilink) IsItem() bool { return w.Item != "" }

// wikilinkRE matches a wikilink, optionally in its embedding form.
var wikilinkRE = regexp.MustCompile(`(!?)\[\[([^\[\]\n]+)\]\]`)

// mdLinkRE matches the target of an inline Markdown link.
var mdLinkRE = regexp.MustCompile(`\]\(\s*<?([^)\s>]+)`)

// autolinkRE matches an angle-bracket autolink.
var autolinkRE = regexp.MustCompile(`<((?:https?|mailto):[^>\s]+)>`)

// inlineCodeRE matches a code span, which never contains links to index.
var inlineCodeRE = regexp.MustCompile("`+[^`\n]*`+")

// ParsePage turns the bytes of a Markdown page into knowledge-base metadata.
//
// It never fails. A page without front matter, or whose front matter is not a
// YAML mapping, is indexed with its body alone: knowledge-base pages are free
// Markdown and a broken block must not hide the page from the tree.
func ParsePage(filePath, relPath string, data []byte) *KBPage {
	p := &KBPage{
		Path:    filePath,
		RelPath: relPath,
		Size:    int64(len(data)),
		Rev:     ComputeRev(data),
	}
	body := normalizeText(data)
	if block, rest, err := SplitFrontMatter(data); err == nil {
		fm := map[string]any{}
		if yaml.Unmarshal(block, &fm) == nil && len(fm) > 0 {
			p.FrontMatter = fm
		}
		body = rest
	}
	p.Body = body
	p.Headings, p.Links, p.External = scanMarkdown(body)

	if p.FrontMatter != nil {
		p.Title = strings.TrimSpace(stringOf(p.FrontMatter["title"]))
		p.Tags = anyToStrings(p.FrontMatter["tags"])
		if ts, err := ParseTimestamp(stringOf(p.FrontMatter["updated"])); err == nil {
			p.Updated = ts
		}
	}
	if p.Title == "" {
		for _, h := range p.Headings {
			if h.Level == 1 {
				p.Title = h.Text
				break
			}
		}
	}
	if p.Title == "" {
		p.Title = humanizeFileName(filePath)
	}
	return p
}

// scanMarkdown walks a document once and extracts its headings, its wikilinks
// and its external links. Fenced code blocks and code spans are skipped so that
// a Mermaid diagram or a shell snippet never contributes a link.
func scanMarkdown(body string) (headings []Heading, links []Wikilink, external []string) {
	var fence string
	seenExternal := make(map[string]bool)
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		clean := inlineCodeRE.ReplaceAllString(line, " ")
		if h, ok := parseHeading(clean); ok {
			headings = append(headings, h)
		}
		for _, m := range wikilinkRE.FindAllStringSubmatch(clean, -1) {
			links = append(links, ParseWikilink(m[2], m[1] == "!"))
		}
		for _, m := range mdLinkRE.FindAllStringSubmatch(clean, -1) {
			if isExternalURL(m[1]) && !seenExternal[m[1]] {
				seenExternal[m[1]] = true
				external = append(external, m[1])
			}
		}
		for _, m := range autolinkRE.FindAllStringSubmatch(clean, -1) {
			if !seenExternal[m[1]] {
				seenExternal[m[1]] = true
				external = append(external, m[1])
			}
		}
	}
	return headings, links, external
}

// parseHeading decodes an ATX heading line.
func parseHeading(line string) (Heading, bool) {
	trimmed := strings.TrimLeft(line, " ")
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
		return Heading{}, false
	}
	text := strings.TrimSpace(strings.TrimRight(trimmed[level+1:], " #"))
	if text == "" {
		return Heading{}, false
	}
	return Heading{Level: level, Text: text, Anchor: Slugify(text)}, true
}

// ParseWikilink decodes the inside of a `[[…]]` reference.
//
// Accepted forms (docs/03 section 14.1, docs/04 section 4):
//
//	architecture/overview          page by path, ".md" omitted
//	overview                       page by basename
//	ACME-US-0042                   backlog item by id
//	ACME-US-0042#20260901T1045Z-jose   a comment of that item
//	WEB/WEB-US-0031                item of another project
//	ACME:architecture/overview     page of another project
//	ACME-US-0042|the SSO story     any of the above with custom link text
func ParseWikilink(raw string, embed bool) Wikilink {
	w := Wikilink{Raw: raw, Embed: embed}
	rest := raw
	if i := strings.Index(rest, "|"); i >= 0 {
		w.Text = strings.TrimSpace(rest[i+1:])
		rest = rest[:i]
	}
	if i := strings.Index(rest, "#"); i >= 0 {
		w.Anchor = strings.TrimSpace(rest[i+1:])
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)

	// "KEY:page/path" — a page of another project.
	if i := strings.Index(rest, ":"); i > 0 {
		if key := ProjectKey(rest[:i]); ValidProjectKey(key) {
			w.Project = key
			rest = strings.TrimSpace(rest[i+1:])
		}
	}
	// "KEY/ITEM-ID" — an item of another project.
	if i := strings.Index(rest, "/"); i > 0 && w.Project == "" {
		if key := ProjectKey(rest[:i]); ValidProjectKey(key) && ItemID(rest[i+1:]).Valid() {
			w.Project = key
			rest = rest[i+1:]
		}
	}
	w.Target = rest
	if ItemID(rest).Valid() {
		w.Item = ItemID(rest)
		if w.Project == "" {
			key, _, _, err := ParseItemID(rest)
			if err == nil {
				w.Project = key
			}
		}
	}
	return w
}

// TreeNode is one node of the knowledge-base tree returned by KbTree: a folder
// or a page, children sorted folders-first and then by name.
type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Title    string      `json:"title,omitempty"`
	IsDir    bool        `json:"is_dir"`
	Children []*TreeNode `json:"children,omitempty"`
}

// buildTree assembles a tree out of a set of pages. root is the path the tree is
// rooted at, and every page path must live under it.
func buildTree(root string, pages []*KBPage) *TreeNode {
	node := &TreeNode{Name: path.Base(root), Path: root, IsDir: true}
	if root == "." || root == "" {
		node.Name = "."
		node.Path = "."
	}
	dirs := map[string]*TreeNode{node.Path: node}
	ensureDir := func(dir string) *TreeNode {
		if n, ok := dirs[dir]; ok {
			return n
		}
		// Create every missing ancestor from the root downwards.
		var missing []string
		for d := dir; d != node.Path && d != "." && d != "/"; d = path.Dir(d) {
			if _, ok := dirs[d]; ok {
				break
			}
			missing = append(missing, d)
		}
		for i := len(missing) - 1; i >= 0; i-- {
			d := missing[i]
			parent, ok := dirs[path.Dir(d)]
			if !ok {
				parent = node
			}
			child := &TreeNode{Name: path.Base(d), Path: d, IsDir: true}
			parent.Children = append(parent.Children, child)
			dirs[d] = child
		}
		if n, ok := dirs[dir]; ok {
			return n
		}
		return node
	}
	for _, p := range pages {
		parent := ensureDir(path.Dir(p.Path))
		parent.Children = append(parent.Children, &TreeNode{
			Name:  path.Base(p.Path),
			Path:  p.Path,
			Title: p.Title,
		})
	}
	sortTree(node)
	return node
}

// sortTree orders children folders first, then by name, recursively.
func sortTree(n *TreeNode) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return a.Name < b.Name
	})
	for _, c := range n.Children {
		sortTree(c)
	}
}

// normalizeText strips a UTF-8 BOM, converts CRLF to LF and trims the blank
// lines that surround a document.
func normalizeText(data []byte) string {
	clean := bytes.TrimPrefix(data, bom)
	clean = bytes.ReplaceAll(clean, []byte("\r\n"), []byte("\n"))
	return strings.Trim(string(clean), "\n")
}

// humanizeFileName turns "architecture/sso-overview.md" into "sso overview". It
// is the last-resort title of a page with no front matter and no H1.
func humanizeFileName(p string) string {
	base := strings.TrimSuffix(path.Base(p), ".md")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

// anyToStrings coerces a front-matter value into a list of strings, accepting
// both the sequence form and a comma-separated scalar.
func anyToStrings(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := strings.TrimSpace(stringOf(e)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), t...)
	case string:
		var out []string
		for _, part := range strings.Split(t, ",") {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(stringOf(v)); s != "" {
			return []string{s}
		}
		return nil
	}
}

// isExternalURL reports whether a Markdown link target leaves the vault.
func isExternalURL(target string) bool {
	lower := strings.ToLower(target)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:")
}
