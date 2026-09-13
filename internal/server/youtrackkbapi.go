package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// The HTTP surface of the knowledge-base synchronization, task GIT-T-0214.
//
// Three routes over the three core methods internal/vault already implements:
// "youtrack.kb.status", "youtrack.kb.publish" and "youtrack.kb.pull". The
// handlers hold no synchronization logic of their own and never talk to
// YouTrack: status compares what the repository knows, publish and pull queue a
// background job, and the engine does the work.
//
// They are mounted twice, from one definition:
//
//   - under /api/v1/youtrack/kb/…, which is the documented spelling and
//     addresses a project with the ?key= convention the rest of the /youtrack
//     subtree uses;
//   - inside mountKB, which gives the per-project (/projects/{key}/kb/youtrack/…)
//     and per-team (/teams/{key}/kb/youtrack/…) forms for free, because the
//     knowledge base is scoped that way everywhere else.
//
// Both reach the same handlers, so the two spellings cannot drift.

// mountYouTrackKB registers the three routes under whichever parent mounts it.
func (s *Server) mountYouTrackKB(r chi.Router) {
	r.Get("/status", s.handleYouTrackKBStatus)
	r.Post("/publish", s.handleYouTrackKBPublish)
	r.Post("/pull", s.handleYouTrackKBPull)
}

// youtrackKBRequest is the body of the two queueing routes. Status takes the
// same three fields as query parameters instead.
type youtrackKBRequest struct {
	// Path is the vault-relative page or folder. Empty means the project's
	// whole documentation folder.
	Path string `json:"path,omitempty"`
	// Recursive includes every page below a folder.
	Recursive bool `json:"recursive,omitempty"`
}

// handleYouTrackKBStatus serves GET …/youtrack/kb/status?path=&recursive=&remote=.
//
// `remote` stays opt-in: it is one article read per page, and defaulting it on
// would turn a tree view of a documentation folder into hundreds of requests
// against somebody else's rate limit.
func (s *Server) handleYouTrackKBStatus(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackKBScope(w, r)
	if !ok {
		return
	}
	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes "+key+".")
		return
	}
	result, ok := s.call(w, r, m, "youtrack.kb.status", map[string]any{
		"project":   key,
		"path":      r.URL.Query().Get("path"),
		"recursive": queryBool(r, "recursive"),
		"remote":    queryBool(r, "remote"),
	})
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleYouTrackKBPublish serves POST …/youtrack/kb/publish.
func (s *Server) handleYouTrackKBPublish(w http.ResponseWriter, r *http.Request) {
	s.queueYouTrackKBJob(w, r, "youtrack.kb.publish")
}

// handleYouTrackKBPull serves POST …/youtrack/kb/pull.
func (s *Server) handleYouTrackKBPull(w http.ResponseWriter, r *http.Request) {
	s.queueYouTrackKBJob(w, r, "youtrack.kb.pull")
}

// queueYouTrackKBJob is the body of both queueing routes. The vault answers
// `unavailable` when no engine or no client is installed and `not_found` when
// the path selects no page, so there is nothing left for the handler to decide.
func (s *Server) queueYouTrackKBJob(w http.ResponseWriter, r *http.Request, method string) {
	key, ok := s.youtrackKBScope(w, r)
	if !ok {
		return
	}
	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes "+key+".")
		return
	}
	var body youtrackKBRequest
	if r.ContentLength != 0 && !decodeBody(w, r, &body) {
		return
	}
	result, ok := s.call(w, r, m, method, map[string]any{
		"project":   key,
		"path":      body.Path,
		"recursive": body.Recursive,
	})
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusAccepted, result)
}

// youtrackKBScope resolves the project a knowledge-base sync request is about
// and refuses a project that declares no YouTrack connection.
//
// The key comes from the path for the scoped mounts, from ?key= or ?project=
// for the flat one, and from the single served project when there is exactly
// one. A project with no `integrations.youtrack` block answers 404: the routes
// are gated on `features.youtrack`, which is false precisely when nothing is
// linked, and a 404 is what tells a client that this project has no such
// resource rather than that the build lacks the feature.
func (s *Server) youtrackKBScope(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	if key == "" {
		key = strings.TrimSpace(r.URL.Query().Get("key"))
	}
	if key == "" {
		key = strings.TrimSpace(r.URL.Query().Get("project"))
	}
	if key == "" {
		resolved, ok := s.youtrackProjectKey(w, r)
		if !ok {
			return "", false
		}
		key = resolved
	}
	found, err := s.youtrack.linkFor(key)
	if err != nil {
		s.failYouTrack(w, r, err)
		return "", false
	}
	if found.link == nil {
		failProblem(w, r, codeNotFound,
			"Project "+key+" is not connected to YouTrack. Run `gintrack youtrack connect` first.")
		return "", false
	}
	return key, true
}

// queryBool reads an optional boolean query parameter. A bare parameter with no
// value — `?recursive` — reads as true, which is what a hand-written URL means.
func queryBool(r *http.Request, name string) bool {
	if !r.URL.Query().Has(name) {
		return false
	}
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return true
	}
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}
