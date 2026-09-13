package mapping

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The knowledge-base half of the mapping package: the transform between a
// git-in-track page and a YouTrack article.
//
// Five rules govern it, and each of them is decided once here so that the
// publish job and the pull job cannot drift apart:
//
//  1. The title lives in exactly one place. YouTrack keeps it in `summary`,
//     and the article body never carries it as an H1. Going up, a leading H1
//     is removed; coming down, the summary is written to the page's `title`
//     front-matter key and nothing is prepended to the body. A round trip
//     therefore cannot end up with the title twice.
//
//  2. The `## Feedback` block never leaves the repository. It is local review
//     commentary (ADR-030), it is stripped from every outgoing payload, and
//     because core.PruneKbFeedback rewrites it on every write, its absence
//     upstream is never evidence of a remote change — compare with
//     EqualContent, which ignores the block on both sides.
//
//  3. Front matter is stripped going up and rebuilt coming down. The body
//     YouTrack stores is Markdown only; the page's front matter is the local
//     side's business and is carried over key for key, with `title` and the
//     `external` entry of this system refreshed.
//
//  4. A wikilink becomes an article link only when its target is a page that
//     is already published. Anything else degrades to the text the link
//     displayed, with a warning: a published article must not carry a link
//     that resolves nowhere.
//
//  5. An attachment is addressed by file name. YouTrack resolves an image or a
//     link target against the entity's own attachments rather than against a
//     URL, so local references are rewritten to bare file names on the way up
//     and back to the paths the page used on the way down.
//
// Two YouTrack Markdown extensions are handled explicitly rather than by
// accident. `{color:red}…{color}` and `{width=300px}` are passed through
// untouched on the way up — they are YouTrack's own syntax and it is YouTrack
// they are going to — and preserved, with one warning each, on the way down,
// so that a round trip is byte-stable. This is deliberately not what
// NormalizeDescription does for issue descriptions, which strips both: an
// issue description is imported once, while a page is round-tripped.
//
// Bare issue identifiers such as "ACME-42" are never wrapped in a link in
// either direction. YouTrack auto-links them server-side, and wrapping one
// here would double-wrap it on the way back.

// Feedback block markers, mirroring the unexported constants of
// internal/core/kbfeedback.go (ADR-030). They are duplicated rather than
// exported from core because core is the writer of the block and this package
// is only ever its reader.
const (
	feedbackBegin = "<!-- gintrack:feedback:begin -->"
	feedbackEnd   = "<!-- gintrack:feedback:end -->"
)

// PageRef is what the transform needs to know about one knowledge-base page in
// order to resolve a link to it or from it. The caller builds the list from its
// index; this package resolves nothing on its own, for the same reason
// Relations exists on the issue side.
type PageRef struct {
	// Slug is the page path relative to the documentation folder, without the
	// ".md" extension: the form a wikilink addresses it by
	// (core.KBPage.Slug).
	Slug string `json:"slug"`
	// Title is the page title, used for nothing but diagnostics.
	Title string `json:"title,omitempty"`
	// ArticleID is the YouTrack readable id of the article the page is
	// published as, such as "ACME-A-3". An empty ArticleID means the page has
	// never been published, and a link to it degrades to text.
	ArticleID string `json:"articleId,omitempty"`
	// URL is the article URL recorded in the page's `external` entry. It is
	// matched literally when converting a link back into a wikilink, which is
	// what makes a link recorded under an older context path still resolve.
	URL string `json:"url,omitempty"`
}

// articleURL returns the URL a link to this page points at: the recorded one
// when there is one, else one built from the instance base URL. With neither
// the bare article id is returned, which YouTrack still auto-links.
func (r PageRef) articleURL(baseURL string) string {
	if r.URL != "" {
		return r.URL
	}
	if base := strings.TrimRight(strings.TrimSpace(baseURL), "/"); base != "" {
		return fmt.Sprintf("%s/article/%s", base, r.ArticleID)
	}
	return r.ArticleID
}

// PageIndex resolves page references in both directions. It is built once per
// publish or pull run and is read-only afterwards, so it is safe to share.
type PageIndex struct {
	bySlug    map[string]PageRef
	byArticle map[string]PageRef
	byURL     map[string]PageRef
}

// NewPageIndex builds the index. A page is addressable by its full slug and,
// when that is unambiguous, by its base name too, which is the second
// addressing form of docs/03 section 14.1. A base name shared by two pages
// resolves to neither, because guessing would publish a link to the wrong
// page. A nil or empty list yields a usable index in which nothing resolves.
func NewPageIndex(refs []PageRef) *PageIndex {
	x := &PageIndex{
		bySlug:    make(map[string]PageRef, len(refs)*2),
		byArticle: make(map[string]PageRef, len(refs)),
		byURL:     make(map[string]PageRef, len(refs)),
	}
	ambiguous := map[string]bool{}
	for _, ref := range refs {
		ref.Slug = strings.Trim(strings.TrimSpace(ref.Slug), "/")
		ref.ArticleID = strings.TrimSpace(ref.ArticleID)
		ref.URL = strings.TrimRight(strings.TrimSpace(ref.URL), "/")
		if ref.Slug == "" {
			continue
		}
		x.bySlug[ref.Slug] = ref
		if ref.ArticleID != "" {
			x.byArticle[ref.ArticleID] = ref
		}
		if ref.URL != "" {
			x.byURL[ref.URL] = ref
		}
		if base := path.Base(ref.Slug); base != ref.Slug {
			if _, taken := x.bySlug[base]; taken {
				ambiguous[base] = true
				continue
			}
			x.bySlug[base] = ref
		}
	}
	for base := range ambiguous {
		delete(x.bySlug, base)
	}
	return x
}

// BySlug resolves a wikilink target to a page.
func (x *PageIndex) BySlug(slug string) (PageRef, bool) {
	if x == nil {
		return PageRef{}, false
	}
	ref, ok := x.bySlug[strings.Trim(strings.TrimSpace(slug), "/")]
	return ref, ok
}

// ByArticle resolves a YouTrack article id to a page.
func (x *PageIndex) ByArticle(id string) (PageRef, bool) {
	if x == nil {
		return PageRef{}, false
	}
	ref, ok := x.byArticle[strings.TrimSpace(id)]
	return ref, ok
}

// ByURL resolves an article URL, as recorded in a page's external reference,
// to a page.
func (x *PageIndex) ByURL(url string) (PageRef, bool) {
	if x == nil {
		return PageRef{}, false
	}
	ref, ok := x.byURL[strings.TrimRight(strings.TrimSpace(url), "/")]
	return ref, ok
}

// PageOptions is everything the page transform needs beyond the page itself.
// Its zero value is usable: nothing resolves, every wikilink degrades to text
// with a warning, and attachment references are resolved against the page's
// own folder.
type PageOptions struct {
	// Options carries the instance base URL and the synced_at stamp, exactly
	// as it does for the issue mappers. FieldMap and ItemID are unused here:
	// an article has no custom fields.
	Options

	// Pages resolves wikilink targets and article links. A nil index is legal
	// and means nothing resolves.
	Pages *PageIndex
}

// AttachmentRef is one local file a published article needs. Name is the file
// name YouTrack will resolve the rewritten reference by; Path is the
// vault-relative path of the bytes to upload. Uploading them is the caller's
// job — this package has no filesystem — and the content it returns already
// refers to them by Name, so the upload and the update can be sent in either
// order.
type AttachmentRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ArticlePayload is what a page becomes: the two fields of an article that
// carry content, plus the attachments the content depends on.
type ArticlePayload struct {
	// Summary is the article title. It is the only place the title lives.
	Summary string `json:"summary"`
	// Content is the article body in Markdown: no front matter, no feedback
	// block, no H1 title.
	Content string `json:"content"`
	// Attachments are the local files Content references by name, ordered by
	// name so that a publish run is reproducible.
	Attachments []AttachmentRef `json:"attachments,omitempty"`
}

// Input renders the payload as the body of a create or an update. The caller
// sets ProjectID on a create and ParentArticleID when it is placing the
// article in the tree; both are outside what a page's content can say.
func (p ArticlePayload) Input() youtrack.ArticleInput {
	summary, content := p.Summary, p.Content
	return youtrack.ArticleInput{Summary: &summary, Content: &content}
}

// kbState accumulates what one body's rewrite learned. Warnings are collected
// per (field, value, reason) so that a repeated construct is reported once.
type kbState struct {
	baseURL  string
	dir      string
	pages    *PageIndex
	uploads  map[string]string
	locals   map[string]string
	warnings map[Warning]bool
}

func newKBState(opts PageOptions, pagePath string) *kbState {
	return &kbState{
		baseURL:  opts.BaseURL,
		dir:      path.Dir(path.Clean(strings.TrimPrefix(strings.TrimSpace(pagePath), "/"))),
		pages:    opts.Pages,
		uploads:  map[string]string{},
		locals:   map[string]string{},
		warnings: map[Warning]bool{},
	}
}

func (s *kbState) warn(field, value, fallback, reason string) {
	s.warnings[Warning{Field: field, Value: value, Fallback: fallback, Reason: reason}] = true
}

// collect renders the accumulated warnings in the package's deterministic
// order.
func (s *kbState) collect() []Warning {
	out := make([]Warning, 0, len(s.warnings))
	for w := range s.warnings {
		out = append(out, w)
	}
	return sortWarnings(out)
}

// attachments renders the recorded uploads, ordered by name.
func (s *kbState) attachments() []AttachmentRef {
	if len(s.uploads) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.uploads))
	for name := range s.uploads {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]AttachmentRef, 0, len(names))
	for _, name := range names {
		out = append(out, AttachmentRef{Name: name, Path: s.uploads[name]})
	}
	return out
}

// PageToArticle turns a knowledge-base page into the article payload that
// publishes it.
//
// The front matter, the `## Feedback` block and a leading H1 repeating the
// title are all removed; the title is returned in Summary and nowhere else.
// Wikilinks to published pages become article links and every other wikilink
// degrades to its text with a warning. Local file references are rewritten to
// the bare names YouTrack resolves against the article's attachments, and the
// files behind them are returned in Attachments for the caller to upload.
//
// It never fails: anything it could not translate is a Warning, so a publish
// run finishes and reports.
func PageToArticle(page core.KBPage, opts PageOptions) (ArticlePayload, []Warning) {
	s := newKBState(opts, page.Path)

	body := stripFrontMatter(page.Body)
	body, _ = splitFeedback(body)

	summary := strings.TrimSpace(page.Title)
	body, h1 := cutLeadingH1(body)
	if summary == "" {
		summary = h1
	}
	if h1 != "" && summary != "" && h1 != summary {
		s.warn("summary", h1, summary,
			"the page body opened with an H1 that differs from the page title; "+
				"the title was published and the H1 dropped, because YouTrack keeps the title in summary alone")
	}
	if summary == "" {
		s.warn("summary", "", "",
			"the page has neither a title nor a leading H1, and a YouTrack article cannot be created without a summary")
	}

	body = rewriteMarkdown(body, s.wikilinksToArticleLinks)
	body = rewriteMarkdown(body, s.attachmentsToNames)

	return ArticlePayload{
		Summary:     summary,
		Content:     strings.TrimRight(body, "\n"),
		Attachments: s.attachments(),
	}, s.collect()
}

// ArticleToPage turns an article back into the body and the front matter of
// the page it mirrors.
//
// The returned body is the article content with article links converted back
// to wikilinks, attachment names resolved back to the paths the existing page
// referenced them by, and the existing page's feedback block appended
// verbatim. The title is not written into the body: it goes into the `title`
// key of the returned front matter, which is the same single rule
// PageToArticle applies upwards. The front matter is the existing page's, key
// for key, with `title` refreshed and the `external` entry of this system set
// to the article.
//
// existing may be a zero KBPage — that is a page being created from an article
// for the first time — in which case there is no feedback block to preserve
// and no attachment reference to resolve.
func ArticleToPage(article youtrack.Article, existing core.KBPage, opts PageOptions) (body string, front map[string]any, warnings []Warning) {
	s := newKBState(opts, existing.Path)
	s.locals = localAttachments(stripFrontMatter(existing.Body))

	body, _ = cutLeadingH1(article.Content)
	body = rewriteMarkdown(body, s.articleLinksToWikilinks)
	body = rewriteMarkdown(body, s.namesToAttachments)
	s.warnYouTrackMarkup(body)

	_, feedback := splitFeedback(stripFrontMatter(existing.Body))
	body = strings.TrimRight(body, "\n")
	if feedback != "" {
		if body != "" {
			body += "\n\n"
		}
		body += feedback
	}

	front = cloneFrontMatter(existing.FrontMatter)
	if summary := strings.TrimSpace(article.Summary); summary != "" {
		front["title"] = summary
	} else if _, ok := front["title"]; !ok && existing.Title != "" {
		front["title"] = existing.Title
	}
	if ref := articleExternal(article, opts); ref.Valid() {
		front["external"] = mergeExternal(existing.ExternalRefs, ref)
	}
	return body, front, s.collect()
}

// articleExternal builds the external reference the page records for the
// article it mirrors. The readable id is preferred over the internal one
// because that is what a human recognizes and what a URL carries.
func articleExternal(article youtrack.Article, opts PageOptions) core.External {
	id := strings.TrimSpace(article.IDReadable)
	if id == "" {
		id = strings.TrimSpace(article.ID)
	}
	return opts.External(id, "article")
}

// mergeExternal returns the page's `external` front-matter value with the
// entry for this system replaced and every other system kept, in the shape
// core.externalsFromAny reads back: a list of mappings whose keys are the YAML
// names of core.External.
func mergeExternal(existing []core.External, ref core.External) []any {
	out := make([]any, 0, len(existing)+1)
	replaced := false
	for _, e := range existing {
		if e.Ref() == ref.Ref() {
			out = append(out, externalMap(ref))
			replaced = true
			continue
		}
		out = append(out, externalMap(e))
	}
	if !replaced {
		out = append(out, externalMap(ref))
	}
	return out
}

// externalMap renders one external reference as a YAML mapping.
func externalMap(e core.External) map[string]any {
	e = core.NormalizeExternal(e)
	m := map[string]any{"system": e.System, "id": e.ID}
	if e.URL != "" {
		m["url"] = e.URL
	}
	if e.Key != "" {
		m["key"] = e.Key
	}
	if !e.SyncedAt.IsZero() {
		m["synced_at"] = e.SyncedAt.String()
	}
	return m
}

// cloneFrontMatter copies the page's front matter one level deep, which is all
// this package changes. A nil map yields an empty one, so the caller always
// gets something it can write to.
func cloneFrontMatter(front map[string]any) map[string]any {
	out := make(map[string]any, len(front)+2)
	for k, v := range front {
		out[k] = v
	}
	return out
}

// EqualContent reports whether two page bodies carry the same content, with
// the feedback block ignored on both sides.
//
// This is the comparison a pull must use. core.PruneKbFeedback rewrites the
// block on every write (ADR-030 R-FB-4), and the block never leaves the
// repository at all, so comparing raw bodies would report a remote change
// every time a note is added, moved or pruned locally. Leading and trailing
// blank lines are ignored for the same reason: they are normalisation, not
// content.
func EqualContent(a, b string) bool {
	left, _ := splitFeedback(stripFrontMatter(a))
	right, _ := splitFeedback(stripFrontMatter(b))
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

// localAttachments maps the base name of every local file a body references to
// the reference as it was written, which is how a bare attachment name coming
// down is put back where it was. A base name used by two different references
// is ambiguous and is left out, so that a pull never moves a file.
func localAttachments(body string) map[string]string {
	out := map[string]string{}
	ambiguous := map[string]bool{}
	rewriteMarkdown(body, func(text string) string {
		for _, m := range kbLinkRe.FindAllStringSubmatch(text, -1) {
			target := trimAngles(m[3])
			if !isLocalFileRef(target) {
				continue
			}
			name := path.Base(target)
			if prev, ok := out[name]; ok && prev != target {
				ambiguous[name] = true
			}
			out[name] = target
		}
		return text
	})
	for name := range ambiguous {
		delete(out, name)
	}
	return out
}

// warnYouTrackMarkup reports the YouTrack Markdown extensions a pulled body
// carries. They are preserved rather than stripped — see the package rules at
// the top of this file — but a page that renders in the web app with a literal
// "{color:red}" in it is worth one line in the report.
func (s *kbState) warnYouTrackMarkup(body string) {
	if colorExtRe.MatchString(body) {
		s.warn("content", "{color:…}", "preserved",
			"the body uses the YouTrack {color:…} extension, which is not portable Markdown; "+
				"it was preserved so that the round trip is stable, and will render literally outside YouTrack")
	}
	if widthExtRe.MatchString(body) {
		s.warn("content", "{width=…}", "preserved",
			"the body uses the YouTrack {width=…} image sizing extension, which is not portable Markdown; "+
				"it was preserved so that the round trip is stable, and will render literally outside YouTrack")
	}
}

// stripFrontMatter removes a leading YAML front-matter block from a body.
//
// core.KBPage.Body never carries one — core.ParsePage has already split it off
// — so this is defensive, for a KBPage a caller built by hand. It applies
// exactly the test ParsePage applies, a well-formed block that decodes to a
// non-empty mapping, so that a body opening with a "---" thematic rule is
// never mistaken for front matter.
func stripFrontMatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	block, rest, err := core.SplitFrontMatter([]byte(body))
	if err != nil {
		return body
	}
	fm := map[string]any{}
	if yaml.Unmarshal(block, &fm) != nil || len(fm) == 0 {
		return body
	}
	return rest
}

// splitFeedback separates a page body from its feedback block.
//
// The block is recognized by the same rule core.parseKbFeedbackDoc applies:
// the end marker must be the last non-blank line of the body, and the begin
// marker must appear before it outside a fenced code block. A page that merely
// documents the format in a code sample therefore has no block, which is the
// whole point of that rule.
func splitFeedback(body string) (content, feedback string) {
	if !strings.Contains(body, feedbackBegin) {
		return body, ""
	}
	lines := strings.Split(body, "\n")
	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last < 0 || strings.TrimSpace(lines[last]) != feedbackEnd {
		return body, ""
	}
	begin, fence := -1, ""
	for i := 0; i < last; i++ {
		if m := fenceRe.FindStringSubmatch(lines[i]); fence == "" && m != nil {
			fence = m[1]
			continue
		} else if fence != "" {
			if closesFence(lines[i], fence) {
				fence = ""
			}
			continue
		}
		if strings.TrimSpace(lines[i]) == feedbackBegin {
			begin = i
		}
	}
	if begin < 0 {
		return body, ""
	}
	head := strings.Join(lines[:begin], "\n")
	return strings.TrimRight(head, "\n"), strings.Join(lines[begin:last+1], "\n")
}

// cutLeadingH1 removes the first ATX H1 of a body when it is the first thing in
// it, and returns its text. Only a leading H1 is removed: an H1 further down is
// a section of the document, not a repeated title.
func cutLeadingH1(body string) (rest, title string) {
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return body, ""
	}
	trimmed := strings.TrimSpace(lines[i])
	if !strings.HasPrefix(trimmed, "# ") && trimmed != "#" {
		return body, ""
	}
	title = strings.TrimSpace(strings.TrimRight(strings.TrimPrefix(trimmed, "#"), " #"))
	tail := lines[i+1:]
	for len(tail) > 0 && strings.TrimSpace(tail[0]) == "" {
		tail = tail[1:]
	}
	return strings.Join(tail, "\n"), title
}
