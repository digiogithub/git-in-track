package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The YouTrack connection, story GIT-US-0052.
//
// The browser never speaks to YouTrack. It asks the companion, the companion
// holds the token and the companion makes the call, which is what keeps the
// credential on one machine and out of every devtools network log. There is no
// pass-through endpoint for the same reason ADR-025 refuses to generalize the
// git CORS proxy: only the specific calls the UI needs exist.
//
// The configuration splits in two, exactly as ADR-032 describes. The committed
// half — instance URL, remote project, field map, sync modes — lives in the
// project's project.yaml, so a clone knows where its items came from. The token
// lives in the machine-local 0600 configuration file, keyed by project key, and
// never leaves this process: no response, no problem document, no log line and
// no WebSocket payload carries it.

// youtrackProbeTimeout bounds one outbound call. The router gives a request
// 30 s (server.go); a connection test that needs more than this is a failure
// worth reporting rather than a wait worth extending.
const youtrackProbeTimeout = 15 * time.Second

// youtrackMaxProjects caps the discovery endpoints. The settings autosuggest
// calls them on a keystroke, and a bounded answer is what makes that safe.
const youtrackMaxProjects = 100

// youtrackState owns the YouTrack connection of every mounted project: the
// committed link read from project.yaml, the machine-local tokens, and one
// cached client per project so that a burst of keystrokes shares a single rate
// limiter instead of opening a new bucket per request.
type youtrackState struct {
	// mu guards tokens and clients, which a settings write replaces at runtime.
	mu      sync.RWMutex
	tokens  config.YouTrackTokens
	clients map[string]*youtrack.Client
	// clientKeys remembers what each cached client was built for, so a changed
	// URL or token rebuilds it instead of being silently ignored.
	clientKeys map[string]string

	// repos is the mounted repositories; it is fixed after New.
	repos *registry
	// configPath is where a token change is persisted; empty means the change
	// lives only for this process, exactly as gitState.persist documents.
	configPath string

	// jobClient overrides how a background job resolves its client. It is the
	// one seam the job tests of this package replace, so that a handler can be
	// exercised against a fake instance with no network and no clock. Nil means
	// the cached client of the project (youtrackState.jobClientFor).
	jobClient func(project string) (youtrackJobClient, vault.YouTrackLink, error)
}

// newYouTrackState builds the YouTrack layer over the mounted repositories.
func newYouTrackState(opts Options, reg *registry) *youtrackState {
	return &youtrackState{
		tokens:     opts.YouTrack.Clone(),
		clients:    map[string]*youtrack.Client{},
		clientKeys: map[string]string{},
		repos:      reg,
		configPath: opts.ConfigPath,
	}
}

// youtrackSettings is the JSON shape of GET and PATCH
// /api/v1/youtrack/settings. It deliberately has no token field: there is no
// caller that needs one and every rendering of one is a leak waiting to happen.
type youtrackSettings struct {
	// ProjectKey is the git-in-track project this connection belongs to.
	ProjectKey string `json:"projectKey"`
	// Configured reports whether project.yaml holds an integrations.youtrack
	// block at all.
	Configured bool `json:"configured"`
	// URL is the YouTrack instance URL, context path included.
	URL string `json:"url,omitempty"`
	// Project is the YouTrack project short name.
	Project string `json:"project,omitempty"`
	// FieldMap maps a git-in-track field onto a YouTrack custom field.
	FieldMap map[string]string `json:"fieldMap,omitempty"`
	// PushComments is `manual` or `auto`.
	PushComments string `json:"pushComments,omitempty"`
	// KBSync is `manual` or `on_write`, and KBSyncDirection `push`, `pull` or
	// `both`.
	KBSync          string `json:"kbSync,omitempty"`
	KBSyncDirection string `json:"kbSyncDirection,omitempty"`
	// HasToken reports that a credential resolves for this project, and
	// TokenSource where it came from: `env`, `file`, `flag` or `none`.
	HasToken    bool   `json:"hasToken"`
	TokenSource string `json:"tokenSource"`
	// Persisted reports whether a change reached the configuration file or only
	// the running process, the same contract as the git settings.
	Persisted bool `json:"persisted"`
	// ProjectPath is the project.yaml the committed half was read from,
	// relative to the repository, so the UI can say what a change would edit.
	ProjectPath string `json:"projectPath,omitempty"`
	// Repo is the mounted repository holding that file.
	Repo string `json:"repo,omitempty"`
}

// youtrackSettingsPatch is the sparse body of PATCH
// /api/v1/youtrack/settings. Every field is a pointer so that "absent" and
// "set to empty" stay distinguishable, which is what makes disconnecting a
// project expressible at all.
type youtrackSettingsPatch struct {
	URL             *string            `json:"url"`
	Project         *string            `json:"project"`
	FieldMap        *map[string]string `json:"fieldMap"`
	PushComments    *string            `json:"pushComments"`
	KBSync          *string            `json:"kbSync"`
	KBSyncDirection *string            `json:"kbSyncDirection"`
	// Token is write-only: it is accepted here and never read back. An empty
	// string forgets the stored credential.
	Token *string `json:"token"`
}

// youtrackTestRequest is the body of POST /api/v1/youtrack/test. Both fields
// are optional: with neither, the saved connection is tested; with both, a
// connection the user has typed but not yet saved is.
type youtrackTestRequest struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// youtrackTestResult is what a successful probe reports: who the token
// authenticates as, and against which instance.
type youtrackTestResult struct {
	OK       bool   `json:"ok"`
	BaseURL  string `json:"baseUrl"`
	Login    string `json:"login"`
	FullName string `json:"fullName,omitempty"`
	Email    string `json:"email,omitempty"`
	// Project is the linked YouTrack project when one is configured and could
	// be read with this token.
	Project string `json:"project,omitempty"`
}

// youtrackProjectInfo is one row of GET /api/v1/youtrack/projects.
type youtrackProjectInfo struct {
	ID        string `json:"id"`
	ShortName string `json:"shortName"`
	Name      string `json:"name"`
	Archived  bool   `json:"archived"`
}

// youtrackFieldInfo is one row of GET /api/v1/youtrack/fields: a custom field
// of the remote project, so the field-mapping UI offers real names instead of
// free text.
type youtrackFieldInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type,omitempty"`
	BundleID   string `json:"bundleId,omitempty"`
	BundleType string `json:"bundleType,omitempty"`
	CanBeEmpty bool   `json:"canBeEmpty"`
}

// link is the committed half of a project's connection, together with where it
// was read from. A project with no block yields a nil link and no error.
type youtrackLink struct {
	link *config.YouTrackLink
	// path is the absolute project.yaml the block lives in.
	path string
	// rel is that file's path inside the repository, which is what a UI shows.
	rel string
	// repo is the mounted repository id.
	repo string
}

// linkFor reads the committed half of one project's connection.
func (y *youtrackState) linkFor(projectKey string) (youtrackLink, error) {
	m, ok := y.repos.forProject(projectKey)
	if !ok {
		return youtrackLink{}, fmt.Errorf("%w: no mounted repository exposes project %s", errYouTrackNoProject, projectKey)
	}
	for _, ref := range m.vlt.Projects() {
		if string(ref.Key) != projectKey {
			continue
		}
		out := youtrackLink{path: filepath.Join(m.path, filepath.FromSlash(ref.ConfigPath)), rel: ref.ConfigPath, repo: m.id}
		parsed, err := config.LoadYouTrackLink(out.path)
		if err != nil {
			return youtrackLink{}, err //nolint:wrapcheck // config already names the file
		}
		out.link = parsed
		return out, nil
	}
	return youtrackLink{}, fmt.Errorf("%w: no mounted repository exposes project %s", errYouTrackNoProject, projectKey)
}

// errYouTrackNoProject is returned when a request names a project this
// companion does not serve.
var errYouTrackNoProject = errors.New("unknown project")

// projectKeys returns every mounted project key, sorted.
func (y *youtrackState) projectKeys() []string {
	var out []string
	for _, m := range y.repos.ready() {
		out = append(out, m.projectKeys()...)
	}
	sort.Strings(out)
	return out
}

// configured reports whether at least one mounted project declares a YouTrack
// block. It is what `features.youtrack` answers: browser-only mode has no
// companion and therefore never reports it at all.
func (y *youtrackState) configured() bool {
	for _, key := range y.projectKeys() {
		if found, err := y.linkFor(key); err == nil && found.link != nil {
			return true
		}
	}
	return false
}

// view renders the settings of one project, token excluded.
func (y *youtrackState) view(projectKey string) (youtrackSettings, error) {
	found, err := y.linkFor(projectKey)
	if err != nil {
		return youtrackSettings{}, err
	}
	out := youtrackSettings{ProjectKey: projectKey, ProjectPath: found.rel, Repo: found.repo}
	if found.link != nil {
		out.Configured = true
		out.URL = found.link.URL
		out.Project = found.link.Project
		out.FieldMap = found.link.FieldMap
		out.PushComments = string(found.link.PushComments)
		out.KBSync = string(found.link.KBSync)
		out.KBSyncDirection = string(found.link.KBSyncDirection)
	}
	y.mu.RLock()
	token, source := y.tokens.For(projectKey)
	y.mu.RUnlock()
	out.HasToken = token != ""
	out.TokenSource = string(source)
	return out, nil
}

// apply folds a sparse patch into the committed half and, when the patch
// carries one, into the stored credential. It reports whether the token half
// reached the configuration file.
func (y *youtrackState) apply(projectKey string, patch youtrackSettingsPatch) (persisted bool, err error) {
	found, err := y.linkFor(projectKey)
	if err != nil {
		return false, err
	}
	if link, touched := mergeYouTrackPatch(found.link, patch); touched {
		if _, err := config.SaveYouTrackLink(found.path, link); err != nil {
			return false, err //nolint:wrapcheck // config already names the file and the field
		}
	}
	if patch.Token == nil {
		return false, nil
	}
	return y.setToken(projectKey, *patch.Token)
}

// mergeYouTrackPatch applies the committed half of a patch, reporting whether
// anything in it addressed project.yaml at all.
func mergeYouTrackPatch(current *config.YouTrackLink, patch youtrackSettingsPatch) (config.YouTrackLink, bool) {
	out := config.YouTrackLink{}
	if current != nil {
		out = *current
	}
	touched := false
	if patch.URL != nil {
		out.URL, touched = *patch.URL, true
	}
	if patch.Project != nil {
		out.Project, touched = *patch.Project, true
	}
	if patch.FieldMap != nil {
		out.FieldMap, touched = *patch.FieldMap, true
	}
	if patch.PushComments != nil {
		out.PushComments, touched = config.PushCommentsMode(*patch.PushComments), true
	}
	if patch.KBSync != nil {
		out.KBSync, touched = config.KBSyncMode(*patch.KBSync), true
	}
	if patch.KBSyncDirection != nil {
		out.KBSyncDirection, touched = config.KBSyncDirection(*patch.KBSyncDirection), true
	}
	return out, touched
}

// setToken stores a credential for a project. It follows gitState.persist
// exactly: reload the file, replace one section, save, and report whether the
// file took the change. A server with no configuration path — a test, or
// `serve --repo` — keeps the change in this process only.
func (y *youtrackState) setToken(projectKey, token string) (bool, error) {
	y.mu.Lock()
	path := y.configPath
	if token == "" {
		y.tokens.Clear(projectKey)
	} else {
		y.tokens.Set(projectKey, token)
	}
	y.dropClientLocked(projectKey)
	y.mu.Unlock()

	if path == "" {
		return false, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return false, err //nolint:wrapcheck // config already names the file
	}
	if token == "" {
		cfg.ClearYouTrackToken(projectKey)
	} else {
		cfg.SetYouTrackToken(projectKey, token)
	}
	if err := config.Save(path, cfg); err != nil {
		return false, err //nolint:wrapcheck // config already names the file
	}
	return true, nil
}

// dropClientLocked forgets the cached client of a project. The caller holds mu.
func (y *youtrackState) dropClientLocked(projectKey string) {
	delete(y.clients, projectKey)
	delete(y.clientKeys, projectKey)
}

// clientFor returns a client for a project's saved connection, building one on
// first use and reusing it afterwards so that every caller shares one rate
// limiter. A project with no block, or with no token, is not an error the
// caller can fix by retrying: it is reported as youtrack_not_configured.
func (y *youtrackState) clientFor(projectKey string) (*youtrack.Client, *config.YouTrackLink, error) {
	found, err := y.linkFor(projectKey)
	if err != nil {
		return nil, nil, err
	}
	if found.link == nil {
		return nil, nil, fmt.Errorf("%w: project %s declares no integrations.youtrack block", errYouTrackNotConfigured, projectKey)
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	token, _ := y.tokens.For(projectKey)
	if token == "" {
		return nil, nil, fmt.Errorf("%w: no YouTrack token is stored for project %s", errYouTrackNotConfigured, projectKey)
	}
	// The identity of a cached client is its instance and its credential. The
	// token is hashed into the key by length alone — comparing the value would
	// be correct but would keep a second copy of it in a map key.
	want := found.link.URL + "\n" + strconv.Itoa(len(token))
	if cached, ok := y.clients[projectKey]; ok && y.clientKeys[projectKey] == want {
		return cached, found.link, nil
	}
	client, err := youtrack.New(youtrack.Options{BaseURL: found.link.URL, Token: token})
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // the client redacts the token on every error path
	}
	y.clients[projectKey] = client
	y.clientKeys[projectKey] = want
	return client, found.link, nil
}

// errYouTrackNotConfigured is returned when a project has no usable connection.
var errYouTrackNotConfigured = errors.New("youtrack is not configured")

// -------------------------------------------------------------- handlers ---

// mountYouTrack composes the /youtrack subtree. It sits inside the
// authenticated group, so every route below needs the companion's bearer token.
func (s *Server) mountYouTrack(r chi.Router) {
	r.Get("/settings", s.handleYouTrackSettings)
	r.Patch("/settings", s.handleYouTrackSettingsPatch)
	r.Post("/test", s.handleYouTrackTest)
	r.Get("/projects", s.handleYouTrackProjects)
	r.Get("/fields", s.handleYouTrackFields)
	// The issue search the import dialog types into (GIT-US-0054). It is a
	// read, so it needs no If-Match and takes no body.
	r.Get("/issues", s.handleYouTrackIssues)
	// The two halves of running an import: what it would do, and doing it.
	r.Post("/import/preview", s.handleYouTrackImportPreview)
	r.Post("/import", s.handleYouTrackImport)
}

// youtrackProjectKey resolves the `key` query parameter onto a mounted project,
// defaulting to the only one when the companion serves exactly one. It writes
// the problem document itself and reports whether a key was found.
func (s *Server) youtrackProjectKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	if key := r.URL.Query().Get("key"); key != "" {
		return key, true
	}
	keys := s.youtrack.projectKeys()
	if len(keys) == 1 {
		return keys[0], true
	}
	if len(keys) == 0 {
		failProblem(w, r, codeRepoNotRegistered,
			"This companion serves no project. Register a repository with `gintrack add <path>`.")
		return "", false
	}
	failProblem(w, r, codeInvalidRequest,
		"This companion serves several projects: name one with ?key=<projectKey>.")
	return "", false
}

// handleYouTrackSettings serves GET /api/v1/youtrack/settings.
func (s *Server) handleYouTrackSettings(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackProjectKey(w, r)
	if !ok {
		return
	}
	view, err := s.youtrack.view(key)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, view)
}

// handleYouTrackSettingsPatch serves PATCH /api/v1/youtrack/settings.
func (s *Server) handleYouTrackSettingsPatch(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackProjectKey(w, r)
	if !ok {
		return
	}
	var patch youtrackSettingsPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	persisted, err := s.youtrack.apply(key, patch)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	view, err := s.youtrack.view(key)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	view.Persisted = persisted
	writeJSON(w, r, http.StatusOK, view)
}

// handleYouTrackTest serves POST /api/v1/youtrack/test. It accepts an unsaved
// URL and token so that a user can test a connection before committing it.
func (s *Server) handleYouTrackTest(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackProjectKey(w, r)
	if !ok {
		return
	}
	var body youtrackTestRequest
	if r.ContentLength != 0 && !decodeBody(w, r, &body) {
		return
	}

	client, link, err := s.youtrackProbeClient(key, body)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), youtrackProbeTimeout)
	defer cancel()

	me, err := client.Me(ctx)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	out := youtrackTestResult{
		OK:       true,
		BaseURL:  client.BaseURL(),
		Login:    me.Login,
		FullName: me.FullName,
		Email:    me.Email,
	}
	if link != nil && link.Project != "" {
		// A token that authenticates but cannot see the linked project is a
		// distinct, common failure; report the project when it resolves and
		// stay silent when it does not, rather than failing the whole probe.
		if project, err := client.Project(ctx, link.Project); err == nil {
			out.Project = project.ShortName
		}
	}
	writeJSON(w, r, http.StatusOK, out)
}

// youtrackProbeClient builds the client a test runs against: the one the saved
// settings describe, or a throwaway one for the URL and token in the body.
func (s *Server) youtrackProbeClient(key string, body youtrackTestRequest) (*youtrack.Client, *config.YouTrackLink, error) {
	if body.URL == "" && body.Token == "" {
		return s.youtrack.clientFor(key)
	}
	found, linkErr := s.youtrack.linkFor(key)
	link := found.link
	baseURL, token := body.URL, body.Token
	if baseURL == "" && link != nil {
		baseURL = link.URL
	}
	if token == "" {
		token, _ = s.youtrack.tokenFor(key)
	}
	if baseURL == "" || token == "" {
		if linkErr != nil {
			return nil, nil, linkErr
		}
		return nil, nil, fmt.Errorf("%w: a URL and a token are needed to test a connection", errYouTrackNotConfigured)
	}
	client, err := youtrack.New(youtrack.Options{BaseURL: baseURL, Token: token})
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // the client redacts the token on every error path
	}
	return client, link, nil
}

// tokenFor resolves a project's credential for the handlers of this file.
func (y *youtrackState) tokenFor(projectKey string) (string, config.TokenSource) {
	y.mu.RLock()
	defer y.mu.RUnlock()
	return y.tokens.For(projectKey)
}

// handleYouTrackProjects serves GET /api/v1/youtrack/projects?q=. It is called
// on a keystroke, so the page is capped here as well as by the client's own
// rate limiter.
func (s *Server) handleYouTrackProjects(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackProjectKey(w, r)
	if !ok {
		return
	}
	client, _, err := s.youtrack.clientFor(key)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), youtrackProbeTimeout)
	defer cancel()

	projects, err := client.Projects(ctx, r.URL.Query().Get("q"), youtrack.Page{Top: youtrackMaxProjects})
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	out := make([]youtrackProjectInfo, 0, len(projects))
	for _, p := range projects {
		if len(out) == youtrackMaxProjects {
			break
		}
		out = append(out, youtrackProjectInfo{ID: p.ID, ShortName: p.ShortName, Name: p.Name, Archived: p.Archived})
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"projects": out, "total": len(out), "limit": youtrackMaxProjects})
}

// handleYouTrackFields serves GET /api/v1/youtrack/fields?project=. The
// `project` parameter is the YouTrack project, short name or internal id; it
// defaults to the linked one.
func (s *Server) handleYouTrackFields(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackProjectKey(w, r)
	if !ok {
		return
	}
	client, link, err := s.youtrack.clientFor(key)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	project := r.URL.Query().Get("project")
	if project == "" && link != nil {
		project = link.Project
	}
	if project == "" {
		failProblem(w, r, codeInvalidRequest, "Name the YouTrack project with ?project=<shortName>.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), youtrackProbeTimeout)
	defer cancel()

	settings, err := client.CustomFieldSettings(ctx, project)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	out := make([]youtrackFieldInfo, 0, len(settings))
	for _, setting := range settings {
		out = append(out, youtrackFieldInfo{
			ID:         setting.Field.ID,
			Name:       setting.Field.Name,
			Type:       setting.Field.FieldType.ID,
			BundleID:   setting.Bundle.ID,
			BundleType: setting.Bundle.Type,
			CanBeEmpty: setting.CanBeEmpty,
		})
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"project": project,
		"fields":  out,
		"total":   len(out),
		// The git-in-track side of a mapping, so the UI offers both halves from
		// one call instead of hard-coding the list in the frontend.
		"gintrackFields": config.FieldMapKeys,
	})
}

// failYouTrack turns an error from the client or from this package into the
// problem document the settings UI switches on.
//
// It never renders the cause of a client error itself: internal/youtrack
// redacts the token on every error path and this is where that guarantee would
// be undone by a well-meaning "%v of the underlying error".
func (s *Server) failYouTrack(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errYouTrackNoProject):
		failProblem(w, r, codeNotFound, err.Error())
	case errors.Is(err, errYouTrackNotConfigured), errors.Is(err, youtrack.ErrInvalidInput):
		failProblem(w, r, codeYouTrackNotConfigured,
			"This project has no usable YouTrack connection yet. Set the instance URL, the project and a token first.")
	case errors.Is(err, config.ErrInvalid):
		failProblem(w, r, codeInvalidRequest, err.Error())
	case errors.Is(err, youtrack.ErrUnauthorized):
		failProblem(w, r, codeYouTrackUnauthorized,
			"YouTrack rejected the permanent token. Check that it has not expired and that it was copied whole, the `perm:` prefix included.")
	case errors.Is(err, youtrack.ErrForbidden):
		failProblem(w, r, codeYouTrackForbidden,
			"The token authenticates, but the account it belongs to lacks permission for this resource. Grant it access to the project in YouTrack.")
	case errors.Is(err, youtrack.ErrNotFound):
		failProblem(w, r, codeYouTrackNotFound,
			"YouTrack answered 404. The instance URL is most likely missing its context path, as in https://host/youtrack.")
	case errors.Is(err, youtrack.ErrRateLimited):
		failProblem(w, r, "rate_limited",
			"YouTrack is throttling this client. Wait a moment and try again.")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		failProblem(w, r, codeYouTrackUnreachable,
			"The YouTrack instance did not answer in time.")
	default:
		s.log.Warn("youtrack request failed", "error", err)
		failProblem(w, r, codeYouTrackUnreachable,
			"The YouTrack instance could not be reached. Check the URL and that this machine can reach the host.")
	}
}
