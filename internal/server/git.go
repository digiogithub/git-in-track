package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Commit-on-save, story GIT-US-0020.
//
// The write path itself is untouched: a handler writes through the vault, and
// only afterwards hands the write set to the committer, which batches it and
// commits it in the background. A git failure therefore never fails a save —
// the file is already on disk and the failure is reported as an event and by
// GET /api/v1/git/status (docs/06-git-sync.md section 3.3).

// gitState holds the git settings and the per-repository backends.
type gitState struct {
	// mu guards settings, template and committer, which the settings endpoint
	// replaces at runtime.
	mu        sync.RWMutex
	settings  config.Git
	template  *gitops.Template
	committer *gitops.Committer

	// backends is fixed after New: a repository either is a git working tree or
	// is not, and that does not change while the process runs.
	backends map[string]gitops.Backend
	// reasons says why a repository has no backend, so the UI can explain it.
	reasons map[string]string
	// resolved is the backend name `auto` picked, for /capabilities.
	resolved string
	version  string
	// configPath is where a settings change is persisted; empty means the
	// change lives only for this process.
	configPath string
	// tool is the `Tool:` trailer of every commit.
	tool string
}

// gitSettings is the JSON shape of GET and PATCH /api/v1/git/settings. It is
// the same shape the browser provider stores per workspace, so the two modes
// expose one contract (docs/05-web-app.md section 11).
type gitSettings struct {
	CommitOnSave     bool   `json:"commitOnSave"`
	CommitDebounceMs int    `json:"commitDebounceMs"`
	MessageTemplate  string `json:"messageTemplate"`
	Backend          string `json:"backend"`
	ResolvedBackend  string `json:"resolvedBackend"`
	GitVersion       string `json:"gitVersion,omitempty"`
	AuthorName       string `json:"authorName,omitempty"`
	AuthorEmail      string `json:"authorEmail,omitempty"`
	SignCommits      bool   `json:"signCommits"`
	// Pending is how many batched edits are waiting to be committed.
	Pending int `json:"pending"`
	// Persisted reports whether a change was written to the configuration file
	// or only applied to the running process.
	Persisted bool `json:"persisted"`
}

// gitRepoStatus is one repository in GET /api/v1/git/status.
type gitRepoStatus struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Git     bool   `json:"git"`
	Reason  string `json:"reason,omitempty"`
	Backend string `json:"backend,omitempty"`
	// Identity is the author commits are made with, empty when none resolves.
	Identity string `json:"identity,omitempty"`
	// IdentityError explains a missing identity, which is the one failure a
	// user has to fix before commit-on-save can work at all.
	IdentityError string              `json:"identityError,omitempty"`
	Status        *gitops.Status      `json:"status,omitempty"`
	Capabilities  gitops.Capabilities `json:"capabilities"`
}

// commitEventData is the payload of the `git.commit` event. The commit is
// debounced, so it cannot be part of the write response; the UI learns about it
// here instead (docs/07 section 5.6).
type commitEventData struct {
	Repo    string   `json:"repo"`
	SHA     string   `json:"sha,omitempty"`
	Subject string   `json:"subject,omitempty"`
	Empty   bool     `json:"empty"`
	Paths   []string `json:"paths,omitempty"`
	Code    string   `json:"code,omitempty"`
	Message string   `json:"message,omitempty"`
}

// newGitState builds the git layer over the mounted repositories.
func newGitState(opts Options, reg *registry, log logger, publish func(commitEventData)) *gitState {
	settings := opts.Git
	if settings.Backend == "" {
		settings.Backend = config.BackendAuto
	}
	if settings.MessageTemplate == "" {
		settings.MessageTemplate = config.DefaultCommitMessageTemplate
	}
	if settings.CommitDebounce == 0 {
		settings.CommitDebounce = config.DefaultCommitDebounce
	}

	g := &gitState{
		settings:   settings,
		backends:   map[string]gitops.Backend{},
		reasons:    map[string]string{},
		configPath: opts.ConfigPath,
		tool:       gitops.ToolName + " " + opts.Version + " (" + opts.Mode + ")",
	}
	g.resolved, g.version = gitops.Resolve(gitops.Kind(settings.Backend), "")

	for _, m := range reg.all() {
		backend, err := gitops.Open(m.path, gitops.Options{
			Backend:     gitops.Kind(settings.Backend),
			AuthorName:  settings.AuthorName,
			AuthorEmail: settings.AuthorEmail,
		})
		if err != nil {
			g.reasons[m.id] = err.Error()
			log.Debug("repository is not driven by git", "repo", m.id, "reason", err)
			continue
		}
		g.backends[m.id] = backend
	}

	tpl, err := gitops.ParseTemplate(settings.MessageTemplate)
	if err != nil {
		log.Warn("the configured commit message template is invalid; falling back to the default",
			"template", settings.MessageTemplate, "error", err)
		tpl = gitops.MustParseTemplate(gitops.DefaultTemplate)
	}
	g.template = tpl
	g.committer = g.newCommitter(tpl, settings, publish)
	return g
}

// logger is the slice of *slog.Logger this file needs; it keeps newGitState
// testable without a logging framework in the way.
type logger interface {
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
}

// newCommitter builds the committer for a set of settings.
func (g *gitState) newCommitter(tpl *gitops.Template, settings config.Git, publish func(commitEventData)) *gitops.Committer {
	return gitops.NewCommitter(gitops.CommitterOptions{
		Debounce: settings.CommitDebounce,
		Template: tpl,
		Sign:     settings.SignCommits,
		Backend:  g.backendFor,
		OnResult: func(out gitops.Outcome) {
			if publish == nil {
				return
			}
			publish(commitEventData{
				Repo:    out.Repo,
				SHA:     out.Result.SHA,
				Subject: out.Result.Subject,
				Empty:   out.Result.Empty,
				Paths:   out.Result.Paths,
				Code:    out.Code,
				Message: out.Message,
			})
		},
	})
}

// backendFor resolves a repository id to its backend.
func (g *gitState) backendFor(repo string) (gitops.Backend, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	b, ok := g.backends[repo]
	return b, ok
}

// enabled reports whether commit-on-save is on.
func (g *gitState) enabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.settings.CommitOnSave
}

// current returns the settings and the committer under one lock.
func (g *gitState) current() (config.Git, *gitops.Committer) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.settings, g.committer
}

// enqueue records a write for commit-on-save. It is a no-op when the feature is
// off, which is what keeps the write path free of git when nobody asked for it.
func (g *gitState) enqueue(change gitops.Change) {
	settings, committer := g.current()
	if !settings.CommitOnSave || committer == nil {
		return
	}
	change.Fields.Tool = g.tool
	committer.Enqueue(change)
}

// flush commits everything pending right now.
func (g *gitState) flush(ctx context.Context) []gitops.Outcome {
	_, committer := g.current()
	if committer == nil {
		return nil
	}
	return committer.Flush(ctx)
}

// close flushes and stops the committer, so a shutdown leaves no edit
// uncommitted.
func (g *gitState) close(ctx context.Context) {
	_, committer := g.current()
	if committer != nil {
		committer.Close(ctx)
	}
}

// pending reports how many batches are waiting.
func (g *gitState) pending() int {
	_, committer := g.current()
	if committer == nil {
		return 0
	}
	return committer.Pending()
}

// view renders the current settings.
func (g *gitState) view() gitSettings {
	g.mu.RLock()
	settings, resolved, version := g.settings, g.resolved, g.version
	g.mu.RUnlock()
	return gitSettings{
		CommitOnSave:     settings.CommitOnSave,
		CommitDebounceMs: int(settings.CommitDebounce / time.Millisecond),
		MessageTemplate:  settings.MessageTemplate,
		Backend:          string(settings.Backend),
		ResolvedBackend:  resolved,
		GitVersion:       version,
		AuthorName:       settings.AuthorName,
		AuthorEmail:      settings.AuthorEmail,
		SignCommits:      settings.SignCommits,
		Pending:          g.pending(),
	}
}

// gitSettingsPatch is the body of PATCH /api/v1/git/settings. Every field is a
// pointer so that an absent one is left alone.
type gitSettingsPatch struct {
	CommitOnSave     *bool   `json:"commitOnSave,omitempty"`
	CommitDebounceMs *int    `json:"commitDebounceMs,omitempty"`
	MessageTemplate  *string `json:"messageTemplate,omitempty"`
	AuthorName       *string `json:"authorName,omitempty"`
	AuthorEmail      *string `json:"authorEmail,omitempty"`
	SignCommits      *bool   `json:"signCommits,omitempty"`
}

// apply validates a patch, swaps the running settings and returns the new view.
// The pending batches are committed with the old template first, so a template
// change never rewrites the message of an edit that was already made.
func (g *gitState) apply(ctx context.Context, patch gitSettingsPatch, publish func(commitEventData)) (gitSettings, error) {
	g.mu.RLock()
	next := g.settings
	g.mu.RUnlock()

	if patch.CommitOnSave != nil {
		next.CommitOnSave = *patch.CommitOnSave
	}
	if patch.CommitDebounceMs != nil {
		if *patch.CommitDebounceMs < 0 {
			return gitSettings{}, &settingsError{field: "commitDebounceMs", message: "must not be negative"}
		}
		next.CommitDebounce = time.Duration(*patch.CommitDebounceMs) * time.Millisecond
	}
	if patch.MessageTemplate != nil {
		next.MessageTemplate = *patch.MessageTemplate
	}
	if patch.AuthorName != nil {
		next.AuthorName = *patch.AuthorName
	}
	if patch.AuthorEmail != nil {
		next.AuthorEmail = *patch.AuthorEmail
	}
	if patch.SignCommits != nil {
		next.SignCommits = *patch.SignCommits
	}

	tpl, err := gitops.ParseTemplate(next.MessageTemplate)
	if err != nil {
		return gitSettings{}, &settingsError{field: "messageTemplate", message: err.Error()}
	}

	// Commit what is queued before the settings change takes effect.
	g.flush(ctx)

	g.mu.Lock()
	g.settings, g.template = next, tpl
	old := g.committer
	g.committer = g.newCommitter(tpl, next, publish)
	g.mu.Unlock()
	if old != nil {
		old.Close(ctx)
	}
	return g.view(), nil
}

// settingsError is one rejected setting.
type settingsError struct {
	field   string
	message string
}

// Error implements the error interface.
func (e *settingsError) Error() string { return e.field + ": " + e.message }

// persist writes the new git section into the configuration file, so a change
// made in the UI survives a restart. A server with no configuration path (a
// test, or `serve --repo`) keeps the change in memory only.
func (g *gitState) persist() (bool, error) {
	g.mu.RLock()
	path, settings := g.configPath, g.settings
	g.mu.RUnlock()
	if path == "" {
		return false, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return false, err //nolint:wrapcheck // config already names the file
	}
	cfg.Git = settings
	if err := config.Save(path, cfg); err != nil {
		return false, err //nolint:wrapcheck // config already names the file
	}
	return true, nil
}

// -------------------------------------------------------------- handlers ---

// mountGit composes the /git subtree. The sync pipeline of GIT-US-0021 mounts
// itself next to it.
func (s *Server) mountGit(r chi.Router) {
	r.Get("/settings", s.handleGitSettings)
	r.Patch("/settings", s.handleGitSettingsPatch)
	r.Get("/status", s.handleGitStatus)
	r.Post("/commit", s.handleGitCommit)
}

// handleGitSettings serves GET /api/v1/git/settings.
func (s *Server) handleGitSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, s.git.view())
}

// handleGitSettingsPatch serves PATCH /api/v1/git/settings.
func (s *Server) handleGitSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var patch gitSettingsPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	view, err := s.git.apply(r.Context(), patch, s.publishCommit)
	if err != nil {
		failProblem(w, r, codeInvalidRequest, err.Error())
		return
	}
	persisted, err := s.git.persist()
	if err != nil {
		// The running process already honours the change; only the file did
		// not take it, and the user has to know which of the two happened.
		s.log.Warn("could not persist the git settings", "error", err)
	}
	view.Persisted = persisted
	writeJSON(w, r, http.StatusOK, view)
}

// handleGitStatus serves GET /api/v1/git/status, for every mounted repository
// or for the one named by ?repo=.
func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	wanted := r.URL.Query().Get("repo")
	out := make([]gitRepoStatus, 0, len(s.repos.all()))
	for _, m := range s.repos.all() {
		if wanted != "" && m.id != wanted {
			continue
		}
		out = append(out, s.gitStatusOf(r.Context(), m))
	}
	if wanted != "" && len(out) == 0 {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+wanted+".")
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"repos":    out,
		"settings": s.git.view(),
	})
}

// gitStatusOf reads one repository's git state.
func (s *Server) gitStatusOf(ctx context.Context, m *mount) gitRepoStatus {
	out := gitRepoStatus{Repo: m.id, Path: m.path}
	backend, ok := s.git.backendFor(m.id)
	if !ok {
		out.Reason = s.git.reasonFor(m.id)
		return out
	}
	out.Git = true
	out.Backend = backend.Name()
	out.Capabilities = backend.Capabilities()
	if id, err := backend.Identity(ctx); err == nil {
		out.Identity = id.String()
	} else {
		out.IdentityError = err.Error()
	}
	if st, err := backend.Status(ctx); err == nil {
		out.Status = &st
	}
	return out
}

// reasonFor explains why a repository has no backend.
func (g *gitState) reasonFor(repo string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.reasons[repo]
}

// handleGitCommit serves POST /api/v1/git/commit: commit the batched edits now,
// which is the "Commit N changes" button of the sync panel and the explicit
// commit a user makes when commit-on-save is off.
func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo    string   `json:"repo,omitempty"`
		Paths   []string `json:"paths,omitempty"`
		Message string   `json:"message,omitempty"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}

	// With no explicit paths the call is a flush of what commit-on-save queued.
	if len(body.Paths) == 0 {
		outcomes := s.git.flush(r.Context())
		writeJSON(w, r, http.StatusOK, map[string]any{"commits": renderOutcomes(outcomes)})
		return
	}

	m, ok := s.repos.lookup(body.Repo)
	if !ok {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+body.Repo+".")
		return
	}
	backend, ok := s.git.backendFor(m.id)
	if !ok {
		failProblem(w, r, codeInvalidRequest,
			"Repository "+m.id+" is not a git working tree: "+s.git.reasonFor(m.id))
		return
	}
	settings, _ := s.git.current()
	msg := gitops.Message{Subject: body.Message}
	if msg.Subject == "" {
		rendered, err := s.git.render(gitops.Fields{Action: gitops.ActionUpdate, Count: len(body.Paths), Tool: s.git.tool})
		if err != nil {
			failProblem(w, r, codeInvalidRequest, err.Error())
			return
		}
		msg = rendered
	}
	res, err := backend.Commit(r.Context(), gitops.CommitRequest{
		Paths:   body.Paths,
		Message: msg,
		Sign:    settings.SignCommits,
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	s.publishCommit(commitEventData{
		Repo: m.id, SHA: res.SHA, Subject: res.Subject, Empty: res.Empty, Paths: res.Paths,
	})
	writeJSON(w, r, http.StatusOK, map[string]any{"commits": []commitEventData{{
		Repo: m.id, SHA: res.SHA, Subject: res.Subject, Empty: res.Empty, Paths: res.Paths,
	}}})
}

// render renders a message with the configured template.
func (g *gitState) render(fields gitops.Fields) (gitops.Message, error) {
	g.mu.RLock()
	tpl := g.template
	g.mu.RUnlock()
	msg, err := tpl.Render(fields)
	if err != nil {
		return gitops.Message{}, err //nolint:wrapcheck // gitops errors already carry a code and a message
	}
	return msg, nil
}

// renderOutcomes projects committer outcomes onto the wire shape.
func renderOutcomes(outcomes []gitops.Outcome) []commitEventData {
	out := make([]commitEventData, 0, len(outcomes))
	for _, o := range outcomes {
		out = append(out, commitEventData{
			Repo: o.Repo, SHA: o.Result.SHA, Subject: o.Result.Subject,
			Empty: o.Result.Empty, Paths: o.Result.Paths,
			Code: o.Code, Message: o.Message,
		})
	}
	return out
}

// publishCommit announces a commit-on-save outcome on the event stream.
func (s *Server) publishCommit(data commitEventData) {
	if data.SHA == "" && data.Code == "" {
		// A no-op commit is not worth an event.
		return
	}
	s.hub.Publish(eventGitCommit, data)
}

// writeGitError maps a gitops failure onto the problem contract.
func writeGitError(w http.ResponseWriter, r *http.Request, err error) {
	code := gitops.CodeOf(err)
	if code == "" {
		code = gitops.CodeCommitFailed
	}
	status := http.StatusInternalServerError
	if code == gitops.CodeNoIdentity || code == gitops.CodeTemplateInvalid || code == gitops.CodeUnsupported {
		status = http.StatusBadRequest
	}
	writeProblem(w, r, status, code, "Git operation failed", err.Error())
}

// ------------------------------------------------------- write path hooks ---

// commitItemWrite queues the commit of one item write. It is called after the
// write has already reached disk, so it can never fail a save.
func (s *Server) commitItemWrite(m *mount, result any, id, op string) {
	if s.git == nil || !s.git.enabled() || m == nil {
		return
	}
	writes, ok := writesOf(result)
	if !ok {
		return
	}
	item, _ := field(result, "item").(*core.Item)
	s.git.enqueue(gitops.Change{
		Repo:   m.id,
		Paths:  pathsOf(writes),
		Fields: itemFields(id, op, item),
	})
}

// commitPageWrite queues the commit of a knowledge-base page write.
func (s *Server) commitPageWrite(m *mount, result any, path string) {
	if s.git == nil || !s.git.enabled() || m == nil {
		return
	}
	writes, ok := writesOf(result)
	if !ok {
		return
	}
	s.git.enqueue(gitops.Change{
		Repo:   m.id,
		Paths:  pathsOf(writes),
		Fields: gitops.Fields{ItemID: path, Title: path, Type: "page", Action: gitops.ActionUpdate},
	})
}

// commitWriteSets queues one commit per repository a multi-repository call
// wrote. A card move touches the item in its project clone and the board in the
// team repository: two repositories, therefore two commits, each in its own
// repository (docs/06 section 9.4).
func (s *Server) commitWriteSets(sets []vault.RepoWriteSet, fields gitops.Fields) {
	if s.git == nil || !s.git.enabled() {
		return
	}
	for _, set := range sets {
		paths := make([]string, 0, len(set.Written)+len(set.Removed))
		for _, f := range set.Written {
			paths = append(paths, f.Path)
		}
		paths = append(paths, set.Removed...)
		if len(paths) == 0 {
			continue
		}
		s.git.enqueue(gitops.Change{Repo: set.VaultID, Paths: paths, Fields: fields})
	}
}

// pathsOf flattens a write set into the paths to stage.
func pathsOf(writes vault.WriteSet) []string {
	out := make([]string, 0, len(writes.Written)+len(writes.Removed))
	for _, f := range writes.Written {
		out = append(out, f.Path)
	}
	return append(out, writes.Removed...)
}

// itemFields builds the template context of an item write.
func itemFields(id, op string, item *core.Item) gitops.Fields {
	fields := gitops.Fields{ItemID: id, Action: actionOf(op)}
	if item != nil {
		fields.Title = item.Title
		fields.Type = string(item.Type)
		fields.Status = string(item.Status)
		if key, _, _, err := core.ParseItemID(id); err == nil {
			fields.ProjectKey = string(key)
		}
	}
	return fields
}

// actionOf maps the REST operation name onto the `{{action}}` placeholder.
func actionOf(op string) gitops.Action {
	switch op {
	case "created":
		return gitops.ActionCreate
	case "deleted":
		return gitops.ActionDelete
	case "moved":
		return gitops.ActionMove
	case "commented":
		return gitops.ActionComment
	default:
		return gitops.ActionUpdate
	}
}

// moveFields builds the template context of a card move: the card, the column
// it landed in and the board it belongs to (docs/06 section 3.3).
func moveFields(moved vault.BoardMoveResult) gitops.Fields {
	fields := gitops.Fields{
		Action: gitops.ActionMove,
		Board:  moved.Board.ID,
		Status: moved.Move.Status,
	}
	if moved.Item != nil {
		fields.ItemID = string(moved.Item.ID)
		fields.Title = moved.Item.Title
		fields.Type = string(moved.Item.Type)
		if fields.Status == "" {
			fields.Status = string(moved.Item.Status)
		}
		if key, _, _, err := core.ParseItemID(string(moved.Item.ID)); err == nil {
			fields.ProjectKey = string(key)
		}
	}
	return fields
}

// sprintFields builds the template context of a sprint or board edit. Neither
// is an item, so the sprint or board id takes the `{{id}}` slot.
func sprintFields(id, kind string, action gitops.Action) gitops.Fields {
	return gitops.Fields{ItemID: id, Title: id, Type: kind, Action: action}
}
