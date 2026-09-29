package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Connect-only access to managed Pando instances (GIT-US-0176, ADR-039).
//
// `gintrack mcp` and `gintrack spec` never start Pando. In managed mode they
// read the state file and token of the instance a running `gintrack serve`
// supervises, and answer `unavailable` when there is none.

// MsgManagedNotRunning is the reason given when no supervised instance is
// running for a repository.
const MsgManagedNotRunning = "managed Pando is not running — start `gintrack serve`"

// discoveredInstance is a [ManagedInstance] that only reads an instance
// directory. Start, Stop and Restart do nothing: the process belongs to the
// `gintrack serve` that holds its lock.
type discoveredInstance struct{ dir string }

func (discoveredInstance) Start(context.Context) error { return nil }
func (discoveredInstance) Stop(context.Context) error  { return nil }
func (discoveredInstance) Restart()                    {}

func (d discoveredInstance) Status() supervisor.Status {
	st, live := supervisor.Inspect(d.dir)
	if live != supervisor.LiveRunning || st.State == supervisor.StateStopped {
		st.State = supervisor.StateStopped
		st.LastError = MsgManagedNotRunning
	} else if _, _, ok := d.Endpoint(); st.State == supervisor.StateReady && !ok {
		// Ready on paper, but nothing answers or its token is unreadable.
		st.State = supervisor.StateStopped
		st.LastError = MsgManagedNotRunning
	}
	return st
}

func (d discoveredInstance) Endpoint() (mcpURL, token string, ok bool) {
	st, live := supervisor.Inspect(d.dir)
	if live != supervisor.LiveRunning || st.State != supervisor.StateReady ||
		st.MCPURL == "" || !supervisor.Healthy(st) {
		return "", "", false
	}
	token, err := supervisor.ReadToken(d.dir)
	if err != nil || token == "" {
		return "", "", false
	}
	return st.MCPURL, token, true
}

// InstallDiscoveredSemanticSearch installs, on space, a semantic searcher that
// fans out to the instances a running `gintrack serve` supervises for the
// repositories that opted in, and returns the host that hands the impact tiers
// each repository's own instance. Nothing is started. With no opted-in
// repository the host is empty and semantic search stays unavailable.
func InstallDiscoveredSemanticSearch(cacheDir string, space *vault.Workspace, repos []SemanticRepo, log *slog.Logger) *SemanticHost {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	reg := semanticRegistry(space, repos)
	ms := newManagedState(Options{CacheDir: cacheDir}, config.PandoResolution{Mode: config.PandoModeManaged}, reg, nil, log)
	for _, m := range reg.mounts {
		if !m.semantic.Load() || m.role != roleProject {
			continue
		}
		dir := supervisor.InstanceDir(cacheDir, supervisor.InstanceKey(m.path))
		ms.byRepo[m.id] = &managedRepo{repo: m.id, root: m.path, project: codeProjectID(m), sup: discoveredInstance{dir: dir}}
		ms.order = append(ms.order, m.id)
	}
	if len(ms.order) == 0 {
		space.SetSemanticSearcher(nil)
		return &SemanticHost{}
	}
	space.SetSemanticSearcher(&managedSearcher{ms: ms})
	return &SemanticHost{discovered: ms}
}

// ---------------------------------------------------------------- routes ---

// managedControlResult is what a lifecycle route answers.
type managedControlResult struct {
	Repo    string              `json:"repo"`
	Action  string              `json:"action"`
	Managed managedInstanceView `json:"managed"`
}

var (
	errManagedForeign    = errors.New("this instance is supervised by another gintrack serve: run the command there")
	errManagedNotRunning = errors.New("no managed instance is running")
)

// handleManagedControl serves POST /api/v1/search/managed/{repo}/start|stop|
// restart|reset, the lifecycle control `gintrack pando` drives. It changes the
// processes of this serve only: whether a repository opts in stays the
// business of `repos[].semanticSearch`.
func (s *Server) handleManagedControl(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := strings.TrimSpace(chi.URLParam(r, "repo"))
		ms := s.search.managed
		if ms == nil {
			failProblem(w, r, codeSearchNotConfigured,
				"Pando is not in managed mode ("+s.search.mode.String()+"): there is no instance to "+action+".")
			return
		}
		m, ok := ms.repos.lookup(repo)
		if !ok || !m.ready() {
			failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+repo+".")
			return
		}
		view, err := ms.control(context.WithoutCancel(r.Context()), m, action)
		switch {
		case errors.Is(err, errManagedForeign), errors.Is(err, errManagedNotRunning):
			failProblem(w, r, codeManagedInstance, fmt.Sprintf("%s: %v.", repo, err))
			return
		case err != nil:
			failProblem(w, r, codeInternal, err.Error())
			return
		}
		writeJSON(w, r, http.StatusOK, managedControlResult{Repo: repo, Action: action, Managed: view})
	}
}

// control applies one lifecycle verb to the instance of a repository.
func (ms *managedState) control(ctx context.Context, m *mount, action string) (managedInstanceView, error) {
	slot := ms.slot(m.id)
	if slot != nil && action != "start" {
		slot.mu.Lock()
		_, foreign := slot.sup.(foreignInstance)
		slot.mu.Unlock()
		if foreign {
			return managedInstanceView{}, errManagedForeign
		}
	}
	switch action {
	case "start":
		return ms.Enable(m.id), nil
	case "stop":
		if err := ms.Disable(ctx, m.id); err != nil {
			return managedInstanceView{}, err
		}
		return ms.viewFor(m), nil
	case "restart":
		if slot == nil || ms.restart(m.id) == 0 {
			return managedInstanceView{}, errManagedNotRunning
		}
		return ms.viewOf(slot, m.semantic.Load()), nil
	case "reset":
		if err := ms.Disable(ctx, m.id); err != nil {
			return managedInstanceView{}, err
		}
		dir := supervisor.InstanceDir(ms.cacheDir, supervisor.InstanceKey(m.path))
		if err := os.RemoveAll(supervisor.DataDir(dir)); err != nil {
			return managedInstanceView{}, fmt.Errorf("delete the index of %s: %w", m.id, err)
		}
		return ms.Enable(m.id), nil
	}
	return managedInstanceView{}, fmt.Errorf("unknown action %q", action)
}
