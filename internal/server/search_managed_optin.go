package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

// codeSearchNotManaged means the request is about the managed Pando, and this
// companion is not in managed mode (ADR-039).
const codeSearchNotManaged = "search_not_managed"

// managedOptInRequest is the body of PUT /api/v1/search/managed/{repo}/opt-in.
type managedOptInRequest struct {
	// Enabled is the new value of `repos[].semanticSearch`.
	Enabled bool `json:"enabled"`
	// DeleteIndex, with Enabled false, also removes the instance directory —
	// Pando's index and generated files — once the instance has stopped.
	DeleteIndex bool `json:"deleteIndex,omitempty"`
}

// managedOptInResponse is what the route answers.
type managedOptInResponse struct {
	Repo string `json:"repo"`
	// OptedIn is the value in force now.
	OptedIn bool `json:"optedIn"`
	// Persisted says whether the choice reached the configuration file. False
	// means this process has no configuration path, or the file does not list
	// the repository: the choice lasts until the process exits.
	Persisted bool `json:"persisted"`
	// IndexDeleted says whether the instance directory was removed.
	IndexDeleted bool                `json:"indexDeleted"`
	Managed      managedInstanceView `json:"managed"`
}

// handleSearchManagedOptIn serves PUT /api/v1/search/managed/{repo}/opt-in. It
// persists the machine-local opt-in first and only then starts or stops the
// instance, so a refused write leaves the running state as it was.
func (s *Server) handleSearchManagedOptIn(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(chi.URLParam(r, "repo"))
	var req managedOptInRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ms := s.search.managed
	if ms == nil {
		failProblem(w, r, codeSearchNotManaged,
			"Semantic search is not managed by this companion: set search.pando.mode to managed (or install pando) to opt a repository in.")
		return
	}
	m, ok := s.repos.lookup(repo)
	if !ok || !m.ready() {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+repo+".")
		return
	}
	if req.DeleteIndex && req.Enabled {
		failProblem(w, r, codeInvalidRequest, "deleteIndex only applies when enabled is false.")
		return
	}

	persisted := false
	if path := s.search.configPath; path != "" {
		err := config.SetSemanticSearch(path, m.id, req.Enabled)
		switch {
		case err == nil:
			persisted = true
		case errors.Is(err, config.ErrRepoNotInFile):
			// A `serve --repo` mount: the choice lives in memory only.
		default:
			s.log.Warn("could not persist the semantic search opt-in", "repo", m.id, "error", err)
			failProblem(w, r, codeInternal, "The opt-in could not be saved: "+err.Error())
			return
		}
	}
	m.semantic.Store(req.Enabled)

	out := managedOptInResponse{Repo: m.id, OptedIn: req.Enabled, Persisted: persisted}
	if req.Enabled {
		out.Managed = ms.Enable(m.id)
		writeJSON(w, r, http.StatusOK, out)
		return
	}
	slot := ms.slot(m.id)
	foreign := false
	if slot != nil {
		slot.mu.Lock()
		_, foreign = slot.sup.(foreignInstance)
		slot.mu.Unlock()
	}
	if err := ms.Disable(context.WithoutCancel(r.Context()), m.id); err != nil {
		failProblem(w, r, codeInternal, err.Error())
		return
	}
	if req.DeleteIndex && !foreign {
		dir := supervisor.InstanceDir(ms.cacheDir, supervisor.InstanceKey(m.path))
		if err := os.RemoveAll(dir); err != nil {
			failProblem(w, r, codeInternal, "The instance stopped but its index could not be removed: "+err.Error())
			return
		}
		out.IndexDeleted = true
	}
	out.Managed = ms.viewFor(m)
	writeJSON(w, r, http.StatusOK, out)
}
