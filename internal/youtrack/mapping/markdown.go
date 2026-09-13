package mapping

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Regular expressions for the three YouTrack Markdown constructs this package
// touches. They are deliberately narrow: everything they do not match is
// passed through byte for byte.
var (
	// imageEmbedRe matches a Markdown image embed and captures the alt text,
	// the target and an optional title.
	imageEmbedRe = regexp.MustCompile(`!\[([^\]]*)\]\(\s*([^)\s]+)((?:\s+"[^"]*")?)\s*\)`)
	// colorExtRe matches both halves of the {color:red}…{color} extension.
	colorExtRe = regexp.MustCompile(`\{color(?::[^}]*)?\}`)
	// widthExtRe matches the {width=300px} sizing extension.
	widthExtRe = regexp.MustCompile(`\{width=[^}]*\}`)
	// fenceRe matches the opening or closing fence of a fenced code block.
	fenceRe = regexp.MustCompile("^\\s{0,3}(```+|~~~+)")
)

// NormalizeDescription turns an untrusted YouTrack description or comment body
// into the Markdown git-in-track stores. It is structural only: it never
// escapes, sanitizes or re-wraps anything, and a body it has nothing to do to
// comes back byte for byte.
//
// Three YouTrack-specific behaviors, each chosen explicitly:
//
//  1. Attachment image embeds. YouTrack resolves "![alt](file.png)" against the
//     entity's own attachments rather than against a URL, so a bare filename
//     means nothing once the body leaves YouTrack. Such a target is rewritten
//     to "<DefaultAttachmentPrefix>/<itemID>/file.png", which is where the
//     importer writes the downloaded file. A target that is already a URL
//     (any scheme, a protocol-relative "//host", a "data:" payload) or that
//     already contains a path separator is left alone, and so is every
//     non-image link. With an empty itemID nothing is rewritten and a warning
//     says so, because a path without the id would point at the wrong folder.
//
//  2. "{color:red}text{color}" is a YouTrack extension no CommonMark renderer
//     understands. Both markers are removed and the text between them is kept:
//     losing a color is acceptable, losing the sentence is not. One warning
//     per body reports it.
//
//  3. "{width=300px}" carries image sizing YouTrack appends after an embed.
//     There is no portable Markdown for it, so it is removed and reported.
//
// Bare issue identifiers such as "ACME-42" are deliberately left untouched:
// YouTrack auto-links them server-side, so wrapping them in a link here would
// double-wrap them on the way back.
//
// Fenced code blocks and inline code spans are exempt from all three rules —
// a body documenting the "{color:…}" syntax must survive the round trip.
func NormalizeDescription(body, itemID string) (string, []Warning) {
	return normalizeBody(body, itemID, DefaultAttachmentPrefix)
}

// normalizeBody is NormalizeDescription with the attachment prefix injected,
// which is how Options.AttachmentPrefix reaches it.
func normalizeBody(body, itemID, prefix string) (string, []Warning) {
	if body == "" {
		return "", nil
	}
	st := &bodyState{itemID: strings.TrimSpace(itemID), prefix: strings.Trim(prefix, "/")}
	lines := strings.Split(body, "\n")
	var fence string
	for i, line := range lines {
		m := fenceRe.FindStringSubmatch(line)
		switch {
		case fence == "" && m != nil:
			fence = m[1]
			continue
		case fence != "" && closesFence(line, fence):
			fence = ""
			continue
		case fence != "":
			continue
		}
		lines[i] = st.rewriteLine(line)
	}
	return strings.Join(lines, "\n"), sortWarnings(st.warnings())
}

// closesFence reports whether a line is the closing fence of an open block: a
// run of at least as many of the same fence characters and nothing else.
func closesFence(line, fence string) bool {
	trimmed := strings.TrimRight(strings.TrimLeft(line, " "), " \t")
	if len(trimmed) < len(fence) {
		return false
	}
	return strings.Trim(trimmed, fence[:1]) == ""
}

// bodyState accumulates what one body's rewrite learned, so that a warning is
// reported once per body rather than once per occurrence.
type bodyState struct {
	itemID  string
	prefix  string
	color   bool
	width   bool
	unnamed map[string]bool
}

// warnings renders the accumulated state as warnings.
func (s *bodyState) warnings() []Warning {
	var out []Warning
	if s.color {
		out = append(out, Warning{
			Field:  "description",
			Value:  "{color:…}",
			Reason: "the YouTrack {color:…} extension is not portable Markdown; the markers were removed and the text kept",
		})
	}
	if s.width {
		out = append(out, Warning{
			Field:  "description",
			Value:  "{width=…}",
			Reason: "the YouTrack {width=…} image sizing extension is not portable Markdown and was removed",
		})
	}
	names := make([]string, 0, len(s.unnamed))
	for name := range s.unnamed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, Warning{
			Field:  "description",
			Value:  name,
			Reason: "an attachment embed could not be rewritten because the item id is not known yet",
		})
	}
	return out
}

// rewriteLine applies the three rules to the parts of a line that are not
// inline code spans.
func (s *bodyState) rewriteLine(line string) string {
	var b strings.Builder
	for _, seg := range splitCodeSpans(line) {
		if seg.code {
			b.WriteString(seg.text)
			continue
		}
		b.WriteString(s.rewriteText(seg.text))
	}
	return b.String()
}

// rewriteText applies the three rules to one stretch of prose.
func (s *bodyState) rewriteText(text string) string {
	text = imageEmbedRe.ReplaceAllStringFunc(text, func(m string) string {
		parts := imageEmbedRe.FindStringSubmatch(m)
		alt, target, title := parts[1], parts[2], parts[3]
		if !isAttachmentName(target) {
			return m
		}
		if s.itemID == "" {
			if s.unnamed == nil {
				s.unnamed = make(map[string]bool)
			}
			s.unnamed[target] = true
			return m
		}
		return fmt.Sprintf("![%s](%s/%s/%s%s)", alt, s.prefix, s.itemID, target, title)
	})
	if widthExtRe.MatchString(text) {
		s.width = true
		text = widthExtRe.ReplaceAllString(text, "")
	}
	if colorExtRe.MatchString(text) {
		s.color = true
		text = colorExtRe.ReplaceAllString(text, "")
	}
	return text
}

// isAttachmentName reports whether an image target is a bare attachment file
// name rather than a URL or a path. A name with no separator and no scheme is
// the only shape YouTrack resolves against the entity's attachments.
func isAttachmentName(target string) bool {
	if target == "" || strings.ContainsAny(target, "/\\") {
		return false
	}
	if strings.HasPrefix(target, "<") || strings.HasPrefix(target, "#") {
		return false
	}
	// A scheme ("data:", "mailto:") or a Windows drive letter is never a name.
	return !strings.Contains(target, ":")
}

// segment is one stretch of a line, either code or prose.
type segment struct {
	text string
	code bool
}

// splitCodeSpans splits a line into alternating prose and inline-code
// segments. A backtick run opens a span that the next run of the same length
// closes; an unterminated run is prose, which is what CommonMark says.
func splitCodeSpans(line string) []segment {
	var (
		out   []segment
		start int
		i     int
	)
	for i < len(line) {
		if line[i] != '`' {
			i++
			continue
		}
		run := 1
		for i+run < len(line) && line[i+run] == '`' {
			run++
		}
		closeAt := indexBacktickRun(line, i+run, run)
		if closeAt < 0 {
			i += run
			continue
		}
		if i > start {
			out = append(out, segment{text: line[start:i]})
		}
		end := closeAt + run
		out = append(out, segment{text: line[i:end], code: true})
		i, start = end, end
	}
	if start < len(line) {
		out = append(out, segment{text: line[start:]})
	}
	return out
}

// indexBacktickRun returns the offset of the next run of exactly n backticks at
// or after from, or -1.
func indexBacktickRun(line string, from, n int) int {
	for i := from; i < len(line); i++ {
		if line[i] != '`' {
			continue
		}
		run := 1
		for i+run < len(line) && line[i+run] == '`' {
			run++
		}
		if run == n {
			return i
		}
		i += run - 1
	}
	return -1
}
