// Package pandosync keeps a Pando-indexable copy of the backlog on disk.
//
// The companion mirrors every item and every knowledge-base page into a corpus
// directory outside the repository, one Markdown file per source document:
//
//	<Dir>/<project>/items/<ITEM-ID>.md
//	<Dir>/<project>/kb/<relative page path>.md
//
// Pando imports that directory through `[Remembrances] KBPath` with
// `KBAutoImport = true` and **`KBWatch = false`**: its watcher does not parse
// front matter and strips `tags` from every document it re-processes, so the
// companion writes files and lets the scheduled import pass pick them up.
//
// # What survives the round trip
//
// Pando parses front matter into a fixed struct and discards every key other
// than `tags` and `aliases`. The front matter this package writes is therefore
// for a human reading the corpus, not a data channel: nothing downstream may
// read an item's status, milestone or parent back out of a Pando search hit.
// A consumer resolves a hit to an item id and re-reads the authoritative fields
// from git-in-track's own index. Because even the id is not guaranteed to come
// back, every exported document repeats it as the first line of the body, where
// it lands inside the indexed chunk whatever the parser keeps.
//
// # Dependencies
//
// The package depends on internal/core and on a narrow [Source] the vault
// satisfies. It deliberately does not import internal/server: the event hub
// lives there and adapts its own events to [Event], not the other way round.
package pandosync
