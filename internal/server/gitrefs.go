package server

import (
	"net/http"
	"strconv"

	"github.com/digiogithub/git-in-track/internal/gitops"
)

// The ref listing of GIT-US-0149: the branches and the recent commits the base
// and head pickers of the impact view offer. It is a read over the
// repository's backend — git, system git or jj alike — and writes nothing.

// Bounds of the `limit` of GET /api/v1/git/refs.
const (
	defaultRefCommits = 20
	maxRefCommits     = 200
)

// gitRefsResponse is the body of GET /api/v1/git/refs.
type gitRefsResponse struct {
	Repo    string `json:"repo"`
	Backend string `json:"backend"`
	// Branches are the local branches, then the remote-tracking ones, each by
	// name; a jj repository lists its bookmarks.
	Branches []gitops.Branch `json:"branches"`
	// Commits are the last commits reachable from HEAD (jj: `@`), newest
	// first.
	Commits []gitops.Commit `json:"commits"`
}

// handleGitRefs serves GET /api/v1/git/refs?repo=<id>&limit=<n>.
func (s *Server) handleGitRefs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo := q.Get("repo")
	if repo == "" {
		failProblem(w, r, codeInvalidRequest, "A ref listing needs ?repo=<id>.")
		return
	}
	limit := defaultRefCommits
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > maxRefCommits {
			failProblem(w, r, codeInvalidRequest,
				"limit must be a number of commits from 0 to "+strconv.Itoa(maxRefCommits)+".")
			return
		}
		limit = n
	}
	m, ok := s.repos.lookup(repo)
	if !ok {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+repo+".")
		return
	}
	backend, ok := s.git.backendFor(m.id)
	if !ok {
		failProblem(w, r, codeUnavailable,
			"Repository "+m.id+" has no git history to list refs from: "+s.git.reasonFor(m.id))
		return
	}
	branches, err := backend.Branches(r.Context())
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	commits := []gitops.Commit{}
	if limit > 0 {
		recent, logErr := backend.Commits(r.Context(), gitops.LogRequest{To: "HEAD", Limit: limit})
		switch {
		case logErr == nil:
			commits = recent
		case len(branches) > 0:
			writeGitError(w, r, logErr)
			return
		}
		// No branch and no readable HEAD is a repository without a commit
		// yet: an empty listing, not a failure.
	}
	writeJSON(w, r, http.StatusOK, gitRefsResponse{
		Repo: m.id, Backend: backend.Name(), Branches: branches, Commits: commits,
	})
}
