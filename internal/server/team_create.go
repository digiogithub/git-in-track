package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
)

// createTeamBody is the request of POST /api/v1/repos/{id}/team
// (docs/07 section 5.4).
type createTeamBody struct {
	// Root is the repository-relative folder the team repository is created
	// at. An empty value and "." both mean the repository root, which is where
	// R-TEAM-LOC-1 puts team.yaml.
	Root string `json:"root,omitempty"`
	// Key is the prefix of every sprint and retro id, matching
	// [A-Z][A-Z0-9-]{1,15}.
	Key string `json:"key"`
	// Name is the display name; it defaults to the key.
	Name string `json:"name,omitempty"`
	// Description is one optional paragraph.
	Description string `json:"description,omitempty"`
	// Timezone is an IANA name; it defaults to UTC.
	Timezone string `json:"timezone,omitempty"`
	// KnowledgePath is the team knowledge-base folder; it defaults to
	// "knowledge".
	KnowledgePath string `json:"knowledgePath,omitempty"`
	// Members are the people the team starts with. It may be empty: a team
	// repository is created before anybody is listed in it (ADR-020).
	Members []core.Member `json:"members,omitempty"`
}

// registerRepoBody is the request of POST /api/v1/repos. It is decoded only to
// build the command the user has to run: the route never registers anything.
type registerRepoBody struct {
	Path string `json:"path"`
	Role string `json:"role,omitempty"`
	Docs string `json:"docs,omitempty"`
}

// handleCreateTeam serves POST /api/v1/repos/{id}/team: it scaffolds a team
// repository — team.yaml, the `.pmngr/` artifact folders and the knowledge base
// — inside a registered repository that is not one yet.
//
// It is the companion half of the "create a team repository" flow the browser
// runs through "team.create": both go through the same vault method, so the
// team.yaml a companion writes and the one a browser writes are the same bytes.
func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, ok := s.repos.lookup(id)
	if !ok {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+id+".")
		return
	}
	var body createTeamBody
	if !decodeBody(w, r, &body) {
		return
	}
	result, ok := s.call(w, r, m, "team.create", map[string]any{
		"root":          body.Root,
		"key":           body.Key,
		"name":          body.Name,
		"description":   body.Description,
		"timezone":      body.Timezone,
		"knowledgePath": body.KnowledgePath,
		"members":       body.Members,
	})
	if !ok {
		return
	}
	m.touch(s.now())
	s.publishIndexUpdated(m, indexCounts{Full: true}, requestIDOf(r))
	writeJSON(w, r, http.StatusCreated, result)
}

// handleRegisterRepo serves POST /api/v1/repos, and deliberately refuses.
//
// Registering a repository writes the user's configuration file, and that file
// belongs to the CLI: the companion reads it at startup, so a server-side write
// would make the running process disagree with the file on disk, and it would
// let a browser tab add arbitrary paths of the user's machine to the workspace.
// The honest answer is 501 with the exact command to run, never a fake success
// (ADR-020).
func (s *Server) handleRegisterRepo(w http.ResponseWriter, r *http.Request) {
	var body registerRepoBody
	if !decodeBody(w, r, &body) {
		return
	}
	failProblem(w, r, codeNotImplemented,
		"Registering a repository is a change to your gintrack configuration, which only the CLI writes. "+
			"Run this command, then reload: "+gintrackAddCommand(body))
}

// gintrackAddCommand renders the `gintrack add` invocation that registers what
// the request asked for. A path with a space is quoted so the line can be
// pasted into a shell as it is.
func gintrackAddCommand(body registerRepoBody) string {
	parts := []string{"gintrack", "add", shellArg(strings.TrimSpace(body.Path))}
	if strings.EqualFold(strings.TrimSpace(body.Role), "team") {
		parts = append(parts, "--team")
	}
	if docs := strings.TrimSpace(body.Docs); docs != "" {
		parts = append(parts, "--docs", shellArg(docs))
	}
	return strings.Join(parts, " ")
}

// shellArg quotes an argument that would otherwise split in a shell.
func shellArg(arg string) string {
	if arg == "" {
		return `"<path>"`
	}
	if strings.ContainsAny(arg, " \t\"'$`\\") {
		return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
	}
	return arg
}
