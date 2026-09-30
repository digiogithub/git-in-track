package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

// aguiInstance is the fake managed AG-UI adapter: a fakeInstance whose
// endpoint is the fakeAGUI stand-in and whose token is the one that stand-in
// enforces.
type aguiInstance struct{ *fakeInstance }

func (a aguiInstance) Endpoint() (string, string, bool) {
	url, _, ok := a.fakeInstance.Endpoint()
	return url, upstreamToken, ok
}

// aguiRig is a companion in managed mode with the agent proxy on.
type aguiRig struct {
	s        *Server
	upstream *fakeAGUI
	agui     map[string]*fakeInstance
	mcp      map[string]*fakeInstance
	opts     map[string]supervisor.Options
	mu       sync.Mutex
	created  []supervisor.Kind
}

type aguiRigOptions struct {
	repos, optIn []string
	mcpHTTP      bool
	mutate       func(*config.Config)
}

func newAGUIRig(t *testing.T, o aguiRigOptions) *aguiRig {
	t.Helper()
	rig := &aguiRig{
		upstream: newFakeAGUI(t), agui: map[string]*fakeInstance{}, mcp: map[string]*fakeInstance{},
		opts: map[string]supervisor.Options{},
	}
	roots := map[string]string{}
	var repos []Repo
	for _, id := range o.repos {
		root := copyTree(t, fixtureRoot)
		roots[root] = id
		repos = append(repos, Repo{ID: id, Path: root, Role: "project", DocsFolder: "docs", SemanticSearch: contains(o.optIn, id)})
		rig.agui[id] = &fakeInstance{url: rig.upstream.server.URL}
		rig.mcp[id] = &fakeInstance{url: "fake://" + id}
	}
	cfg := config.Default()
	cfg.Agent.Enabled = true
	// The default upstream is dead on purpose: a request that reaches it fell
	// back when it should not have.
	cfg.Agent.Pando.URL = "http://127.0.0.1:1"
	if o.mutate != nil {
		o.mutate(cfg)
	}
	s, err := New(Options{
		Token: "test-token", Workspace: "test", Repos: repos, CacheDir: t.TempDir(),
		Agent: true, MCPHTTP: o.mcpHTTP, Pando: cfg.PandoTargets(),
		Now:           func() time.Time { return time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC) },
		Search:        config.Search{Pando: config.SearchPando{Mode: config.PandoModeAuto}},
		pandoLookPath: func(string) (string, error) { return "/fake/pando", nil },
		pandoNewInstance: func(so supervisor.Options) (ManagedInstance, error) {
			rig.mu.Lock()
			defer rig.mu.Unlock()
			id, ok := roots[so.RepoRoot]
			if !ok {
				return nil, errors.New("unknown repository " + so.RepoRoot)
			}
			rig.created = append(rig.created, so.Kind)
			if so.Kind == supervisor.KindAGUI {
				rig.opts[id] = so
				return aguiInstance{rig.agui[id]}, nil
			}
			return rig.mcp[id], nil
		},
		pandoNewClient: func(string, string, string) (pandoAPI, error) {
			return &graphPando{fakePando: &fakePando{}}, nil
		},
		pandoTick: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	rig.s = s
	return rig
}

func (r *aguiRig) begin(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.s.search.startManaged(ctx)
	t.Cleanup(func() {
		cancel()
		r.s.search.stopManaged(context.Background())
	})
}

func (r *aguiRig) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	r.s.Handler().ServeHTTP(rec, req)
	return rec
}

func (r *aguiRig) kinds() []supervisor.Kind {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]supervisor.Kind(nil), r.created...)
}

// Verifies: GIT-US-0185 (managed AG-UI adapter)
func TestManagedAGUI(t *testing.T) {
	t.Run("the panel reaches the managed adapter with no agent.pando configuration", func(t *testing.T) {
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a"}, optIn: []string{"a"}, mcpHTTP: true})
		r.begin(t)
		eventually(t, "the adapter to be created", func() bool { return len(r.opts) == 1 })
		r.agui["a"].set(supervisor.StateReady, 41, "")

		rec := r.get(t, agentPath+"/info?repo=a")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		if got := r.upstream.last().Auth; got != "Bearer "+upstreamToken {
			t.Errorf("the adapter's token was not injected: %q", got)
		}
		if strings.Contains(rec.Body.String(), r.upstream.server.URL) {
			t.Errorf("the response names the adapter origin: %s", rec.Body)
		}
	})

	t.Run("an adapter that is not ready answers 503 and never falls back", func(t *testing.T) {
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a"}, optIn: []string{"a"}, mcpHTTP: true})
		r.begin(t)
		eventually(t, "the adapter to be created", func() bool { return len(r.opts) == 1 })
		r.agui["a"].set(supervisor.StateRestarting, 0, "pando exited: boom")

		rec := r.get(t, agentPath+"/info?repo=a")
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("status = %d retry = %q, want 503 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
		}
		if !strings.Contains(rec.Body.String(), "restarting") || !strings.Contains(rec.Body.String(), "boom") {
			t.Errorf("body = %s, want the state and the reason", rec.Body)
		}
	})

	t.Run("a repository that did not opt in uses agent.pando", func(t *testing.T) {
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a", "b"}, optIn: []string{"a"}, mcpHTTP: true})
		r.begin(t)
		eventually(t, "the adapter of a", func() bool { return len(r.opts) == 1 })
		if _, ok := r.opts["b"]; ok {
			t.Fatal("an adapter was created for a repository that did not opt in")
		}
		// The default upstream is dead, so the answer proves the fallback was tried.
		if rec := r.get(t, agentPath+"/info?repo=b"); rec.Code != http.StatusBadGateway && rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want an upstream failure from the dead default", rec.Code)
		}
	})

	t.Run("an explicit agent.pando.repos row keeps the repository external", func(t *testing.T) {
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a"}, optIn: []string{"a"}, mcpHTTP: true,
			mutate: func(c *config.Config) {
				c.Agent.Pando.Repos = []config.PandoRepo{{Repo: "a", URL: "http://127.0.0.1:1"}}
			}})
		r.begin(t)
		eventually(t, "the MCP instance", func() bool { return len(r.kinds()) >= 1 })
		time.Sleep(50 * time.Millisecond)
		for _, k := range r.kinds() {
			if k == supervisor.KindAGUI {
				t.Fatal("an AG-UI adapter was started for a repository with an explicit row")
			}
		}
	})

	t.Run("agent.pando.managed false runs no adapter", func(t *testing.T) {
		off := false
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a"}, optIn: []string{"a"}, mcpHTTP: true,
			mutate: func(c *config.Config) { c.Agent.Pando.Managed = &off }})
		r.begin(t)
		eventually(t, "the MCP instance", func() bool { return len(r.kinds()) >= 1 })
		time.Sleep(50 * time.Millisecond)
		for _, k := range r.kinds() {
			if k == supervisor.KindAGUI {
				t.Fatal("an AG-UI adapter was started with agent.pando.managed: false")
			}
		}
	})

	t.Run("without the MCP endpoint the adapter is skipped and says why", func(t *testing.T) {
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a"}, optIn: []string{"a"}})
		r.begin(t)
		eventually(t, "the MCP instance", func() bool { return len(r.kinds()) >= 1 })
		for _, k := range r.kinds() {
			if k == supervisor.KindAGUI {
				t.Fatal("an AG-UI adapter was started without the MCP endpoint it calls back")
			}
		}
		m := r.s.search.managed.viewFor(mustMount(t, r.s, "a"))
		if m.AGUI == nil || m.AGUI.State != managedStateSkipped || !strings.Contains(m.AGUI.Error, "--mcp-http") {
			t.Errorf("view = %+v, want a skipped adapter that names --mcp-http", m.AGUI)
		}
	})

	t.Run("the rendered configuration is the agent init one with the managed values", func(t *testing.T) {
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a"}, optIn: []string{"a"}, mcpHTTP: true})
		r.begin(t)
		eventually(t, "the adapter to be created", func() bool { return len(r.opts) == 1 })
		so := r.opts["a"]
		if so.Kind != supervisor.KindAGUI || so.Files == nil || so.Binary != "/fake/pando" {
			t.Fatalf("options = %+v", so)
		}
		files, err := so.Files(4242)
		if err != nil {
			t.Fatal(err)
		}
		cfg := string(files[".pando.toml"])
		for _, want := range []string{
			"Port = 4242", "Host = '127.0.0.1'", "'gintrack_*'", "RequireToken = true", "HumanInTheLoop = true",
			"Authorization = 'Bearer test-token'", "/mcp'", "[ToolDiscovery]", "Enabled = false",
		} {
			if !strings.Contains(cfg, want) {
				t.Errorf(".pando.toml misses %q", want)
			}
		}
		for _, banned := range []string{"code_hybrid_search", "code_find_symbol", "'kb_add_document'", "'remember'"} {
			if strings.Contains(cfg, banned) {
				t.Errorf(".pando.toml allows %s", banned)
			}
		}
		for _, name := range []string{"agents/personas/backlog-assistant.md", "agents/skills/gintrack-search/SKILL.md"} {
			if len(files[name]) == 0 {
				t.Errorf("no %s rendered", name)
			}
		}
	})

	t.Run("stopping ends the adapter and the MCP instance", func(t *testing.T) {
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a"}, optIn: []string{"a"}, mcpHTTP: true})
		r.begin(t)
		eventually(t, "both to start", func() bool {
			s1, _, _ := r.agui["a"].counts()
			s2, _, _ := r.mcp["a"].counts()
			return s1 == 1 && s2 == 1
		})
		r.s.search.stopManaged(context.Background())
		if _, stops, _ := r.agui["a"].counts(); stops != 1 {
			t.Errorf("adapter stops = %d, want 1", stops)
		}
		if _, stops, _ := r.mcp["a"].counts(); stops != 1 {
			t.Errorf("MCP stops = %d, want 1", stops)
		}
	})

	t.Run("a restart bounces the adapter too", func(t *testing.T) {
		r := newAGUIRig(t, aguiRigOptions{repos: []string{"a"}, optIn: []string{"a"}, mcpHTTP: true})
		r.begin(t)
		eventually(t, "the adapter to be created", func() bool { return len(r.opts) == 1 })
		if n := r.s.search.managed.restart("a"); n != 1 {
			t.Fatalf("restart = %d", n)
		}
		if _, _, restarts := r.agui["a"].counts(); restarts != 1 {
			t.Errorf("adapter restarts = %d, want 1", restarts)
		}
	})
}

func mustMount(t *testing.T, s *Server, id string) *mount {
	t.Helper()
	m, ok := s.repos.lookup(id)
	if !ok {
		t.Fatalf("no repository %s", id)
	}
	return m
}
