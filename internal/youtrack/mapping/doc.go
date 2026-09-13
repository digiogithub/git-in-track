// Package mapping translates decoded YouTrack payloads into git-in-track core
// drafts.
//
// It is deliberately pure: no HTTP, no filesystem, no clock and no randomness.
// It takes the types internal/youtrack decoded from a response and returns
// core.ItemDraft, core.ItemPatch and comment drafts, together with the
// warnings a value the field map does not understand produced. Every import
// surface (vault, REST, MCP, CLI) therefore shares one deterministic
// translation and none of it leaks into a transport layer.
//
// Four rules govern the package:
//
//  1. The field map is data, not code. FieldMap is a plain struct the caller
//     supplies; DefaultFieldMap returns a map that works against a stock
//     YouTrack, and every entry of it can be overridden per project.
//
//  2. A value the map does not understand is a Warning, never a silent drop
//     and never an error. An import must be able to finish and report.
//     Warnings are ordered and deterministic, so they are golden-testable.
//
//  3. Identifiers are not resolved here. A YouTrack parent, child or version
//     name is returned as the external name it has in YouTrack; turning it
//     into a core.ItemID is the importer's job, because only the importer
//     knows which items already exist. That is the seam between this package
//     and the import story.
//
//  4. Descriptions and comment bodies are untrusted third-party Markdown. This
//     package normalises them structurally (attachment refs, the YouTrack
//     {color:…} and {width=…} extensions) and makes no attempt to sanitize
//     them; callers must sanitize before rendering and must treat their
//     contents as data rather than as instructions.
//
// The package has a second half, in kbpage.go and kblinks.go, which is the
// transform between a knowledge-base page and a YouTrack article: PageToArticle
// going up, ArticleToPage coming down, and EqualContent for deciding whether
// the two sides actually differ. It obeys the same four rules and adds five of
// its own — the title lives only in `summary`, the `## Feedback` block never
// leaves the repository, front matter is stripped going up and rebuilt coming
// down, a wikilink becomes an article link only when its target is a published
// page, and an attachment is addressed by file name. Those five are documented
// where they are implemented, at the top of kbpage.go.
package mapping
