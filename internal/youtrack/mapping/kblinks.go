package mapping

import (
	"path"
	"regexp"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Link and attachment rewriting for knowledge-base pages. The rules are the
// half of the page transform that touches the Markdown itself; kbpage.go holds
// the document-level rules (front matter, the title and the feedback block).

var (
	// kbWikilinkRe matches a wikilink, optionally in its embedding form. It is
	// the same grammar core.ParsePage scans with; the inside is decoded by
	// core.ParseWikilink so that the two never drift.
	kbWikilinkRe = regexp.MustCompile(`(!?)\[\[([^\[\]\n]+)\]\]`)
	// kbLinkRe matches an inline Markdown link or image embed and captures the
	// "!" of an embed, the text, the target and an optional title. Reference
	// links and autolinks are deliberately not matched: they are passed
	// through byte for byte.
	kbLinkRe = regexp.MustCompile(`(!?)\[((?:[^\[\]\\]|\\.)*)\]\(\s*(<[^<>]*>|[^()\s]+)((?:\s+"[^"]*")?)\s*\)`)
	// kbArticlePathRe matches the tail of a YouTrack article URL, which is
	// "/article/<id>" in the UI and "/articles/<id>" in the REST API.
	kbArticlePathRe = regexp.MustCompile(`/articles?/([A-Za-z0-9_-]+)$`)
)

// rewriteMarkdown applies fn to every stretch of prose of a Markdown body:
// fenced code blocks and inline code spans are exempt, so a page that
// documents the wikilink or the attachment syntax survives the round trip
// unchanged. It is the shared traversal of both directions of the transform.
func rewriteMarkdown(body string, fn func(string) string) string {
	if body == "" {
		return ""
	}
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
		var b strings.Builder
		for _, seg := range splitCodeSpans(line) {
			if seg.code {
				b.WriteString(seg.text)
				continue
			}
			b.WriteString(fn(seg.text))
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// wikilinksToArticleLinks rewrites every `[[…]]` of a body on the way up.
//
// A target that is a page the index knows and that is already published as an
// article becomes an inline Markdown link to that article. Everything else —
// an unknown page, a page that has never been published, a backlog item —
// degrades to the plain text the link displayed, with one warning naming the
// target, because a dead link in a published article is worse than a phrase.
//
// The link text is the alias when the wikilink carries one and the target
// exactly as it was written when it does not. Using the target rather than the
// page title is what makes articleLinksToWikilinks the exact inverse: a page
// title would come back as an alias the author never wrote.
func (s *kbState) wikilinksToArticleLinks(text string) string {
	return kbWikilinkRe.ReplaceAllStringFunc(text, func(m string) string {
		parts := kbWikilinkRe.FindStringSubmatch(m)
		embed, raw := parts[1] == "!", parts[2]
		w := core.ParseWikilink(raw, embed)
		display := w.Text
		if display == "" {
			display = strings.TrimSpace(strings.SplitN(raw, "|", 2)[0])
		}

		ref, ok := s.resolvePage(w)
		if !ok {
			s.warn("links", raw, display,
				"the wikilink target is not a page published as a YouTrack article, so the link was replaced by its text")
			return display
		}
		if embed {
			s.warn("links", raw, "a link",
				"a wikilink transclusion has no YouTrack equivalent and was flattened into a link")
		}
		url := ref.articleURL(s.baseURL)
		if w.Anchor != "" {
			url += "#" + w.Anchor
		}
		return "[" + display + "](" + url + ")"
	})
}

// resolvePage looks a wikilink up in the page index. A link that addresses a
// backlog item is never a page and is refused before the lookup: YouTrack
// auto-links bare issue ids server-side, and this package never wraps one.
func (s *kbState) resolvePage(w core.Wikilink) (PageRef, bool) {
	if s.pages == nil || w.IsItem() || w.Target == "" {
		return PageRef{}, false
	}
	ref, ok := s.pages.BySlug(w.Target)
	if !ok || ref.ArticleID == "" {
		return PageRef{}, false
	}
	return ref, true
}

// articleLinksToWikilinks rewrites inline links on the way down. A link whose
// target is an article the page index knows becomes a wikilink again; every
// other link is left exactly as it was, because this package cannot tell an
// intentional external URL from a stale one.
func (s *kbState) articleLinksToWikilinks(text string) string {
	return kbLinkRe.ReplaceAllStringFunc(text, func(m string) string {
		parts := kbLinkRe.FindStringSubmatch(m)
		if parts[1] == "!" {
			return m
		}
		display, target := parts[2], trimAngles(parts[3])
		base, anchor, hasAnchor := cutArticleAnchor(target)
		ref, ok := s.lookupArticle(base)
		if !ok {
			return m
		}
		inside := ref.Slug
		if hasAnchor && anchor != "" {
			inside += "#" + anchor
		}
		if display != "" && display != inside {
			inside += "|" + display
		}
		return "[[" + inside + "]]"
	})
}

// lookupArticle resolves a link target to a page: first by the URL as written,
// then by the article id the URL ends with, so that a link recorded against a
// different context path than the one configured today still resolves.
func (s *kbState) lookupArticle(target string) (PageRef, bool) {
	if s.pages == nil || target == "" {
		return PageRef{}, false
	}
	if ref, ok := s.pages.ByURL(target); ok {
		return ref, true
	}
	m := kbArticlePathRe.FindStringSubmatch(strings.TrimRight(target, "/"))
	if m == nil {
		return PageRef{}, false
	}
	return s.pages.ByArticle(m[1])
}

// cutArticleAnchor splits a link target on its last "#". It reports false when
// there is no fragment, so that an empty fragment ("…/ACME-A-3#") is not
// confused with none.
func cutArticleAnchor(target string) (base, anchor string, ok bool) {
	i := strings.LastIndex(target, "#")
	if i < 0 {
		return target, "", false
	}
	return target[:i], target[i+1:], true
}

// trimAngles removes the pointy brackets of a "<…>" link target.
func trimAngles(target string) string {
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		return target[1 : len(target)-1]
	}
	return target
}

// attachmentsToNames rewrites the local file references of a body on the way up
// and records the files that have to be uploaded.
//
// YouTrack resolves an image or a link target against the entity's own
// attachments by file name, not by URL, so "![diagram](./images/diagram.png)"
// must become "![diagram](diagram.png)" and the file must be uploaded to the
// article. A target that is a URL, an anchor, or another Markdown page is left
// alone. Two different files with the same base name cannot both be resolved
// by that name, so the second one is reported and left untouched rather than
// silently pointed at the first one's bytes.
func (s *kbState) attachmentsToNames(text string) string {
	return kbLinkRe.ReplaceAllStringFunc(text, func(m string) string {
		parts := kbLinkRe.FindStringSubmatch(m)
		bang, display, target, title := parts[1], parts[2], trimAngles(parts[3]), parts[4]
		if !isLocalFileRef(target) {
			return m
		}
		name := path.Base(target)
		local, err := s.resolveLocal(target)
		if err != nil {
			s.warn("attachments", target, "", err.Error())
			return m
		}
		if seen, ok := s.uploads[name]; ok && seen != local {
			s.warn("attachments", target, "",
				"another file is already attached under the name "+name+
					", and YouTrack resolves an attachment by name, so the reference was left as it is")
			return m
		}
		s.uploads[name] = local
		return bang + "[" + display + "](" + name + title + ")"
	})
}

// namesToAttachments is the inverse: a bare attachment name coming down is
// rewritten to the path the page referenced it by before it was published.
// A name the page never referenced is left as it is and reported, because this
// package has no filesystem and cannot invent a location for it.
func (s *kbState) namesToAttachments(text string) string {
	return kbLinkRe.ReplaceAllStringFunc(text, func(m string) string {
		parts := kbLinkRe.FindStringSubmatch(m)
		bang, display, target, title := parts[1], parts[2], trimAngles(parts[3]), parts[4]
		if !isAttachmentName(target) || isMarkdownRef(target) || path.Ext(target) == "" {
			return m
		}
		local, ok := s.locals[target]
		if !ok {
			s.warn("attachments", target, "",
				"the article references an attachment the page does not, so the reference was kept as a bare file name")
			return m
		}
		return bang + "[" + display + "](" + local + title + ")"
	})
}

// resolveLocal turns a reference as written into a vault-relative path, so that
// the caller that owns the filesystem knows which bytes to upload. A reference
// climbing out of the vault is refused: publishing must never read outside it.
func (s *kbState) resolveLocal(target string) (string, error) {
	if strings.HasPrefix(target, "/") {
		return path.Clean(strings.TrimPrefix(target, "/")), nil
	}
	joined := path.Clean(path.Join(s.dir, target))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", errOutsideVault
	}
	return joined, nil
}

// errOutsideVault is the reason reported for a reference that climbs out of the
// vault. It is a value rather than a sentence built per occurrence so that the
// warning text is stable in the golden files.
var errOutsideVault = errReason(
	"the reference points outside the vault, so the file was not uploaded and the reference was left as it is")

// errReason is a reason string usable as an error.
type errReason string

func (e errReason) Error() string { return string(e) }

// isLocalFileRef reports whether a link target names a file of the vault: a
// relative or root-relative path that is not a URL, not an anchor and not
// another Markdown page (a page link is a wikilink's job, not an attachment's).
func isLocalFileRef(target string) bool {
	if target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "//") {
		return false
	}
	if isMarkdownRef(target) {
		return false
	}
	// A scheme ("https:", "mailto:", "data:") is never a local file. The check
	// is on the segment before the first slash so that "a:b/c.png" — a legal,
	// if unwise, relative path — is not mistaken for a scheme.
	head := target
	if i := strings.IndexByte(head, '/'); i >= 0 {
		head = head[:i]
	}
	if strings.Contains(head, ":") {
		return false
	}
	return path.Ext(target) != ""
}

// isMarkdownRef reports whether a target addresses another Markdown document.
func isMarkdownRef(target string) bool {
	base, _, _ := cutArticleAnchor(target)
	return strings.EqualFold(path.Ext(base), ".md")
}
