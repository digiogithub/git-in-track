package server

import (
	"context"
	"errors"
	"net"
	"path/filepath"

	"github.com/digiogithub/git-in-track/internal/agentcfg"
	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

// Managed AG-UI adapters (GIT-US-0185, ADR-039 decision 4, ADR-035).
//
// In managed mode `gintrack serve` runs `pando agui-serve` for the agent panel
// the same way it runs `pando mcp-server` for search: one process per opted-in
// repository, supervised by internal/pando/supervisor (lifecycle, health,
// backoff, orphan reaping, state file), living in its own directory
// `<cache>/pando/<key>-agui/`. The configuration it runs with is generated
// there from the same templates `gintrack agent init` writes into a repository
// (internal/agentcfg), so nothing is written inside the repository and the tool
// allow-list is the same one. The proxy at /api/v1/agent resolves the upstream
// of a repository through aguiEndpoint before it looks at `agent.pando`, so the
// panel finds it with no configuration at all: the browser never knew the
// upstream in the first place, only `features.agent`.

// managedAGUI is the settings the managed AG-UI adapters are built from.
type managedAGUI struct {
	// targets is the `agent.pando` snapshot: a repository with an explicit row
	// is external and is never managed.
	targets config.PandoTargets
	// companionURL is the base URL Pando's gintrack MCP client reaches this
	// companion at; it is only known once the listener is bound.
	companionURL func() string
	// token is the companion's bearer token, "" when authentication is off. It
	// is written into the generated configuration, which is mode 0600 in a
	// 0700 directory.
	token string
	// refusal, when set, is why no adapter can run at all (the MCP endpoint
	// the adapter calls back is off); every opted-in repository reports it.
	refusal string
}

// aguiAdapter is the state of one managed AG-UI adapter as the proxy sees it.
type aguiAdapter struct {
	// URL and Token are the listener and its bearer token; set only when ready.
	URL, Token string
	// NotReady says why an adapter that exists cannot answer.
	NotReady string
}

// prepareAGUILocked builds the AG-UI supervisor of a slot that has an MCP
// instance. ms.mu is held. It leaves slot.agui nil, with slot.aguiSkipped set
// when the reason is worth showing, when no adapter should run.
func (ms *managedState) prepareAGUILocked(slot *managedRepo, m *mount) {
	cfg := ms.agui
	if cfg == nil || !cfg.targets.ManagedAGUI() || cfg.targets.Known(m.id) {
		return
	}
	if cfg.refusal != "" {
		slot.aguiSkipped = cfg.refusal
		return
	}
	if cfg.companionURL == nil {
		slot.aguiSkipped = "the companion MCP endpoint is not known"
		return
	}
	docs := managedDocs(m)
	if docs == "" {
		docs = "docs"
	}
	sup, err := ms.newInstance(supervisor.Options{
		Kind: supervisor.KindAGUI, Binary: ms.res.Binary, CacheDir: ms.cacheDir, RepoRoot: m.path,
		DocsFolder: docs, MinVersion: ms.cfg.MinVersion, Debug: ms.cfg.LogLevelOrDefault() == "debug",
		Logger: ms.log.With("pando-agui", m.id), AGUIPath: agentcfg.DefaultAGUIPath,
		Files: func(port int) (map[string][]byte, error) {
			return renderManagedAGUI(cfg, m.id, m.path, docs, port)
		},
	})
	if err != nil {
		slot.aguiSkipped = "the AG-UI adapter could not be prepared: " + err.Error()
		return
	}
	slot.agui = sup
}

// renderManagedAGUI renders the files of one adapter: the same three files
// `gintrack agent init` writes, with the AG-UI port the supervisor picked and
// the companion token in the clear (the directory is private and holds the
// adapter's own token the same way; there is no repository file to commit by
// mistake). The code-search tools are dropped from the allow-list: the adapter
// indexes the documentation folder only, so they would always answer nothing.
func renderManagedAGUI(cfg *managedAGUI, repoID, root, docs string, port int) (map[string][]byte, error) {
	data := agentcfg.TemplateData{
		RepoID: repoID, RepoPath: root, AGUIPath: agentcfg.DefaultAGUIPath, AGUIHost: "127.0.0.1",
		AGUIPort: port, MaxConcurrentRuns: agentcfg.DefaultMaxRuns, Persona: agentcfg.PersonaID,
		Tools: agentcfg.WithoutCodeTools(agentcfg.Tools), MCPURL: cfg.companionURL() + "/mcp",
		KBPath: filepath.Join(root, docs),
	}
	if cfg.token != "" {
		data.Token, data.BearerHeader = true, "Bearer "+cfg.token
	}
	files := map[string][]byte{}
	for name, tmpl := range map[string]string{
		agentcfg.PandoConfigName: agentcfg.TemplatePando,
		agentcfg.PersonaName:     agentcfg.TemplatePersona,
		agentcfg.SkillName:       agentcfg.TemplateSkill,
	} {
		out, err := agentcfg.Render(tmpl, data)
		if err != nil {
			return nil, err
		}
		files[name] = out
	}
	return files, nil
}

// runAGUI starts the adapter of a slot. The supervisor keeps it healthy in the
// background afterwards, so there is nothing to watch: a start that finds the
// directory owned by another gintrack process connects to that one's instead.
func (ms *managedState) runAGUI(ctx context.Context, slot *managedRepo) {
	defer ms.wg.Done()
	err := slot.agui.Start(ctx)
	switch {
	case err == nil:
	case errors.Is(err, supervisor.ErrLocked):
		dir := supervisor.InstanceDir(ms.cacheDir, supervisor.AGUIKey(slot.root))
		slot.mu.Lock()
		slot.agui = foreignInstance{dir: dir, agui: true}
		slot.mu.Unlock()
		ms.log.Info("managed AG-UI adapter is supervised by another gintrack; connecting to it", "repo", slot.repo)
	default:
		slot.mu.Lock()
		slot.aguiStartErr = err.Error()
		slot.mu.Unlock()
		ms.log.Warn("managed AG-UI adapter did not start", "repo", slot.repo, "error", err)
	}
}

// aguiStatus is the status of a slot's adapter, with a Start failure folded in.
func (r *managedRepo) aguiStatus() supervisor.Status {
	r.mu.Lock()
	sup, startErr := r.agui, r.aguiStartErr
	r.mu.Unlock()
	if sup == nil {
		return supervisor.Status{State: supervisor.StateStopped}
	}
	st := sup.Status()
	if st.LastError == "" {
		st.LastError = startErr
	}
	if startErr != "" && st.State == supervisor.StateStopped {
		st.State = supervisor.StateFailed
	}
	return st
}

// aguiViewOf renders the adapter of a slot, nil when the repository has none.
func (ms *managedState) aguiViewOf(slot *managedRepo) *managedAGUIView {
	slot.mu.Lock()
	agui, skipped := slot.agui, slot.aguiSkipped
	slot.mu.Unlock()
	if agui == nil {
		if skipped == "" {
			return nil
		}
		return &managedAGUIView{State: managedStateSkipped, Error: skipped}
	}
	st := slot.aguiStatus()
	v := &managedAGUIView{State: string(st.State), PID: st.PID, Port: st.Port, Crashes: st.Crashes, Error: st.LastError}
	if !st.Since.IsZero() {
		since := st.Since
		v.Since = &since
	}
	return v
}

// aguiEndpoint is what the agent proxy asks for a repository. found is false
// when the repository has no managed adapter, which is the proxy's cue to use
// `agent.pando`; otherwise the adapter's endpoint, or the reason it has none
// yet — an adapter that is starting must never fall back to another
// repository's upstream.
func (ms *managedState) aguiEndpoint(repo string) (a aguiAdapter, found bool) {
	if ms == nil {
		return aguiAdapter{}, false
	}
	slot := ms.slot(repo)
	if slot == nil {
		return aguiAdapter{}, false
	}
	slot.mu.Lock()
	agui := slot.agui
	slot.mu.Unlock()
	if agui == nil {
		return aguiAdapter{}, false
	}
	if url, token, ok := agui.Endpoint(); ok {
		return aguiAdapter{URL: url, Token: token}, true
	}
	st := slot.aguiStatus()
	msg := "the managed agent adapter is " + string(st.State)
	if st.State == "" {
		msg = "the managed agent adapter is not running"
	}
	if st.LastError != "" {
		msg += ": " + st.LastError
	}
	return aguiAdapter{NotReady: msg}, true
}

// hasAGUI reports whether any repository has a managed adapter, so that the
// agent proxy counts as available even when `agent.pando` names nothing usable.
func (ms *managedState) hasAGUI() bool {
	if ms == nil {
		return false
	}
	for _, slot := range ms.slots() {
		slot.mu.Lock()
		has := slot.agui != nil
		slot.mu.Unlock()
		if has {
			return true
		}
	}
	return false
}

// newManagedAGUI builds the AG-UI settings of a server, nil when the agent proxy
// is off. The adapter calls the companion's MCP endpoint back, so without
// `--mcp-http` (or `mcp.enabled`) it would start and be useless: that is
// reported instead.
func (s *Server) newManagedAGUI(opts Options) *managedAGUI {
	if !opts.Agent {
		return nil
	}
	cfg := &managedAGUI{targets: opts.Pando, companionURL: s.companionURL, token: opts.Token}
	if !opts.MCPHTTP {
		cfg.refusal = "the agent adapter reads the backlog through this companion's /mcp endpoint: start it with --mcp-http (or mcp.enabled)"
	}
	return cfg
}

// companionURL is the base URL Pando reaches this companion at: the bound
// address, with loopback in place of a wildcard bind.
func (s *Server) companionURL() string {
	host, port, err := net.SplitHostPort(s.Addr())
	if err != nil {
		return s.URL()
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = DefaultBind
	}
	return "http://" + net.JoinHostPort(host, port)
}
