package pandosync

import (
	"fmt"
	"path"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Corpus layout. Every exported path is corpus-relative and uses forward
// slashes, the way core.FS wants it.
const (
	itemsDir = "items"
	kbDir    = "kb"
	// mdExt is the only extension the corpus contains. Pando's directory walk
	// has no hidden-directory or node_modules exclusion, so anything else in
	// the corpus root would be indexed as if it were a document.
	mdExt = ".md"
	// tmpPrefix marks the half-written file of an atomic write. It is a dot
	// file without the .md extension so that an importer scanning the corpus
	// mid-write never picks it up.
	tmpPrefix = "."
	tmpSuffix = ".tmp"
)

// ErrForbiddenPath is returned when an exported path would leave the corpus
// root. Item ids and page paths come from files on disk, so a crafted one is a
// real input, not a theoretical one.
type ErrForbiddenPath struct {
	Path   string
	Reason string
}

func (e *ErrForbiddenPath) Error() string {
	return fmt.Sprintf("pandosync: path %q is refused: %s", e.Path, e.Reason)
}

func refuse(p, why string) error { return &ErrForbiddenPath{Path: p, Reason: why} }

// itemPath returns the corpus-relative path of an item document. The id is
// validated against the id grammar, which already rules out a separator, so the
// check is a belt-and-braces one that fails loudly instead of writing outside
// the root.
func itemPath(id core.ItemID, fallbackProject string) (string, error) {
	if !id.Valid() {
		return "", refuse(string(id), "it is not a well-formed item id")
	}
	project := projectOfItem(id, fallbackProject)
	if project == "" {
		return "", refuse(string(id), "no project could be derived from the id")
	}
	return confine(path.Join(project, itemsDir, string(id)+mdExt), project+"/"+itemsDir)
}

// pagePath returns the corpus-relative path of a knowledge-base page document
// from a vault-relative or docs-relative page path.
func pagePath(rel, project string) (string, error) {
	if project == "" {
		return "", refuse(rel, "the page belongs to no project")
	}
	clean, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(clean), mdExt) {
		clean += mdExt
	}
	return confine(path.Join(project, kbDir, clean), project+"/"+kbDir)
}

// cleanRel applies the lexical half of the check, the same one
// internal/mcp/paths.go runs on a tool argument: no empty path, no NUL, no
// backslash, nothing absolute and no `..` segment in any spelling.
func cleanRel(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", refuse(p, "a page path is required")
	}
	if strings.ContainsRune(p, 0) {
		return "", refuse(p, "the path contains a NUL byte")
	}
	if strings.ContainsRune(p, '\\') {
		return "", refuse(p, "the path contains a backslash; corpus paths always use forward slashes")
	}
	if strings.HasPrefix(p, "/") || volumeName(p) != "" {
		return "", refuse(p, "the path is absolute")
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || clean == "/" {
		return "", refuse(p, "the path does not name a file")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".." {
			return "", refuse(p, "the path escapes the corpus root")
		}
	}
	return clean, nil
}

// confine re-checks the assembled path after cleaning and asserts that it still
// sits under the directory it was built for. Cleaning happens once more here
// because path.Join already cleans, and a segment that survived the lexical
// check could still collapse the prefix away.
func confine(full, prefix string) (string, error) {
	clean := path.Clean(full)
	if clean != full || !strings.HasPrefix(clean, prefix+"/") {
		return "", refuse(full, "the path resolves outside "+prefix)
	}
	return clean, nil
}

// volumeName reports the Windows volume of a path ("C:") on every platform, so
// that a corpus written on Linux refuses the same spellings a Windows one does.
func volumeName(p string) string {
	if len(p) < 2 || p[1] != ':' {
		return ""
	}
	c := p[0]
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return p[:2]
	}
	return ""
}

// tempPathFor returns the sibling temporary path an atomic write goes through.
func tempPathFor(p string) string {
	dir, base := path.Split(p)
	return dir + tmpPrefix + base + tmpSuffix
}

// isTempName reports whether a directory entry is a leftover temporary file
// this package wrote, which a crash between write and rename can leave behind.
func isTempName(name string) bool {
	return strings.HasPrefix(name, tmpPrefix) && strings.HasSuffix(name, tmpSuffix)
}
