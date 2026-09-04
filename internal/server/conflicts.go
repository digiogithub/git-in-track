package server

import (
	"net/http"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
)

// The conflict resolver's server half, story GIT-US-0022 (docs/06-git-sync.md
// section 5, docs/07-cli-and-api.md section 5.5).
//
// Two routes: one reads the three versions of a conflicted path and the merge
// the core proposes for them, the other writes a resolution back, stages it and
// finishes the rebase or merge. The merge itself is `internal/core`, so browser
// mode resolves conflicts with the same rules and the same code.

// conflictFileResponse is GET /api/v1/sync/conflicts/file.
type conflictFileResponse struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	// Kind is content, delete-modify, add-add or unknown.
	Kind string `json:"kind"`
	// Operation is the rebase or merge the conflict belongs to.
	Operation string `json:"operation,omitempty"`
	// Strategy mirrors Operation as the sync vocabulary uses it.
	Strategy gitops.Strategy `json:"strategy,omitempty"`
	// Versions are the three sides plus the working copy.
	Versions gitops.ConflictVersions `json:"versions"`
	// Merge is what the core proposes: the field decisions, the body hunks and
	// the canonical merged file. It is absent for a binary conflict, where the
	// only resolutions are keep-ours and keep-theirs.
	Merge *core.MergeResult `json:"merge,omitempty"`
}

// conflictResolveRequest is POST /api/v1/sync/conflicts/resolve.
type conflictResolveRequest struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	// Resolution is "ours", "theirs", "merged" or "manual".
	Resolution string `json:"resolution"`
	// Content is the file the user edited; it is required for "manual" and
	// ignored otherwise.
	Content string `json:"content,omitempty"`
	// Body replaces the merged body, keeping the merged front matter.
	Body string `json:"body,omitempty"`
	// Fields overrides a front-matter decision: field to ours, theirs or base.
	Fields map[string]string `json:"fields,omitempty"`
	// Hunks overrides a body hunk: index to ours, theirs, both, base, edited.
	Hunks map[string]string `json:"hunks,omitempty"`
	// HunkText carries the text of an edited hunk, keyed by the same index.
	HunkText map[string]string `json:"hunkText,omitempty"`
	// Continue asks for the rebase or merge to be resumed once nothing is left
	// conflicted. It defaults to true: a resolution that stops short of
	// finishing is the exception, not the rule.
	Continue *bool `json:"continue,omitempty"`
}

// conflictResolveResponse reports what a resolution did.
type conflictResolveResponse struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	// Merge is the merge the resolution produced, so the UI can show what was
	// written without reading the file back.
	Merge core.MergeResult `json:"merge"`
	// Result is the git half: staged, remaining, continued, status.
	Result gitops.ResolveResult `json:"result"`
	// Status is the repository row after the resolution.
	Status syncRepoStatus `json:"status"`
}

// The resolutions the API accepts.
const (
	resolutionOurs   = "ours"
	resolutionTheirs = "theirs"
	resolutionMerged = "merged"
	resolutionManual = "manual"
)

// handleSyncConflictFile serves GET /api/v1/sync/conflicts/file?repo=&path=.
func (s *Server) handleSyncConflictFile(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		failProblem(w, r, codeInvalidRequest, "A conflict request needs ?path=<file>.")
		return
	}
	backend, _, ok := s.conflictBackend(w, r, repo)
	if !ok {
		return
	}
	versions, err := backend.ConflictFile(r.Context(), path)
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	out := conflictFileResponse{Repo: repo, Path: path, Kind: versions.Kind, Versions: versions}
	if st, statusErr := backend.SyncStatus(r.Context()); statusErr == nil {
		out.Operation = st.Operation
		out.Strategy = strategyOf(st.Operation)
	}
	if !versions.Binary {
		merge, mergeErr := core.MergeFiles(path, mergeInputOf(versions), nil)
		if mergeErr != nil {
			failProblem(w, r, codeInvalidRequest,
				"The three versions of "+path+" could not be merged: "+mergeErr.Error())
			return
		}
		out.Merge = &merge
	}
	writeJSON(w, r, http.StatusOK, out)
}

// handleSyncConflictResolve serves POST /api/v1/sync/conflicts/resolve.
//
// It merges with the user's decisions, writes the canonical result, stages it
// and — unless the caller asked otherwise — continues the integration. Abort
// stays available at every step: nothing here throws work away.
func (s *Server) handleSyncConflictResolve(w http.ResponseWriter, r *http.Request) {
	var body conflictResolveRequest
	if !decodeBody(w, r, &body) {
		return
	}
	path := strings.TrimSpace(body.Path)
	if path == "" {
		failProblem(w, r, codeInvalidRequest, "A resolution needs the `path` of the conflicted file.")
		return
	}
	resolution, ok := conflictResolutionOf(w, r, body)
	if !ok {
		return
	}
	backend, m, ok := s.conflictBackend(w, r, body.Repo)
	if !ok {
		return
	}
	versions, err := backend.ConflictFile(r.Context(), path)
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	if versions.Binary && resolution.Take == "" && resolution.Content == "" {
		failProblem(w, r, codeInvalidRequest,
			path+" is binary, so it can only be resolved with `ours`, `theirs` or an uploaded file.")
		return
	}

	merge, err := core.MergeFiles(path, mergeInputOf(versions), &resolution)
	if err != nil {
		failProblem(w, r, codeInvalidRequest, "The resolution of "+path+" could not be applied: "+err.Error())
		return
	}
	if !merge.Clean {
		failProblem(w, r, codeInvalidRequest,
			"The resolution of "+path+" still leaves conflicted hunks: decide every hunk, or use "+
				"keep mine, keep theirs or a manual edit.")
		return
	}

	proceed := body.Continue == nil || *body.Continue
	res, err := backend.ResolvePath(r.Context(), gitops.ResolveRequest{
		Path: path, Content: merge.Content, Continue: proceed,
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	s.hub.Publish(eventConflictResolved, map[string]any{
		"repo": m.id, "path": path, "resolution": body.Resolution,
		"continued": res.Continued, "remaining": len(res.Remaining),
	})
	writeJSON(w, r, http.StatusOK, conflictResolveResponse{
		Repo: m.id, Path: path, Merge: merge, Result: res,
		Status: s.syncStatusOf(r.Context(), m),
	})
}

// conflictResolutionOf maps the request onto the core's resolution, refusing a
// shape that would silently drop what the user meant.
func conflictResolutionOf(w http.ResponseWriter, r *http.Request, body conflictResolveRequest) (core.Resolution, bool) {
	out := core.Resolution{
		Body: body.Body, Fields: body.Fields, Hunks: body.Hunks, HunkText: body.HunkText,
	}
	switch body.Resolution {
	case resolutionOurs:
		out.Take = core.SideOurs
	case resolutionTheirs:
		out.Take = core.SideTheirs
	case resolutionManual:
		if body.Content == "" {
			failProblem(w, r, codeInvalidRequest,
				"A manual resolution needs the `content` of the resolved file.")
			return core.Resolution{}, false
		}
		out.Content = body.Content
	case resolutionMerged, "":
		// The merged resolution is the automatic merge plus whatever the user
		// flipped in it, which is what `fields` and `hunks` carry.
	default:
		failProblem(w, r, codeInvalidRequest,
			"Unknown resolution "+body.Resolution+": use ours, theirs, merged or manual.")
		return core.Resolution{}, false
	}
	return out, true
}

// conflictBackend resolves the repository a conflict call names.
func (s *Server) conflictBackend(w http.ResponseWriter, r *http.Request, repo string) (gitops.Backend, *mount, bool) {
	m, ok := s.repos.lookup(repo)
	if !ok {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+repo+".")
		return nil, nil, false
	}
	backend, ok := s.git.backendFor(m.id)
	if !ok {
		failProblem(w, r, codeInvalidRequest,
			"Repository "+m.id+" is not a git working tree: "+s.git.reasonFor(m.id))
		return nil, nil, false
	}
	return backend, m, true
}

// mergeInputOf maps the three index stages onto the core's merge input.
func mergeInputOf(v gitops.ConflictVersions) core.MergeInput {
	return core.MergeInput{Base: v.Base, Ours: v.Ours, Theirs: v.Theirs}
}

// strategyOf names the integration a half-finished operation belongs to.
func strategyOf(operation string) gitops.Strategy {
	if operation == gitops.OpMerge {
		return gitops.StrategyMerge
	}
	if operation == gitops.OpRebase {
		return gitops.StrategyRebase
	}
	return ""
}
