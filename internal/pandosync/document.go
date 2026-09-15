package pandosync

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/digiogithub/git-in-track/internal/core"
)

// idSeparator joins the id and the title on the first body line. It is an em
// dash so that the line reads as prose in a search snippet rather than as a
// key-value pair a parser might try to strip.
const idSeparator = " — "

// RenderItem serializes one item as a corpus document: front matter, the
// identity line, then the body verbatim so that `[[GIT-US-0024]]` wikilinks
// survive into Pando's link graph.
//
// The output is byte-stable for equal input, which is what lets the exporter
// skip a write for an unchanged item.
func RenderItem(it *core.Item, project string) []byte {
	if project == "" {
		project = projectOfItem(it.ID, "")
	}
	fm := []field{
		{"id", scalar(string(it.ID))},
		{"type", scalar(string(it.Type))},
		{"title", scalar(it.Title)},
		{"status", scalar(string(it.Status))},
		{"milestone", scalar(string(it.Milestone))},
		{"parent", scalar(string(it.Parent))},
		{"project", scalar(project)},
		{"updated", scalar(it.Updated.String())},
		{"tags", sequence(itemTags(it))},
		{"aliases", sequence(dedupe([]string{it.Title}))},
	}
	return document(fm, string(it.ID)+idSeparator+displayTitle(it.Title), it.Body)
}

// RenderPage serializes one knowledge-base page. The page has no id of its own,
// so the identity line carries its wikilink slug, which is how another page
// addresses it.
func RenderPage(p *core.KBPage, project string) []byte {
	if project == "" {
		project = string(p.Project)
	}
	rel := pageRelPath(p)
	slug := strings.TrimSuffix(rel, ".md")
	fm := []field{
		{"path", scalar(rel)},
		{"title", scalar(p.Title)},
		{"project", scalar(project)},
		{"updated", scalar(p.Updated.String())},
		{"tags", sequence(dedupe(p.Tags))},
		{"aliases", sequence(dedupe([]string{p.Title}))},
	}
	return document(fm, "Page: "+slug+idSeparator+displayTitle(p.Title), p.Body)
}

// itemTags is the tag list Pando actually keeps: the item id first, so a search
// for the id matches the tag as well as the body, then the labels, the type and
// the status.
func itemTags(it *core.Item) []string {
	tags := make([]string, 0, len(it.Labels)+3)
	tags = append(tags, string(it.ID))
	tags = append(tags, it.Labels...)
	tags = append(tags, string(it.Type), string(it.Status))
	return dedupe(tags)
}

// pageRelPath is the page path relative to its project documentation folder,
// falling back to the vault-relative path for a page the index could not place.
func pageRelPath(p *core.KBPage) string {
	if p.RelPath != "" {
		return p.RelPath
	}
	return p.Path
}

// displayTitle keeps the identity line on one line whatever the title holds.
func displayTitle(title string) string {
	title = strings.ReplaceAll(title, "\r\n", " ")
	title = strings.ReplaceAll(title, "\n", " ")
	return strings.TrimSpace(title)
}

// field is one front-matter key with its already-built node. A nil node means
// the key is omitted, which is how an empty value is written (docs/03 §3.2:
// empty values are omitted, never written as null).
type field struct {
	key  string
	node *yaml.Node
}

// document assembles the fenced front matter, the identity line and the body.
func document(fields []field, identity, body string) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(renderFrontMatter(fields))
	b.WriteString("---\n\n")
	b.WriteString(identity)
	b.WriteString("\n")
	body = strings.TrimLeft(body, "\n")
	if body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

// renderFrontMatter encodes the fields through yaml.v3 so that a title holding
// a colon, a quote or a leading dash is escaped the way any YAML reader expects,
// while the key order stays the one given.
func renderFrontMatter(fields []field) string {
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, f := range fields {
		if f.node == nil {
			continue
		}
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f.key},
			f.node,
		)
	}
	if len(mapping.Content) == 0 {
		return ""
	}
	out, err := yaml.Marshal(mapping)
	if err != nil {
		// A mapping of plain string scalars cannot fail to encode; if the
		// encoder ever disagrees, say so in the document rather than writing a
		// file that silently lost its tags.
		return fmt.Sprintf("# front matter could not be encoded: %v\n", err)
	}
	return string(out)
}

// scalar builds a string node, or nil for an empty value.
func scalar(v string) *yaml.Node {
	if v == "" {
		return nil
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// sequence builds a flow-style list of strings, or nil for an empty list.
func sequence(values []string) *yaml.Node {
	if len(values) == 0 {
		return nil
	}
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, v := range values {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	return n
}

// dedupe drops empty and repeated entries while keeping the first occurrence,
// so the output stays stable for a given input.
func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// projectOfItem returns the project key encoded in an item id, or fallback when
// the id does not parse.
func projectOfItem(id core.ItemID, fallback string) string {
	key, _, _, err := core.ParseItemID(string(id))
	if err != nil {
		return fallback
	}
	return string(key)
}
