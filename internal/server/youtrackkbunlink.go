package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// Forgetting the article a knowledge-base page mirrors, story GIT-US-0095.
//
// Publishing writes an `external:` entry into a page's front matter and every
// later synchronization is decided from it: which article to update, and which
// fingerprint to compare against. Unlinking removes that entry and nothing
// else. Three properties make it safe to offer next to the destructive-looking
// buttons it sits with:
//
//   - It is local. The article is not deleted, not archived and not touched;
//     the instance is not called at all, so an unreachable YouTrack does not
//     stop somebody from cleaning up a reference here.
//   - It is not "disconnect". The project's connection, its token and its field
//     map are untouched: the next publish of this page simply creates a new
//     article instead of updating the one it had forgotten.
//   - It does not need a connection. A page keeps its reference after a project
//     is disconnected, and cleaning one up afterwards is exactly when it is
//     wanted, so the route resolves the project without asking whether it is
//     linked to anything.
//
// The confirmation belongs to the caller: this is a one-line write, and what
// makes it reversible is republishing, not undo.

// youtrackKBUnlinkRequest is the body of POST …/youtrack/kb/unlink.
type youtrackKBUnlinkRequest struct {
	// Path is the vault-relative page. It names one page: a folder-wide unlink
	// would be a bulk edit of committed files behind a single click.
	Path string `json:"path"`
}

// youtrackKBUnlinkResult reports what the write did.
type youtrackKBUnlinkResult struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	// Unlinked is false when the page carried no YouTrack reference, which is
	// an answer rather than a failure: the caller asked for a state the page is
	// already in, and nothing was written.
	Unlinked bool `json:"unlinked"`
	// ArticleID is the article the page mirrored, empty when it mirrored none.
	ArticleID string `json:"articleId,omitempty"`
}

// handleYouTrackKBUnlink serves POST …/youtrack/kb/unlink.
func (s *Server) handleYouTrackKBUnlink(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackKBProject(w, r)
	if !ok {
		return
	}
	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes "+key+".")
		return
	}
	var body youtrackKBUnlinkRequest
	if r.ContentLength != 0 && !decodeBody(w, r, &body) {
		return
	}
	target := strings.TrimSpace(body.Path)
	if target == "" {
		failProblem(w, r, codeInvalidRequest, `"path": name the page to unlink.`)
		return
	}

	result, err := s.unlinkKBPage(r.Context(), m, key, target)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// unlinkKBPage drops the YouTrack entry from one page's front matter.
//
// It reads and writes through the same two vault methods a pull does, so the
// feedback block, the commit pipeline and the optimistic lock all behave
// exactly as they do for any other page write.
func (s *Server) unlinkKBPage(
	ctx context.Context, m *mount, project, target string,
) (youtrackKBUnlinkResult, error) {
	run := &kbRun{
		mount: m, project: project, docs: docsFolderOf(m, project),
		direction: kbDirectionPublish, parents: map[string]string{},
	}
	pagePath := target
	if !strings.HasPrefix(pagePath, run.docs+"/") && run.docs != "." {
		pagePath = vaultPath(run.docs, strings.Trim(target, "/"))
	}

	page, rev, front, err := s.readKBPage(ctx, run, pagePath)
	if err != nil {
		return youtrackKBUnlinkResult{}, err
	}
	out := youtrackKBUnlinkResult{Project: project, Path: page.Path}
	ref, linked := youtrackExternalOf(page.ExternalRefs)
	if !linked {
		return out, nil
	}
	out.ArticleID, out.Unlinked = ref.ID, true

	updated := mergeFrontMatter(front, nil)
	kept := withoutExternalSystem(page.ExternalRefs, mapping.System)
	if len(kept) == 0 {
		// An empty list is an absent key: a page that mirrors nothing says so
		// by having no `external:` block, the way it did before it was ever
		// published.
		delete(updated, "external")
	} else {
		updated["external"] = externalsToFrontMatter(kept)
	}
	if err := s.writeKBPage(ctx, run, page.Path, rev, updated, page.Body); err != nil {
		return youtrackKBUnlinkResult{}, err
	}
	s.log.Info("a knowledge-base page was unlinked from its article",
		"project", project, "page", page.Path, "article", out.ArticleID)
	return out, nil
}

// withoutExternalSystem drops every reference of one system, keeping the rest
// in order: an item may mirror more than one tracker, and forgetting YouTrack
// must not forget the others with it.
func withoutExternalSystem(refs []core.External, system string) []core.External {
	out := make([]core.External, 0, len(refs))
	for _, ref := range refs {
		if ref.Ref().System == system {
			continue
		}
		out = append(out, ref)
	}
	return out
}
