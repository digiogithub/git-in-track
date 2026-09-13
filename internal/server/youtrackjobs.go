package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/syncengine"
	"github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The YouTrack background job kinds, stories GIT-US-0050, GIT-US-0068 and
// GIT-US-0087.
//
// internal/syncengine owns scheduling, batching, rate limiting, retries and the
// journal; internal/server/syncengine.go owns the engine's lifecycle. This file
// and the three beside it own the work: what an import, a comment push and a
// knowledge-base publish or pull actually do.
//
// Four rules hold for every handler here, and breaking any of them breaks the
// engine's contract rather than just the feature:
//
//  1. **Idempotence.** The same job is handed to a handler again after a
//     retryable error, after a restart that replayed the journal, and when a
//     user retries it from the dead-letter list. Every write below is therefore
//     keyed on an external reference — the pair (system, id) — so that running
//     it twice updates rather than duplicates.
//
//  2. **Classification.** A handler error is read by [syncengine.Classify],
//     which understands an HTTP status only through the StatusCoder interface.
//     youtrack.APIError carries its status in a *field*, so it satisfies
//     nothing: classifyYouTrackJobError below is what turns a 403 into a
//     terminal failure instead of five pointless attempts against a rejected
//     token.
//
//  3. **Cancellation.** ctx is cancelled when the job is cancelled and when the
//     engine is closed with an expired deadline. Every loop that can run long
//     checks it, and a cancelled job reports how far it got rather than
//     pretending to have failed.
//
//  4. **No inline pushes.** Nothing in a write path calls a handler directly.
//     The vault decides that a job is needed and hands it to the enqueuer
//     installed below; the engine decides when it runs.

// The kinds this package registers. Three of them are named by internal/vault,
// which is what decides a job is needed, so the constant lives there and this
// package must not spell the string a second time.
const (
	// kindYouTrackImport imports a set of issues into a project's backlog.
	kindYouTrackImport syncengine.Kind = "youtrack.import"
	// kindYouTrackCommentPush pushes one comment file to the issue its item
	// mirrors.
	kindYouTrackCommentPush syncengine.Kind = vault.JobKindCommentPush
	// kindYouTrackKBPublish publishes one page, or one subtree, as articles.
	kindYouTrackKBPublish syncengine.Kind = vault.JobKindKBPublish
	// kindYouTrackKBPull writes one page, or one subtree, back from its
	// articles.
	kindYouTrackKBPull syncengine.Kind = vault.JobKindKBPull
)

// youtrackJobClient is everything the four handlers ask of a YouTrack
// instance. It is an interface rather than *youtrack.Client for the same reason
// vault.YouTrackSource is: the transport stays in internal/youtrack, and every
// test in this package runs against a fake with no network and no clock.
//
// It embeds vault.YouTrackSource because the import installs the very same
// client as the vault's provider, so the two must be one value.
type youtrackJobClient interface {
	vault.YouTrackSource

	// SearchIssues runs one page of an issue search. The import pages
	// explicitly rather than calling SearchAllIssues so that it can stop at
	// vault.YouTrackMaxIssues and observe cancellation between pages.
	SearchIssues(ctx context.Context, query string, page youtrack.Page) ([]youtrack.Issue, error)
	// DownloadAttachment opens an attachment for reading. The caller closes it.
	DownloadAttachment(ctx context.Context, attachment youtrack.Attachment) (io.ReadCloser, error)

	// AddComment creates an issue comment and returns the created one.
	AddComment(ctx context.Context, id, text string) (youtrack.Comment, error)

	// The knowledge-base half. Article is inherited from vault.YouTrackSource;
	// ChildArticles walks the tree downwards, SearchArticles finds a parent
	// article a previous publish created, and the other two write.
	CreateArticle(ctx context.Context, in youtrack.ArticleInput) (youtrack.Article, error)
	UpdateArticle(ctx context.Context, id string, in youtrack.ArticleInput) (youtrack.Article, error)
	ChildArticles(ctx context.Context, id string) ([]youtrack.ArticleRef, error)
	SearchArticles(ctx context.Context, query string, page youtrack.Page) ([]youtrack.Article, error)
}

// youtrackCommentEditor is the optional half of the comment push: editing a
// comment that was already pushed, with POST /api/issues/{id}/comments/{cid}.
//
// It is a separate, optionally implemented interface because the shipped
// *youtrack.Client does not carry the method yet (see the report on
// GIT-T-0154). A client that grows one satisfies this without a change here; a
// client that has not makes the edit path fail terminally with a message that
// says exactly that, instead of silently posting a second comment.
type youtrackCommentEditor interface {
	UpdateComment(ctx context.Context, id, commentID, text string) (youtrack.Comment, error)
}

// youtrackJobPayload is the envelope every job of this package carries.
//
// The envelope is uniform on purpose: a handler has to find the repository and
// the project before it can do anything, and journalling one shape means a
// replayed job from an older build is still routable. Params is the kind's own
// arguments, left as raw JSON so that this file never learns what they are.
//
// Nothing here is content and nothing here is a credential: the journal stores
// it verbatim (see [syncengine.Job]).
type youtrackJobPayload struct {
	// Repo is the mounted repository id. It is recorded when the job is
	// created so that a replayed job does not have to guess again.
	Repo string `json:"repo,omitempty"`
	// Project is the git-in-track project key the job runs against.
	Project string `json:"project,omitempty"`
	// Params are the kind's own arguments.
	Params json.RawMessage `json:"params,omitempty"`
}

// decodeJobPayload reads the envelope of one job. A payload that cannot be
// decoded is terminal: no number of attempts will make malformed JSON parse.
func decodeJobPayload(job syncengine.Job) (youtrackJobPayload, error) {
	var out youtrackJobPayload
	if len(job.Payload) == 0 {
		return out, terminalf("job %s carries no payload", job.ID)
	}
	if err := json.Unmarshal(job.Payload, &out); err != nil {
		return out, terminalf("decode the payload of job %s: %w", job.ID, err)
	}
	return out, nil
}

// decodeJobParams reads the kind's own arguments out of an envelope.
func decodeJobParams[T any](payload youtrackJobPayload) (T, error) {
	var out T
	if len(payload.Params) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(payload.Params, &out); err != nil {
		return out, terminalf("decode the job parameters: %w", err)
	}
	return out, nil
}

// terminalf builds a handler error the engine must not retry.
//
// It exists so that the one place this package reaches for
// [syncengine.Terminal] with a freshly formatted message is a single function:
// wrapcheck reads a call to another package's constructor as an unwrapped
// error, which is exactly what Terminal is not.
func terminalf(format string, args ...any) error {
	//nolint:wrapcheck // syncengine.Terminal is the wrapper the engine's classifier reads
	return syncengine.Terminal(fmt.Errorf(format, args...))
}

// ------------------------------------------------------------ resolution ---

// errYouTrackJobUnroutable is the failure of a job that names a repository or a
// project this companion does not serve. It is terminal: the queue outlives a
// configuration change, so a journalled job can name a repository that was
// unregistered in between, and retrying it forever would keep a dead job alive.
var errYouTrackJobUnroutable = errors.New("the job names nothing this companion serves")

// jobMount resolves the repository and the project key a job runs against. The
// recorded repository wins; the project key is the fallback, which is what
// makes a payload written by a caller that only knew the project still work.
func (s *Server) jobMount(payload youtrackJobPayload) (*mount, string, error) {
	project := strings.TrimSpace(payload.Project)
	if id := strings.TrimSpace(payload.Repo); id != "" {
		if m, ok := s.repos.lookup(id); ok && m.ready() {
			return m, s.projectOfMount(m, project), nil
		}
	}
	if project != "" {
		if m, ok := s.repos.forProject(project); ok && m.ready() {
			return m, project, nil
		}
	}
	return nil, "", terminalf("%w: repo %q, project %q",
		errYouTrackJobUnroutable, payload.Repo, payload.Project)
}

// projectOfMount returns the project key a job runs against: the one it named,
// or the mount's only project when it named none.
func (s *Server) projectOfMount(m *mount, project string) string {
	if project != "" {
		return project
	}
	if keys := m.projectKeys(); len(keys) == 1 {
		return keys[0]
	}
	return ""
}

// jobClient resolves the YouTrack client and the project link one job runs
// against, through the seam tests replace.
func (s *Server) jobClient(project string) (youtrackJobClient, vault.YouTrackLink, error) {
	return s.youtrack.jobClientFor(project)
}

// jobClientFor is the production resolution: the cached client of the project,
// so that every job shares one rate limiter with every settings request.
//
// A project with no block or no token is reported as not configured, which
// classifyYouTrackJobError turns into a terminal failure: a job cannot fix a
// missing credential by trying again.
func (y *youtrackState) jobClientFor(project string) (youtrackJobClient, vault.YouTrackLink, error) {
	y.mu.RLock()
	seam := y.jobClient
	y.mu.RUnlock()
	if seam != nil {
		return seam(project)
	}
	client, link, err := y.clientFor(project)
	if err != nil {
		return nil, vault.YouTrackLink{}, err
	}
	return client, youtrackLinkOf(client, link), nil
}

// youtrackLinkOf renders the committed half of a connection as the plain struct
// internal/vault takes. The base URL comes from the client rather than from the
// file, because the client is what normalised it.
func youtrackLinkOf(client *youtrack.Client, link *config.YouTrackLink) vault.YouTrackLink {
	out := vault.YouTrackLink{BaseURL: client.BaseURL()}
	if link != nil {
		out.Project = link.Project
		out.FieldMap = link.FieldMap
	}
	return out
}

// ------------------------------------------------------------- the seams ---

// installYouTrackSeams hands every mounted vault the two hooks it needs to talk
// to a tracker: the provider that resolves a client, and the enqueuer that
// hands a job to the engine.
//
// Both are installed in New, before anything is listening and before the
// journal is replayed, because a vault that answers "youtrack.kb.publish"
// before the enqueuer exists would report the feature as unavailable.
func (s *Server) installYouTrackSeams() {
	for _, m := range s.repos.ready() {
		s.installYouTrackSeamsOn(m)
	}
}

// installYouTrackSeamsOn installs the two hooks on one mount. The repository id
// is captured here, which is what lets a queued job find its way back to the
// vault that created it after a restart.
func (s *Server) installYouTrackSeamsOn(m *mount) {
	repo := m.id
	m.vlt.SetYouTrackProvider(func(_ context.Context, project string) (vault.YouTrackSource, vault.YouTrackLink, error) {
		client, link, err := s.jobClient(s.projectOfMount(m, project))
		if err != nil {
			return nil, vault.YouTrackLink{}, err
		}
		return client, link, nil
	})
	m.vlt.SetYouTrackEnqueuer(func(ctx context.Context, job vault.YouTrackJob) (string, error) {
		// Detached on purpose: the HTTP response that asked for the job ends
		// long before the job runs, and a queue entry cancelled by its own
		// request going away would be work silently dropped. The same idiom
		// guards the committer and the tunnel driver.
		return s.enqueueYouTrackJob(context.WithoutCancel(ctx), syncengine.Kind(job.Kind), job.Key,
			youtrackJobPayload{Repo: repo, Project: job.Project}, job.Payload)
	})
}

// enqueueYouTrackJob encodes an envelope and hands it to the engine, returning
// the job id the caller reports back to whoever asked.
func (s *Server) enqueueYouTrackJob(
	ctx context.Context, kind syncengine.Kind, key string, envelope youtrackJobPayload, params any,
) (string, error) {
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return "", fmt.Errorf("encode the parameters of a %s job: %w", kind, err)
		}
		envelope.Params = raw
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encode the payload of a %s job: %w", kind, err)
	}
	job, err := s.sync.engine.Enqueue(ctx, syncengine.Request{Kind: kind, Key: key, Payload: body})
	if err != nil {
		return "", fmt.Errorf("queue a %s job: %w", kind, err)
	}
	return job.ID, nil
}

// ---------------------------------------------------------- registration ---

// registerYouTrackJobs registers the four handlers this package owns.
//
// It runs in New, which is the only moment that satisfies the engine's
// contract: [Server.Start] replays the journal, and a replayed job whose kind
// has no handler can never be dispatched.
func (s *Server) registerYouTrackJobs() error {
	handlers := map[syncengine.Kind]syncengine.Handler{
		kindYouTrackImport:      syncengine.HandlerFunc(s.handleYouTrackImportJobs),
		kindYouTrackCommentPush: syncengine.HandlerFunc(s.handleYouTrackCommentJobs),
		kindYouTrackKBPublish:   syncengine.HandlerFunc(s.handleYouTrackKBPublishJobs),
		kindYouTrackKBPull:      syncengine.HandlerFunc(s.handleYouTrackKBPullJobs),
	}
	for _, kind := range []syncengine.Kind{
		kindYouTrackImport, kindYouTrackCommentPush, kindYouTrackKBPublish, kindYouTrackKBPull,
	} {
		if err := s.RegisterSyncHandler(kind, handlers[kind]); err != nil {
			return err
		}
	}
	return nil
}

// ------------------------------------------------------- error handling ---

// classifyYouTrackJobError turns an error from a handler into one the engine
// reads correctly.
//
// It exists because youtrack.APIError carries its status in a field rather than
// behind a StatusCode() method, so [syncengine.Classify] cannot see it and
// would treat every failure — a rejected token included — as worth another
// four attempts. Everything else is left alone: an error that says nothing is
// most often a transport failure, which is exactly the one worth repeating.
func classifyYouTrackJobError(err error) error {
	if err == nil {
		return nil
	}
	// Cancellation is not a failure and must reach the engine unwrapped, so
	// that it ends the job in `cancelled` rather than spending attempts on it.
	if errors.Is(err, context.Canceled) {
		return err
	}
	var api *youtrack.APIError
	if errors.As(err, &api) {
		if syncengine.ClassifyStatus(api.Status) == syncengine.ClassTerminal {
			return terminalf("%w", err)
		}
		return err
	}
	switch {
	case errors.Is(err, youtrack.ErrInvalidInput),
		errors.Is(err, youtrack.ErrUnauthorized),
		errors.Is(err, youtrack.ErrForbidden),
		errors.Is(err, youtrack.ErrNotFound),
		errors.Is(err, errYouTrackNotConfigured),
		errors.Is(err, errYouTrackNoProject),
		errors.Is(err, errYouTrackJobUnroutable):
		return terminalf("%w", err)
	}
	return err
}

// --------------------------------------------------------------- progress ---

// syncJobProgressData is the payload of `sync.job.progress` published by a
// handler that knows how far it has got (docs/07-cli-and-api.md section 5.6).
//
// It is deliberately a superset of the engine's own event payload: a job that
// narrates itself carries `jobId`, `done`, `total` and `currentId`, and repeats
// the first two under the `id` and `processed` names the engine's queue events
// use, so that one client-side reader handles both sources without branching on
// which produced the frame.
type syncJobProgressData struct {
	// JobID and ID are the same engine job id under both spellings.
	JobID string `json:"jobId"`
	ID    string `json:"id"`
	// Kind and Key identify the job family and its coalescing group.
	Kind string `json:"kind"`
	Key  string `json:"key,omitempty"`
	// State is the engine state the job is in, always `running` here: a
	// terminal transition is announced by the engine's own event.
	State string `json:"state"`
	// Done and Processed are the same count under both spellings, and Total is
	// how many units of work this job has.
	Done      int `json:"done"`
	Processed int `json:"processed"`
	Total     int `json:"total"`
	// CurrentID is the unit the job has just finished: an issue id, a page
	// path. It is what a progress line renders beside the bar.
	CurrentID string `json:"currentId,omitempty"`
	// Failed is how many units failed without failing the job. Per-unit
	// failures accumulate in the result rather than aborting the run.
	Failed int `json:"failed,omitempty"`
}

// publishJobProgress announces how far one job has got.
//
// Unlike the queue transitions, this is not throttled here: a handler publishes
// once per batch or once per page, which is already coarse, and throttling a
// count nobody else will publish would leave a bar stuck. A handler that can
// produce a frame per item must batch first — the import does.
func (s *Server) publishJobProgress(job syncengine.Job, done, total, failed int, currentID string) {
	if s.hub == nil {
		return
	}
	s.hub.Publish(eventSyncJobProgress, syncJobProgressData{
		JobID: job.ID, ID: job.ID, Kind: string(job.Kind), Key: job.Key,
		State:     string(syncengine.StateRunning),
		Done:      done,
		Processed: done,
		Total:     total,
		CurrentID: currentID,
		Failed:    failed,
	})
}

// commitJobWrites queues the commit of what a background job wrote.
//
// A job writes through the vault exactly as an HTTP handler does, so
// commit-on-save has to see it too: without this, an import would land a
// hundred files in the working tree and leave the user to stage them by hand.
// It is a no-op when the feature is off, and it can never fail the job — the
// files are already on disk.
func (s *Server) commitJobWrites(ctx context.Context, m *mount, writes vault.WriteSet, fields gitops.Fields) {
	if s.git == nil || !s.git.enabled() || m == nil {
		return
	}
	paths := pathsOf(writes)
	if len(paths) == 0 {
		return
	}
	s.git.enqueue(ctx, gitops.Change{Repo: m.id, Paths: paths, Fields: fields})
}

// ------------------------------------------------------------- utilities ---

// docsFolderOf returns the documentation folder of one project inside a mount,
// "." when the project sits at the repository root and the mount's own folder
// when the project does not resolve.
func docsFolderOf(m *mount, project string) string {
	if m.ready() {
		for _, ref := range m.vlt.Projects() {
			if project == "" || string(ref.Key) == project {
				return ref.DocsPath
			}
		}
	}
	if m.docs != "" {
		return m.docs
	}
	return "."
}

// vaultPath joins a documentation folder and a path inside it into the
// vault-relative form the kb methods address pages by.
func vaultPath(docs, rel string) string {
	rel = strings.Trim(rel, "/")
	docs = strings.Trim(docs, "/")
	if docs == "" || docs == "." {
		return rel
	}
	if rel == "" {
		return docs
	}
	if rel == docs || strings.HasPrefix(rel, docs+"/") {
		return rel
	}
	return path.Join(docs, rel)
}

// dispatchVault calls one vault method on a mount and decodes the answer into
// T. It is the background-job equivalent of Server.call, which answers an HTTP
// request instead.
func dispatchVault(ctx context.Context, m *mount, method string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return terminalf("encode the parameters of %s: %w", method, err)
	}
	result, err := m.vlt.Dispatch(ctx, method, raw)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if out == nil {
		return nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode the answer of %s: %w", method, err)
	}
	if err := json.Unmarshal(encoded, out); err != nil {
		return fmt.Errorf("decode the answer of %s: %w", method, err)
	}
	return nil
}
