package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/impact"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Managed Pando instances (GIT-US-0175, ADR-039).
//
// In managed mode `gintrack serve` supervises one Pando per repository that
// opted in to semantic search (`repos[].semanticSearch`). This file is the
// seam between the supervisor (internal/pando/supervisor) and the search
// surface: it starts the instances, registers each repository as a code project
// of its own instance, fans a semantic search out to every ready one, hands the
// impact tiers the instance of the repository being analyzed, and reports the
// state of each. External and off modes never construct any of it.

// managedRegisterRetry is how long a failed code-project registration waits
// before it is tried again, so a Pando that refuses the project is not asked
// once per poll.
const managedRegisterRetry = 15 * time.Second

// managedTick is how often a watcher looks at its instance when nothing else
// sets the rhythm.
const managedTick = time.Second

// ManagedInstance is the slice of *supervisor.Supervisor the server uses. It is
// an interface so that a test drives every state without spawning a process.
type ManagedInstance interface {
	// Start takes the instance and begins supervising it in the background.
	Start(ctx context.Context) error
	// Stop ends the child and releases the instance.
	Stop(ctx context.Context) error
	// Restart bounces the child without counting a crash.
	Restart()
	// Status is a snapshot of the instance.
	Status() supervisor.Status
	// Endpoint is the MCP URL and bearer token; ok is false until ready.
	Endpoint() (mcpURL, token string, ok bool)
}

var _ ManagedInstance = (*supervisor.Supervisor)(nil)

// The states a managed row reports besides the supervisor's own.
const (
	// managedStateDisabled means the repository has not opted in.
	managedStateDisabled = "disabled"
	// managedStateSkipped means it opted in but no instance was started: the
	// cap was reached, or there is no binary. The error says which.
	managedStateSkipped = "skipped"
)

// managedInstanceView is what the settings response says about one repository
// in managed mode. It never carries the token, nor the endpoint.
type managedInstanceView struct {
	// OptedIn is `repos[].semanticSearch`.
	OptedIn bool `json:"optedIn"`
	// State is stopped, starting, ready, restarting or failed (the
	// supervisor's), or disabled or skipped.
	State   string `json:"state"`
	PID     int    `json:"pid,omitempty"`
	Port    int    `json:"port,omitempty"`
	Version string `json:"version,omitempty"`
	// Since is when State last changed.
	Since *time.Time `json:"since,omitempty"`
	// Crashes counts the crashes inside the supervisor's window.
	Crashes int `json:"crashes,omitempty"`
	// Error is the last error, or why the instance was skipped.
	Error string `json:"error,omitempty"`
}

// managedRepo is one repository's slot: its instance, or the reason it has none.
type managedRepo struct {
	repo, root, project string

	mu sync.Mutex
	// sup is nil when the repository was skipped.
	sup ManagedInstance
	// skipped says why no instance was started.
	skipped string
	// startErr is a Start failure that left no status to read.
	startErr string
	// client is the session to the instance's current endpoint.
	client    pandoAPI
	clientKey string
	// registered is true once the code project was handed to this instance.
	registered  bool
	registerPID int
	// forceIndex asks for a fresh code index once the restarted child is
	// ready: a restart interrupts the indexing job the child was running.
	forceIndex bool
	nextTry    time.Time
	// stop ends the watcher of this slot.
	stop context.CancelFunc
}

// managedState owns the instances of one `gintrack serve`.
type managedState struct {
	log      *slog.Logger
	res      config.PandoResolution
	cfg      config.PandoManaged
	cacheDir string
	repos    *registry
	// search receives what became of each code registration.
	search *searchState

	newInstance func(supervisor.Options) (ManagedInstance, error)
	newClient   func(mcpURL, token, project string) (pandoAPI, error)
	tick        time.Duration

	mu     sync.Mutex
	byRepo map[string]*managedRepo
	order  []string
	runCtx context.Context //nolint:containedctx // the serve lifetime, held for Enable
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// newManagedState builds the state of a resolved managed mode. It starts
// nothing.
func newManagedState(opts Options, res config.PandoResolution, repos *registry, search *searchState, log *slog.Logger) *managedState {
	ms := &managedState{
		log: log, res: res, cfg: opts.Search.Pando.Managed, cacheDir: opts.CacheDir,
		repos: repos, search: search, byRepo: make(map[string]*managedRepo),
		newInstance: opts.pandoNewInstance, newClient: opts.pandoNewClient, tick: opts.pandoTick,
	}
	if ms.newInstance == nil {
		ms.newInstance = func(o supervisor.Options) (ManagedInstance, error) { return supervisor.New(o) }
	}
	if ms.newClient == nil {
		ms.newClient = func(mcpURL, token, project string) (pandoAPI, error) {
			return pando.New(pando.Options{MCPURL: mcpURL, Token: token, ProjectID: project})
		}
	}
	if ms.tick <= 0 {
		ms.tick = managedTick
	}
	return ms
}

// begin starts an instance for every repository that opted in, capped by
// search.pando.managed.maxInstances, and keeps ctx for later Enable calls. It
// never blocks: each Start runs in its own goroutine, because the supervisor's
// version preflight can take up to ten seconds.
func (ms *managedState) begin(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	ms.mu.Lock()
	ms.runCtx, ms.cancel = runCtx, cancel
	ms.mu.Unlock()
	for _, m := range ms.repos.ready() {
		if m.semantic.Load() {
			ms.enable(runCtx, m.id)
		}
	}
}

// Enable starts the managed instance of a mounted repository, or records why
// there is none. It is what `serve` calls for each opted-in repository and what
// a request that switches semantic search on calls. It answers the row as it
// stands and never blocks on the child.
func (ms *managedState) Enable(repo string) managedInstanceView {
	ms.mu.Lock()
	ctx := ms.runCtx
	ms.mu.Unlock()
	if ctx == nil {
		return managedInstanceView{State: managedStateSkipped, OptedIn: true,
			Error: "the server has not started its managed instances yet"}
	}
	return ms.enable(ctx, repo)
}

// enable is Enable under the serve lifetime ctx, which the instance's watcher
// runs in.
func (ms *managedState) enable(runCtx context.Context, repo string) managedInstanceView {
	m, ok := ms.repos.lookup(repo)
	if !ok || !m.ready() {
		return managedInstanceView{State: managedStateSkipped, Error: "no such repository is mounted", OptedIn: true}
	}
	ms.mu.Lock()
	if cur := ms.byRepo[repo]; cur != nil && cur.sup != nil {
		ms.mu.Unlock()
		return ms.viewOf(cur, true)
	}
	slot := &managedRepo{repo: m.id, root: m.path, project: codeProjectID(m)}
	slot.skipped = ms.refusal()
	if slot.skipped == "" {
		slot.skipped = ms.capReachedLocked(repo)
	}
	if slot.skipped == "" {
		sup, err := ms.newInstance(supervisor.Options{
			Binary: ms.res.Binary, CacheDir: ms.cacheDir, RepoRoot: m.path, DocsFolder: managedDocs(m),
			MinVersion: ms.cfg.MinVersion, Debug: ms.cfg.LogLevelOrDefault() == "debug",
			Logger: ms.log.With("pando", m.id),
		})
		if err != nil {
			slot.skipped = "the instance could not be prepared: " + err.Error()
		} else {
			slot.sup = sup
		}
	}
	if _, seen := ms.byRepo[repo]; !seen {
		ms.order = append(ms.order, repo)
	}
	ms.byRepo[repo] = slot
	ctx, stop := context.WithCancel(runCtx)
	if slot.sup != nil {
		slot.stop = stop
		ms.wg.Add(1)
	} else {
		stop()
	}
	ms.mu.Unlock()

	if slot.sup == nil {
		ms.log.Warn("managed Pando not started", "repo", m.id, "reason", slot.skipped)
		return ms.viewOf(slot, true)
	}
	go ms.run(ctx, slot)
	return ms.viewOf(slot, true)
}

// managedDocs is the documentation folder Pando's knowledge base is pointed at.
func managedDocs(m *mount) string {
	if m.docs != "" {
		return m.docs
	}
	if len(m.docsFolders) > 0 {
		return m.docsFolders[0]
	}
	return ""
}

// refusal says why no instance can run at all, empty when one can.
func (ms *managedState) refusal() string {
	switch {
	case ms.res.Unavailable || ms.res.Binary == "":
		return firstNonEmpty(ms.res.Reason, "managed, unavailable: no pando binary")
	case ms.cacheDir == "":
		return "no cache directory is configured for the managed instances"
	}
	return ""
}

// capReachedLocked refuses an instance past maxInstances. ms.mu is held.
func (ms *managedState) capReachedLocked(repo string) string {
	limit := ms.cfg.MaxInstancesOrDefault()
	n := 0
	for id, r := range ms.byRepo {
		if id != repo && r.sup != nil {
			n++
		}
	}
	if n >= limit {
		return fmt.Sprintf("search.pando.managed.maxInstances is %d and %d instances already run: "+
			"raise it or disable semantic search on another repository", limit, n)
	}
	return ""
}

// Disable stops the instance of a repository and forgets it. The data
// directory is kept.
func (ms *managedState) Disable(ctx context.Context, repo string) error {
	ms.mu.Lock()
	slot := ms.byRepo[repo]
	delete(ms.byRepo, repo)
	for i, id := range ms.order {
		if id == repo {
			ms.order = append(ms.order[:i], ms.order[i+1:]...)
			break
		}
	}
	ms.mu.Unlock()
	if slot == nil {
		return nil
	}
	return ms.stopSlot(ctx, slot)
}

func (ms *managedState) stopSlot(ctx context.Context, slot *managedRepo) error {
	slot.mu.Lock()
	sup, stop, client := slot.sup, slot.stop, slot.client
	slot.client = nil
	slot.mu.Unlock()
	if stop != nil {
		stop()
	}
	if client != nil {
		_ = client.Close()
	}
	if sup == nil {
		return nil
	}
	if err := sup.Stop(ctx); err != nil {
		return fmt.Errorf("stop the managed Pando of %s: %w", slot.repo, err)
	}
	return nil
}

// stopAll ends every instance and waits for their goroutines: it is the
// graceful-shutdown path.
func (ms *managedState) stopAll(ctx context.Context) {
	ms.mu.Lock()
	cancel := ms.cancel
	slots := make([]*managedRepo, 0, len(ms.order))
	for _, id := range ms.order {
		slots = append(slots, ms.byRepo[id])
	}
	ms.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var wg sync.WaitGroup
	for _, slot := range slots {
		wg.Add(1)
		go func(slot *managedRepo) {
			defer wg.Done()
			if err := ms.stopSlot(ctx, slot); err != nil {
				ms.log.Warn("managed Pando did not stop cleanly", "repo", slot.repo, "error", err)
			}
		}(slot)
	}
	wg.Wait()
	ms.wg.Wait()
}

// run starts one instance and then watches it: it registers the code project
// once the instance is ready.
func (ms *managedState) run(ctx context.Context, slot *managedRepo) {
	defer ms.wg.Done()
	err := slot.sup.Start(ctx)
	switch {
	case err == nil:
	case errors.Is(err, supervisor.ErrLocked):
		// Another `gintrack serve` owns this instance: connect to it through
		// its state file instead of starting a second child.
		dir := supervisor.InstanceDir(ms.cacheDir, supervisor.InstanceKey(slot.root))
		slot.mu.Lock()
		slot.sup = foreignInstance{dir: dir}
		slot.mu.Unlock()
		ms.log.Info("managed Pando is supervised by another gintrack; connecting to it", "repo", slot.repo)
	default:
		// ErrBinary and ErrVersion leave a failed status that says why; any
		// other failure has none, so it is kept here.
		slot.mu.Lock()
		slot.startErr = err.Error()
		slot.mu.Unlock()
		ms.log.Warn("managed Pando did not start", "repo", slot.repo, "error", err)
	}
	ticker := time.NewTicker(ms.tick)
	defer ticker.Stop()
	for {
		ms.watch(ctx, slot)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// watch is one look at an instance: a ready one that has not been given its
// code project gets it.
func (ms *managedState) watch(ctx context.Context, slot *managedRepo) {
	st := slot.status()
	if st.State != supervisor.StateReady {
		return
	}
	slot.mu.Lock()
	quiet, force := slot.registered, slot.forceIndex
	skip := (slot.registered && slot.registerPID == st.PID) || time.Now().Before(slot.nextTry)
	slot.mu.Unlock()
	if skip {
		return
	}
	client, err := ms.clientOf(slot)
	if err != nil {
		return
	}
	p := codeProject{repo: slot.repo, root: slot.root, id: slot.project}
	var ok bool
	if force {
		// Hand Pando the source tree again whether or not it still knows the
		// project: its indexer skips unchanged files.
		ok = ms.search.registerKnown(ctx, client, nil, p, false)
	} else {
		ok = ms.search.registerProject(ctx, client, p, quiet)
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if ok {
		slot.registered, slot.registerPID, slot.forceIndex = true, st.PID, false
	} else {
		slot.nextTry = time.Now().Add(managedRegisterRetry)
	}
}

// status is the slot's status, with the reason it has none folded in.
func (r *managedRepo) status() supervisor.Status {
	r.mu.Lock()
	sup, startErr := r.sup, r.startErr
	r.mu.Unlock()
	if sup == nil {
		return supervisor.Status{State: supervisor.StateStopped, Project: r.project, Root: r.root}
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

// clientOf returns the session to the slot's current endpoint, rebuilt when the
// port or token changed. It answers an unavailable error saying why when the
// instance is not ready, so a caller can never mistake it for an empty answer.
func (ms *managedState) clientOf(slot *managedRepo) (pandoAPI, error) {
	slot.mu.Lock()
	sup := slot.sup
	slot.mu.Unlock()
	if sup == nil {
		return nil, fmt.Errorf("%w: %s", pando.ErrNotConfigured, firstNonEmpty(slot.skipped, "no managed instance"))
	}
	mcpURL, token, ok := sup.Endpoint()
	if !ok {
		return nil, fmt.Errorf("%w: %s", pando.ErrUnreachable, notReadyReason(slot.status()))
	}
	key := mcpURL + "\x00" + token
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.client != nil && slot.clientKey == key {
		return slot.client, nil
	}
	c, err := ms.newClient(mcpURL, token, slot.project)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", pando.ErrUnreachable, err)
	}
	if slot.client != nil {
		_ = slot.client.Close()
	}
	slot.client, slot.clientKey = c, key
	return c, nil
}

// notReadyReason is the sentence for an instance that cannot answer.
func notReadyReason(st supervisor.Status) string {
	msg := "the managed Pando is " + string(st.State)
	if st.State == "" {
		msg = "the managed Pando is not running"
	}
	if st.LastError != "" {
		msg += ": " + st.LastError
	}
	return msg
}

// slot returns the slot of a repository, nil when none.
func (ms *managedState) slot(repo string) *managedRepo {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.byRepo[repo]
}

// slots lists every slot in mount order.
func (ms *managedState) slots() []*managedRepo {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	out := make([]*managedRepo, 0, len(ms.order))
	for _, id := range ms.order {
		out = append(out, ms.byRepo[id])
	}
	return out
}

// clientFor is the client of one repository's instance.
func (ms *managedState) clientFor(repo string) (pandoAPI, error) {
	slot := ms.slot(repo)
	if slot == nil {
		return nil, fmt.Errorf("%w: semantic search is not enabled for %s", pando.ErrNotConfigured, repo)
	}
	return ms.clientOf(slot)
}

// anyReady reports whether at least one instance is ready.
func (ms *managedState) anyReady() bool {
	for _, slot := range ms.slots() {
		if slot.status().State == supervisor.StateReady {
			return true
		}
	}
	return false
}

// viewOf renders one slot.
func (ms *managedState) viewOf(slot *managedRepo, optedIn bool) managedInstanceView {
	if slot.sup == nil {
		return managedInstanceView{OptedIn: optedIn, State: managedStateSkipped, Error: slot.skipped}
	}
	st := slot.status()
	v := managedInstanceView{
		OptedIn: optedIn, State: string(st.State), PID: st.PID, Port: st.Port, Version: st.Version,
		Crashes: st.Crashes, Error: st.LastError,
	}
	if !st.Since.IsZero() {
		since := st.Since
		v.Since = &since
	}
	return v
}

// viewFor renders the row of one mounted repository: disabled when it did not
// opt in and has no instance.
func (ms *managedState) viewFor(m *mount) managedInstanceView {
	if slot := ms.slot(m.id); slot != nil {
		return ms.viewOf(slot, m.semantic.Load())
	}
	return managedInstanceView{State: managedStateDisabled, OptedIn: m.semantic.Load()}
}

// restart bounces the instances in scope (all of them when repo is empty) and
// answers how many were asked to restart. The supervisor's KBAutoImport
// performs the full sync when the child comes back.
func (ms *managedState) restart(repo string) int {
	n := 0
	for _, slot := range ms.slots() {
		if repo != "" && slot.repo != repo {
			continue
		}
		slot.mu.Lock()
		sup := slot.sup
		slot.mu.Unlock()
		if sup == nil {
			continue
		}
		slot.mu.Lock()
		slot.forceIndex = true
		slot.mu.Unlock()
		sup.Restart()
		n++
	}
	return n
}

// ------------------------------------------------------------- foreign ----

// foreignInstance is an instance another `gintrack serve` supervises. It is
// read through its state file and token, and it is never started, stopped or
// restarted from here.
type foreignInstance struct{ dir string }

func (foreignInstance) Start(context.Context) error { return nil }
func (foreignInstance) Stop(context.Context) error  { return nil }
func (foreignInstance) Restart()                    {}

func (f foreignInstance) Status() supervisor.Status {
	st, err := supervisor.ReadStatus(f.dir)
	if err != nil {
		return supervisor.Status{State: supervisor.StateStopped, LastError: err.Error()}
	}
	if !supervisor.PIDAlive(st.SupervisorPID) {
		st.State = supervisor.StateStopped
		st.LastError = fmt.Sprintf("the gintrack process supervising it (pid %d) is gone", st.SupervisorPID)
	}
	return st
}

func (f foreignInstance) Endpoint() (mcpURL, token string, ok bool) {
	st := f.Status()
	if st.State != supervisor.StateReady || st.MCPURL == "" || !supervisor.PIDAlive(st.PID) {
		return "", "", false
	}
	token, err := supervisor.ReadToken(f.dir)
	if err != nil || token == "" {
		return "", "", false
	}
	return st.MCPURL, token, true
}

// ------------------------------------------------------------- search ------

// managedSearcher fans a semantic search out to every ready instance.
type managedSearcher struct{ ms *managedState }

var _ vault.SemanticSearcher = (*managedSearcher)(nil)

// searcherOf builds the single-instance searcher of one slot, or the reason it
// cannot be built.
func (ms *managedState) searcherOf(slot *managedRepo) (*pandoSearcher, error) {
	client, err := ms.clientOf(slot)
	if err != nil {
		return nil, err
	}
	return &pandoSearcher{
		client: client, repos: ms.repos, log: ms.log,
		projects: []codeProject{{repo: slot.repo, root: slot.root, id: slot.project}},
	}, nil
}

// SearchSemantic asks every ready instance in parallel, inside the one budget
// each pandoSearcher applies, and merges the candidates as a single instance's
// are merged. An instance that is not ready, or fails, costs the answer its own
// hits; when no instance answered at all the result is an error that says why
// for each one, never an empty success.
func (m *managedSearcher) SearchSemantic(ctx context.Context, q vault.SemanticQuery) ([]core.SearchHit, error) {
	slots := m.ms.slots()
	if len(slots) == 0 {
		return nil, fmt.Errorf("%w: no repository has semantic search enabled", pando.ErrNotConfigured)
	}
	if strings.TrimSpace(q.Q) == "" {
		return nil, nil
	}
	type answer struct {
		scored  []scoredHit
		dropped int
		err     error
		skip    bool
	}
	answers := make([]answer, len(slots))
	scope := vault.ScopeKeys(q.Project, q.Projects)
	var wg sync.WaitGroup
	for i, slot := range slots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ps, err := m.ms.searcherOf(slot)
			if err != nil {
				answers[i].err = fmt.Errorf("repository %s: %w", slot.repo, err)
				return
			}
			if !ps.repoInScope(slot.repo, scope) {
				answers[i].skip = true
				return
			}
			scored, dropped, err := ps.collect(ctx, q)
			if err != nil {
				answers[i].err = fmt.Errorf("repository %s: %w", slot.repo, err)
				return
			}
			answers[i].scored, answers[i].dropped = scored, dropped
		}()
	}
	wg.Wait()

	merge := newSemanticMerge(0)
	var errs []error
	answered, dropped := 0, 0
	for _, a := range answers {
		switch {
		case a.err != nil:
			errs = append(errs, a.err)
		case a.skip:
		default:
			answered++
			dropped += a.dropped
			for _, h := range a.scored {
				merge.add(h)
			}
		}
	}
	if answered == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	for _, err := range errs {
		m.ms.log.Debug("a managed Pando did not answer the search", "error", err)
	}
	out := merge.hits(q.Limit)
	if dropped > 0 {
		noteSemanticDrops(ctx, dropped)
	}
	return out, nil
}

// ------------------------------------------------------------- impact ------

// unavailableGraph and unavailableSearcher answer every call with the reason an
// instance cannot, which the impact tiers turn into `unavailable` and that
// reason, never an empty tier.
type unavailableGraph struct{ err error }

func (u unavailableGraph) ImpactAnalysis(context.Context, string, []string, pando.ImpactOptions) (pando.ImpactResult, error) {
	return pando.ImpactResult{}, u.err
}

type unavailableSearcher struct{ err error }

func (u unavailableSearcher) SearchSemantic(context.Context, vault.SemanticQuery) ([]core.SearchHit, error) {
	return nil, u.err
}

// impactGraph is the code graph tier 2 reads for one repository: its own
// instance, nil when the repository has none (the tier then says no Pando is
// configured), or a graph that answers why the instance is not ready.
func (ms *managedState) impactGraph(repo string) impact.CallGraph {
	slot := ms.slot(repo)
	if slot == nil || slot.sup == nil {
		return nil
	}
	client, err := ms.clientOf(slot)
	if err != nil {
		return unavailableGraph{err: err}
	}
	if g, ok := client.(impact.CallGraph); ok && g != nil {
		return g
	}
	return nil
}

// impactSemantic is the searcher tier 3 asks for one repository.
func (ms *managedState) impactSemantic(repo string) vault.SemanticSearcher {
	slot := ms.slot(repo)
	if slot == nil || slot.sup == nil {
		return nil
	}
	ps, err := ms.searcherOf(slot)
	if err != nil {
		return unavailableSearcher{err: err}
	}
	return ps
}

// codeProjects lists the code projects of the repositories that have an
// instance, in mount order.
func (ms *managedState) codeProjects() []codeProject {
	var out []codeProject
	for _, slot := range ms.slots() {
		if slot.sup != nil {
			out = append(out, codeProject{repo: slot.repo, root: slot.root, id: slot.project})
		}
	}
	return out
}
