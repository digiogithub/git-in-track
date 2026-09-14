package server

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/syncengine"
	"github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// The `youtrack.kb.publish` and `youtrack.kb.pull` job kinds, story
// GIT-US-0087.
//
// A handbook is hundreds of pages and therefore hundreds of requests, so both
// directions run in the background engine and the call that starts them answers
// with a job id. What the two share is the hard part — deciding who changed —
// and it is decided the same way in both directions:
//
//   - the page's `external` entry records the fingerprint of the content that
//     was last published, in `key`, and when it was, in `synced_at`;
//   - the local side changed when the fingerprint of the content the page would
//     publish now differs from the recorded one;
//   - the remote side changed when the fingerprint of the article's content
//     differs from it too;
//   - one side changed, that side wins; both changed, nobody wins: the incoming
//     content is written to `<page>.conflict.md`, the page is left exactly as
//     it is, and `youtrack.kb.conflict` says so. There is no three-way merge
//     and there is deliberately none.
//
// The fingerprint is taken over mapping.PageToArticle's output, never over raw
// bytes: core.PruneKbFeedback rewrites the `## Feedback` block on every write
// and the block never leaves the repository (ADR-030), so a byte comparison
// would report a remote edit every time somebody adds a note locally. That is
// also why mapping.EqualContent, not `==`, decides whether a write is needed at
// all.

// eventYouTrackKBConflict is published when a page and its article both moved.
const eventYouTrackKBConflict = "youtrack.kb.conflict"

// conflictSuffix is appended to a page's path to hold incoming content that
// could not be applied.
const conflictSuffix = ".conflict.md"

// kbConflictEventData is the payload of `youtrack.kb.conflict`.
type kbConflictEventData struct {
	// Project is the git-in-track project key, Path the page that diverged and
	// ConflictPath the file the incoming content was written to.
	Project      string `json:"project"`
	Path         string `json:"path"`
	ConflictPath string `json:"conflictPath"`
	// ArticleID is the YouTrack article the page mirrors, and Direction the job
	// that found the divergence: `publish` or `pull`.
	ArticleID string `json:"articleId,omitempty"`
	Direction string `json:"direction"`
}

// The two directions, as they appear in the conflict event.
const (
	kbDirectionPublish = "publish"
	kbDirectionPull    = "pull"
)

// YouTrackKBRequest is one queued publish or pull.
type YouTrackKBRequest struct {
	Repo    string                 `json:"repo,omitempty"`
	Project string                 `json:"project,omitempty"`
	Params  vault.YouTrackKBParams `json:"params"`
}

// EnqueueYouTrackKBPublish queues the publication of a page or a folder.
func (s *Server) EnqueueYouTrackKBPublish(ctx context.Context, req YouTrackKBRequest) (string, error) {
	return s.enqueueKB(ctx, kindYouTrackKBPublish, req)
}

// EnqueueYouTrackKBPull queues writing a page or a folder back from its
// articles.
func (s *Server) EnqueueYouTrackKBPull(ctx context.Context, req YouTrackKBRequest) (string, error) {
	return s.enqueueKB(ctx, kindYouTrackKBPull, req)
}

// enqueueKB is the shared half. The coalescing key is the selection itself, so
// two clicks on one folder queue one job and two folders queue two.
func (s *Server) enqueueKB(ctx context.Context, kind syncengine.Kind, req YouTrackKBRequest) (string, error) {
	key := req.Project + ":" + strings.Trim(req.Params.Path, "/")
	if req.Params.Recursive {
		key += ":recursive"
	}
	envelope := youtrackJobPayload{Repo: req.Repo, Project: req.Project}
	return s.enqueueYouTrackJob(context.WithoutCancel(ctx), kind, key, envelope, req.Params)
}

// handleYouTrackKBPublishJobs is the registered publish handler.
func (s *Server) handleYouTrackKBPublishJobs(ctx context.Context, batch []syncengine.Job) error {
	return s.runKBJobs(ctx, batch, kbDirectionPublish)
}

// handleYouTrackKBPullJobs is the registered pull handler.
func (s *Server) handleYouTrackKBPullJobs(ctx context.Context, batch []syncengine.Job) error {
	return s.runKBJobs(ctx, batch, kbDirectionPull)
}

// runKBJobs walks a batch, which always shares one selection, in order.
func (s *Server) runKBJobs(ctx context.Context, batch []syncengine.Job, direction string) error {
	for _, job := range batch {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // cancellation must reach the engine unwrapped
		}
		if err := s.runKBJob(ctx, job, direction); err != nil {
			return err
		}
	}
	return nil
}

// kbRun is one publish or pull in flight: everything the handlers need that is
// not the job itself.
type kbRun struct {
	job       syncengine.Job
	mount     *mount
	client    youtrackJobClient
	link      vault.YouTrackLink
	project   string
	docs      string
	direction string
	// parents memoizes the article id of each directory of the selection, so a
	// folder publish creates one parent article per directory rather than one
	// per page.
	parents map[string]string
	// failed counts the pages that could not be synchronized. A page failure is
	// reported and skipped: one unreadable article must not abandon the rest of
	// a handbook.
	failed int
	// projectID memoizes the internal entity id of the linked project, which is
	// what creating an article needs; it is resolved on the first create of a
	// job and never for a job that only updates.
	projectID string
}

// entityProjectID resolves the linked project's internal entity id.
//
// A project is configured by its short name — the "ACME" of ACME-42 — and every
// read is scoped by it, but `POST /api/articles` rejects it outright with
// "Invalid structure of entity id": the project of a new article has to be the
// `0-17` form. The settings picker records that id when the project is chosen
// from the instance, so the usual path costs nothing; a link written by hand,
// or by a build older than the key, is resolved from the short name once per
// job, and only for a job that actually creates something.
func (s *Server) entityProjectID(ctx context.Context, run *kbRun) (string, error) {
	if run.projectID == "" {
		run.projectID = run.link.ProjectID
	}
	if run.projectID != "" {
		return run.projectID, nil
	}
	id, err := run.client.ProjectID(ctx, run.link.Project)
	if err != nil {
		return "", fmt.Errorf("resolve the YouTrack project %s: %w", run.link.Project, err)
	}
	run.projectID = id
	return id, nil
}

// runKBJob performs one publish or pull.
func (s *Server) runKBJob(ctx context.Context, job syncengine.Job, direction string) error {
	payload, err := decodeJobPayload(job)
	if err != nil {
		return err
	}
	params, err := decodeJobParams[vault.YouTrackKBParams](payload)
	if err != nil {
		return err
	}
	m, project, err := s.jobMount(payload)
	if err != nil {
		return err
	}
	client, link, err := s.jobClient(project)
	if err != nil {
		return classifyYouTrackJobError(fmt.Errorf("resolve the YouTrack client of %s: %w", project, err))
	}

	run := &kbRun{
		job: job, mount: m, client: client, link: link, project: project,
		docs: docsFolderOf(m, project), direction: direction, parents: map[string]string{},
	}
	pages, err := s.selectKBPages(ctx, run, params)
	if err != nil {
		return err
	}

	s.publishJobProgress(job, 0, len(pages), 0, "")
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			s.log.Info("the knowledge-base job was cancelled",
				"job", job.ID, "direction", direction, "done", i, "of", len(pages))
			return err //nolint:wrapcheck // cancellation must reach the engine unwrapped
		}
		if err := s.runKBPage(ctx, run, page); err != nil {
			if isTerminalKBFailure(err) {
				return err
			}
			run.failed++
			s.log.Warn("a knowledge-base page could not be synchronized",
				"job", job.ID, "direction", direction, "page", page, "error", err)
		}
		s.publishJobProgress(job, i+1, len(pages), run.failed, page)
	}
	s.log.Info("the knowledge-base job finished",
		"job", job.ID, "direction", direction, "pages", len(pages), "failed", run.failed)
	return nil
}

// isTerminalKBFailure reports whether a page failure must stop the whole job.
//
// A rejected token or a revoked permission applies to every remaining page, so
// carrying on would be a hundred more failures and a hundred more requests; a
// single page that will not parse applies only to itself.
func isTerminalKBFailure(err error) bool {
	return syncengine.Classify(classifyYouTrackJobError(err), core.Timestamp{}.Time).Class ==
		syncengine.ClassTerminal
}

// runKBPage synchronizes one page in the job's direction.
func (s *Server) runKBPage(ctx context.Context, run *kbRun, pagePath string) error {
	page, rev, front, err := s.readKBPage(ctx, run, pagePath)
	if err != nil {
		return err
	}
	if run.direction == kbDirectionPublish {
		return s.publishKBPage(ctx, run, page, rev, front)
	}
	return s.pullKBPage(ctx, run, page, rev, front)
}

// ----------------------------------------------------------- the publish ---

// publishKBPage creates or updates the article one page mirrors.
//
// An unlinked page is created with `project` in the body, because YouTrack only
// accepts a project at creation and an article can never be moved afterwards. A
// linked page is updated. Either way the article id and url are written back
// into the page's front matter, rev-guarded, so the next run knows which case
// it is in.
func (s *Server) publishKBPage(
	ctx context.Context, run *kbRun, page core.KBPage, rev string, front map[string]any,
) error {
	opts := run.pageOptions()
	payload, warnings := mapping.PageToArticle(page, opts)
	for _, w := range warnings {
		s.log.Warn("the page could not be published verbatim", "page", page.Path, "warning", w.String())
	}
	if strings.TrimSpace(payload.Summary) == "" {
		return terminalf(
			"%s has neither a title nor a leading heading, and an article cannot be created without a summary",
			page.Path)
	}
	if len(payload.Attachments) > 0 {
		// Uploading a page's local files is not part of this build's client
		// (see the report on GIT-T-0208); the content already refers to them by
		// name, so the article is published and the gap is reported rather than
		// silently producing broken images.
		s.log.Warn("the page references local files this build cannot upload",
			"page", page.Path, "attachments", len(payload.Attachments))
	}

	parent, err := s.ensureKBParent(ctx, run, path.Dir(page.Path))
	if err != nil {
		return err
	}

	ref, linked := youtrackExternalOf(page.ExternalRefs)
	input := payload.Input()
	if parent != "" {
		input.ParentArticleID = &parent
	}

	if !linked {
		projectID, err := s.entityProjectID(ctx, run)
		if err != nil {
			return err
		}
		input.ProjectID = projectID
		created, err := run.client.CreateArticle(ctx, input)
		if err != nil {
			return fmt.Errorf("create an article for %s: %w", page.Path, err)
		}
		return s.writeKBExternal(ctx, run, page, rev, front, created)
	}

	article, err := run.client.Article(ctx, ref.ID)
	if err != nil {
		return fmt.Errorf("read article %s of %s: %w", ref.ID, page.Path, err)
	}
	state := decideKBState(payload, article, ref, page)
	switch state {
	case kbStateInSync:
		// Nothing changed on either side. The reference is still refreshed, so
		// that `synced_at` records that the two were checked and found equal.
		return s.writeKBExternal(ctx, run, page, rev, front, article)
	case kbStateRemoteAhead:
		s.log.Info("the article is ahead of the page; the publish skipped it",
			"page", page.Path, "article", ref.ID)
		return nil
	case kbStateConflict:
		return s.writeKBConflict(ctx, run, page, article)
	case kbStateLocalAhead:
	}

	updated, err := run.client.UpdateArticle(ctx, ref.ID, input)
	if err != nil {
		return fmt.Errorf("update article %s of %s: %w", ref.ID, page.Path, err)
	}
	if updated.IDReadable == "" && updated.ID == "" {
		// An instance that answers an update with an empty body still told us
		// which article it was.
		updated = article
	}
	return s.writeKBExternal(ctx, run, page, rev, front, updated)
}

// ensureKBParent returns the article every page of one directory hangs under,
// creating the chain of parent articles down to it.
//
// Parents are always created before their children, which is what makes the
// remote hierarchy mirror the local one. Ordering inside a directory comes from
// the order pages are published in and nothing else: `ordinal` is documented
// read-only and is silently ignored when sent, so relying on it would be a
// silent no-op.
//
// A re-publish reuses what is there: the directory's article is looked up among
// the children of its own parent by summary, and at the top level through a
// project-scoped, explicitly ordered article search.
func (s *Server) ensureKBParent(ctx context.Context, run *kbRun, dir string) (string, error) {
	dir = strings.Trim(path.Clean(dir), "/")
	root := strings.Trim(run.docs, "/")
	if dir == "" || dir == "." || dir == root || !strings.HasPrefix(dir+"/", root+"/") {
		return "", nil
	}
	if id, ok := run.parents[dir]; ok {
		return id, nil
	}
	parent, err := s.ensureKBParent(ctx, run, path.Dir(dir))
	if err != nil {
		return "", err
	}
	summary := path.Base(dir)

	id, err := s.findKBParent(ctx, run, parent, summary)
	if err != nil {
		return "", err
	}
	if id == "" {
		projectID, idErr := s.entityProjectID(ctx, run)
		if idErr != nil {
			return "", idErr
		}
		content := ""
		input := youtrack.ArticleInput{
			Summary: &summary, Content: &content, ProjectID: projectID,
		}
		if parent != "" {
			input.ParentArticleID = &parent
		}
		created, createErr := run.client.CreateArticle(ctx, input)
		if createErr != nil {
			return "", fmt.Errorf("create the parent article of %s: %w", dir, createErr)
		}
		id = articleID(created)
	}
	run.parents[dir] = id
	return id, nil
}

// findKBParent looks for an existing parent article of a directory.
func (s *Server) findKBParent(ctx context.Context, run *kbRun, parent, summary string) (string, error) {
	if parent != "" {
		children, err := run.client.ChildArticles(ctx, parent)
		if err != nil {
			return "", fmt.Errorf("list the children of article %s: %w", parent, err)
		}
		for _, child := range children {
			if strings.EqualFold(strings.TrimSpace(child.Summary), summary) {
				return firstNonEmpty(child.IDReadable, child.ID), nil
			}
		}
		return "", nil
	}
	// A top-level folder has no parent to list, so the instance is searched.
	// The query is project-scoped and ordered: an article listing that pages
	// without an ordering clause skips and duplicates rows, which is the latent
	// bug the reference CLI carries and this must not copy.
	query := youtrack.ProjectQuery(run.link.Project, "summary: {"+summary+"}")
	articles, err := run.client.SearchArticles(ctx, query, youtrack.Page{Top: 50})
	if err != nil {
		return "", fmt.Errorf("search for the parent article %q: %w", summary, err)
	}
	for _, article := range articles {
		if strings.EqualFold(strings.TrimSpace(article.Summary), summary) &&
			article.ParentArticle.ID == "" {
			return articleID(article), nil
		}
	}
	return "", nil
}

// -------------------------------------------------------------- the pull ---

// pullKBPage writes one article back into the page it mirrors.
func (s *Server) pullKBPage(
	ctx context.Context, run *kbRun, page core.KBPage, rev string, front map[string]any,
) error {
	ref, linked := youtrackExternalOf(page.ExternalRefs)
	if !linked {
		s.log.Info("the page mirrors no article, so there is nothing to pull", "page", page.Path)
		return nil
	}
	article, err := run.client.Article(ctx, ref.ID)
	if err != nil {
		return fmt.Errorf("read article %s of %s: %w", ref.ID, page.Path, err)
	}

	opts := run.pageOptions()
	payload, _ := mapping.PageToArticle(page, opts)
	switch decideKBState(payload, article, ref, page) {
	case kbStateInSync, kbStateLocalAhead:
		// Nothing to write: either the two agree, or the local side is the one
		// that moved and a pull must not undo it.
		return nil
	case kbStateConflict:
		return s.writeKBConflict(ctx, run, page, article)
	case kbStateRemoteAhead:
	}

	body, updated, warnings := mapping.ArticleToPage(article, page, opts)
	for _, w := range warnings {
		s.log.Warn("the article could not be written back verbatim",
			"page", page.Path, "warning", w.String())
	}
	updated = mergeFrontMatter(front, updated)
	// The reference is rewritten rather than taken from the transform: the
	// fingerprint of what has just been synchronized, and the moment it was,
	// are what let the next run tell a local edit from a remote one, and the
	// mapping layer reads no clock and computes no fingerprint.
	updated["external"] = externalsToFrontMatter(upsertExternal(page.ExternalRefs, core.NormalizeExternal(core.External{
		System:   mapping.System,
		ID:       articleID(article),
		URL:      articleURL(run.link.BaseURL, articleID(article)),
		Key:      kbFingerprintOf(article.Content),
		SyncedAt: core.NewTimestamp(s.now()),
	})))
	return s.writeKBPage(ctx, run, page.Path, rev, updated, body)
}

// ------------------------------------------------------- change detection ---

// The four outcomes of comparing a page with the article it mirrors.
const (
	kbStateInSync = iota
	kbStateLocalAhead
	kbStateRemoteAhead
	kbStateConflict
)

// decideKBState decides who changed since the last synchronization.
//
// The recorded fingerprint is the pivot: it was taken over the content that was
// published, so both sides are compared against it rather than against each
// other, which is what makes "both changed" distinguishable from "one changed".
// A page that carries no fingerprint — published by an older build, or linked
// by hand — falls back to the timestamps, which is all there is.
func decideKBState(payload mapping.ArticlePayload, article youtrack.Article, ref core.External, page core.KBPage) int {
	local := kbFingerprintOf(payload.Content)
	remote := kbFingerprintOf(article.Content)

	if mapping.EqualContent(payload.Content, article.Content) &&
		strings.TrimSpace(payload.Summary) == strings.TrimSpace(article.Summary) {
		return kbStateInSync
	}

	localChanged, remoteChanged := local != ref.Key, remote != ref.Key
	if ref.Key == "" {
		localChanged = !page.Updated.IsZero() && page.Updated.After(ref.SyncedAt.Time)
		remoteChanged = article.Updated.Time().After(ref.SyncedAt.Time)
	}
	switch {
	case localChanged && remoteChanged:
		return kbStateConflict
	case localChanged:
		return kbStateLocalAhead
	case remoteChanged:
		return kbStateRemoteAhead
	default:
		// The two differ and neither side looks touched. The safe answer is the
		// one that overwrites nothing.
		return kbStateConflict
	}
}

// kbFingerprintOf is the fingerprint recorded in an external entry's `key`. It
// is taken over the same normalized content internal/vault fingerprints, so the
// two agree about whether a page has moved.
func kbFingerprintOf(content string) string {
	return string(core.ComputeRev([]byte(strings.TrimSpace(content))))
}

// writeKBConflict records incoming content that could not be applied and says
// so on the hub.
//
// The page itself is never touched: a divergence is a decision for a human, and
// the worst possible answer is a silent merge. The conflict file is a normal
// page with the article's content and a note saying where it came from.
func (s *Server) writeKBConflict(
	ctx context.Context, run *kbRun, page core.KBPage, article youtrack.Article,
) error {
	target := strings.TrimSuffix(page.Path, ".md") + conflictSuffix
	body, front, _ := mapping.ArticleToPage(article, core.KBPage{Path: target}, run.pageOptions())
	if front == nil {
		front = map[string]any{}
	}
	front["title"] = firstNonEmpty(strings.TrimSpace(article.Summary), page.Title)
	front["conflict_of"] = page.Path

	if err := s.writeKBPage(ctx, run, target, "", front, body); err != nil {
		return fmt.Errorf("write the conflict page of %s: %w", page.Path, err)
	}
	if s.hub != nil {
		s.hub.Publish(eventYouTrackKBConflict, kbConflictEventData{
			Project: run.project, Path: page.Path, ConflictPath: target,
			ArticleID: articleID(article), Direction: run.direction,
		})
	}
	s.log.Warn("a knowledge-base page and its article both changed",
		"page", page.Path, "conflict", target, "article", articleID(article))
	return nil
}

// --------------------------------------------------------------- the vault ---

// readKBPage reads one page through the vault and rebuilds the core.KBPage the
// mapping layer takes, together with the rev a write must quote and the front
// matter as it is on disk.
func (s *Server) readKBPage(
	ctx context.Context, run *kbRun, pagePath string,
) (page core.KBPage, rev string, front map[string]any, err error) {
	var result struct {
		Path        string         `json:"path"`
		Title       string         `json:"title"`
		FrontMatter map[string]any `json:"frontMatter"`
		Body        string         `json:"body"`
		Rev         string         `json:"rev"`
		Project     string         `json:"project"`
		RelPath     string         `json:"relPath"`
	}
	if err := dispatchVault(ctx, run.mount, "kb.page", map[string]any{"path": pagePath}, &result); err != nil {
		return core.KBPage{}, "", nil, fmt.Errorf("read page %s: %w", pagePath, err)
	}
	page = core.KBPage{
		Path: result.Path, RelPath: result.RelPath, Title: result.Title,
		FrontMatter: result.FrontMatter, Body: result.Body,
		Project: core.ProjectKey(result.Project), Rev: core.Rev(result.Rev),
		ExternalRefs: externalsFromFrontMatter(result.FrontMatter),
	}
	// The indexed body has the feedback block removed — it is local-only and
	// never leaves the repository (ADR-030) — but a pull has to put it back
	// verbatim, so the body a transform runs against is the one on disk.
	if body, ok := rawPageBody(filepath.Join(run.mount.path, filepath.FromSlash(result.Path))); ok {
		page.Body = body
	}
	if raw, ok := result.FrontMatter["updated"].(string); ok {
		if at, err := core.ParseTimestamp(raw); err == nil {
			page.Updated = at
		}
	}
	return page, result.Rev, result.FrontMatter, nil
}

// rawPageBody reads the body of a page from disk, front matter removed and
// feedback block included. It reports false for a file it cannot read, which
// leaves the indexed body in place.
func rawPageBody(file string) (string, bool) {
	data, err := os.ReadFile(file) //nolint:gosec // the path is inside the mounted repository
	if err != nil {
		return "", false
	}
	_, body, err := core.SplitFrontMatter(data)
	if err != nil {
		return "", false
	}
	return body, true
}

// writeKBPage renders a page and writes it through the vault, so that
// WritePage, its feedback pruning and the commit pipeline all apply normally.
//
// rev is the optimistic lock: the one the page was read at for an update, empty
// for a file that does not exist yet. A stale rev is reported, never forced.
func (s *Server) writeKBPage(
	ctx context.Context, run *kbRun, pagePath, rev string, front map[string]any, body string,
) error {
	text, err := renderKBPage(filepath.Join(run.mount.path, filepath.FromSlash(pagePath)), front, body)
	if err != nil {
		return err
	}
	params := map[string]any{"path": pagePath, "text": text}
	if rev != "" {
		params["rev"] = rev
	}
	var result struct {
		Writes vault.WriteSet `json:"writes"`
	}
	if err := dispatchVault(ctx, run.mount, "kb.write", params, &result); err != nil {
		return fmt.Errorf("write page %s: %w", pagePath, err)
	}
	s.commitJobWrites(ctx, run.mount, result.Writes, gitops.Fields{
		ItemID: pagePath, Title: pagePath, Type: "page", Action: gitops.ActionUpdate,
	})
	return nil
}

// writeKBExternal records the article a page now mirrors, with the fingerprint
// of the content that was published and the moment it was.
//
// Writing the fingerprint is what makes the next run able to tell a local edit
// from a remote one; without it, change detection falls back to timestamps and
// a feedback note looks like a content change.
func (s *Server) writeKBExternal(
	ctx context.Context, run *kbRun, page core.KBPage, rev string, front map[string]any, article youtrack.Article,
) error {
	payload, _ := mapping.PageToArticle(page, run.pageOptions())
	ref := core.NormalizeExternal(core.External{
		System:   mapping.System,
		ID:       articleID(article),
		URL:      articleURL(run.link.BaseURL, articleID(article)),
		Key:      kbFingerprintOf(payload.Content),
		SyncedAt: core.NewTimestamp(s.now()),
	})
	if !ref.Valid() {
		return fmt.Errorf("the instance answered without an article id for %s", page.Path)
	}
	updated := mergeFrontMatter(front, nil)
	updated["external"] = externalsToFrontMatter(upsertExternal(page.ExternalRefs, ref))
	return s.writeKBPage(ctx, run, page.Path, rev, updated, page.Body)
}

// --------------------------------------------------------------- selection ---

// selectKBPages lists the pages one job covers, parents before children and in
// index order, which is the order a folder publish has to follow.
func (s *Server) selectKBPages(ctx context.Context, run *kbRun, params vault.YouTrackKBParams) ([]string, error) {
	target := strings.Trim(strings.TrimSpace(params.Path), "/")
	if strings.HasSuffix(target, ".md") {
		return []string{vaultPath(run.docs, target)}, nil
	}
	var tree struct {
		Nodes []kbTreeNode `json:"nodes"`
	}
	var nodes []kbTreeNode
	if err := dispatchVault(ctx, run.mount, "kb.tree", map[string]any{"project": run.project}, &tree); err == nil {
		nodes = tree.Nodes
	}
	if len(nodes) == 0 {
		// The tree is served either as {"nodes": […]} or as a bare forest,
		// depending on the vault build; both are accepted rather than pinning
		// this handler to one of them.
		if err := dispatchVault(ctx, run.mount, "kb.tree", map[string]any{"project": run.project}, &nodes); err != nil {
			return nil, fmt.Errorf("list the knowledge base of %s: %w", run.project, err)
		}
	}

	root := vaultPath(run.docs, target)
	out := make([]string, 0, 16)
	for _, node := range nodes {
		collectKBPages(node, root, params.Recursive, &out)
	}
	sort.Strings(out)
	return out, nil
}

// kbTreeNode is one node of the knowledge-base forest the vault serves.
type kbTreeNode struct {
	Path     string       `json:"path"`
	Name     string       `json:"name"`
	Kind     string       `json:"kind"`
	Title    string       `json:"title,omitempty"`
	Children []kbTreeNode `json:"children,omitempty"`
}

// collectKBPages appends the pages of one node that fall inside the selection.
//
// A non-recursive folder answers for its direct pages only, which is the same
// rule the vault's own selection applies.
func collectKBPages(node kbTreeNode, root string, recursive bool, out *[]string) {
	if node.Kind == "page" {
		if inKBSelection(node.Path, root, recursive) {
			*out = append(*out, node.Path)
		}
		return
	}
	for _, child := range node.Children {
		collectKBPages(child, root, recursive, out)
	}
}

// inKBSelection reports whether a page path belongs to a selection.
func inKBSelection(pagePath, root string, recursive bool) bool {
	pagePath = strings.Trim(pagePath, "/")
	if root == "" {
		return recursive || !strings.Contains(pagePath, "/")
	}
	if !strings.HasPrefix(pagePath, root+"/") {
		return false
	}
	if recursive {
		return true
	}
	return !strings.Contains(strings.TrimPrefix(pagePath, root+"/"), "/")
}

// ---------------------------------------------------------------- helpers ---

// pageOptions is what the mapping layer needs beyond the page: the instance
// base URL and the stamp an external reference records.
func (r *kbRun) pageOptions() mapping.PageOptions {
	return mapping.PageOptions{Options: mapping.Options{BaseURL: r.link.BaseURL}}
}

// articleID is the identifier an external reference records: the readable one
// when the instance sent it, because that is what a human recognizes and what a
// URL carries.
func articleID(article youtrack.Article) string {
	return firstNonEmpty(strings.TrimSpace(article.IDReadable), strings.TrimSpace(article.ID))
}

// articleURL builds the URL of an article on one instance.
func articleURL(baseURL, id string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" || id == "" {
		return ""
	}
	return base + "/article/" + id
}

// firstNonEmpty returns the first value that is not blank.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// mergeFrontMatter copies base and layers overlay on top of it, so that a
// transform that rewrote two keys keeps every other key of the file.
func mergeFrontMatter(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay)+1)
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// externalsFromFrontMatter reads the typed `external:` entries of a page's
// front matter. The vault hands the front matter back as plain JSON, so the
// list arrives as maps and is decoded here rather than re-read from disk.
func externalsFromFrontMatter(front map[string]any) []core.External {
	raw, ok := front["external"].([]any)
	if !ok {
		return nil
	}
	out := make([]core.External, 0, len(raw))
	for _, entry := range raw {
		fields, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		ref := core.External{
			System: stringValue(fields["system"]),
			ID:     stringValue(fields["id"]),
			URL:    stringValue(fields["url"]),
			Key:    stringValue(fields["key"]),
		}
		if at, err := core.ParseTimestamp(stringValue(fields["synced_at"])); err == nil {
			ref.SyncedAt = at
		}
		if ref.Valid() {
			out = append(out, core.NormalizeExternal(ref))
		}
	}
	return out
}

// externalsToFrontMatter renders an external list back as the sequence of
// mappings core reads, with the canonical key order.
func externalsToFrontMatter(refs []core.External) []any {
	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		ref = core.NormalizeExternal(ref)
		entry := map[string]any{"system": ref.System, "id": ref.ID}
		if ref.URL != "" {
			entry["url"] = ref.URL
		}
		if ref.Key != "" {
			entry["key"] = ref.Key
		}
		if !ref.SyncedAt.IsZero() {
			entry["synced_at"] = ref.SyncedAt.String()
		}
		out = append(out, entry)
	}
	return out
}

// stringValue reads a front-matter value as a string, returning "" for
// anything else.
func stringValue(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// renderKBPage assembles the bytes of a page: the front matter, then the body.
//
// The key order of the existing file is preserved, because a publish that
// reordered every key would turn a one-line change into a whole-file diff in
// every review. The original block is read from disk for its order alone; the
// values written are the caller's.
func renderKBPage(file string, front map[string]any, body string) (string, error) {
	node, err := frontMatterNode(file, front)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if len(node.Content) > 0 {
		encoded, err := yaml.Marshal(node)
		if err != nil {
			return "", fmt.Errorf("encode the front matter of %s: %w", file, err)
		}
		out.WriteString("---\n")
		out.Write(encoded)
		out.WriteString("---\n\n")
	}
	// A page that carried no front matter and gained no key keeps none: adding
	// an empty block would be a diff in every page of a handbook.
	if trimmed := strings.Trim(body, "\n"); trimmed != "" {
		out.WriteString(trimmed)
		out.WriteString("\n")
	}
	return out.String(), nil
}

// frontMatterNode builds the YAML mapping a page is written with: the keys the
// file already had, in the order it had them, then the new ones sorted.
func frontMatterNode(file string, front map[string]any) (*yaml.Node, error) {
	order := frontMatterOrder(file)
	node := &yaml.Node{Kind: yaml.MappingNode}
	written := make(map[string]bool, len(front))
	appendKey := func(key string) error {
		value, ok := front[key]
		if !ok || written[key] {
			return nil
		}
		written[key] = true
		encoded := &yaml.Node{}
		if err := encoded.Encode(value); err != nil {
			return fmt.Errorf("encode the front-matter key %q: %w", key, err)
		}
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key}, encoded)
		return nil
	}
	for _, key := range order {
		if err := appendKey(key); err != nil {
			return nil, err
		}
	}
	rest := make([]string, 0, len(front))
	for key := range front {
		if !written[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		if err := appendKey(key); err != nil {
			return nil, err
		}
	}
	return node, nil
}

// frontMatterOrder reads the key order of a file's existing front matter. A
// file that is not there — a conflict page being created — has no order, which
// is fine: everything is then written sorted.
func frontMatterOrder(file string) []string {
	data, err := os.ReadFile(file) //nolint:gosec // the path is inside the mounted repository
	if err != nil {
		return nil
	}
	block, _, err := core.SplitFrontMatter(data)
	if err != nil {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(block, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	mappingNode := doc.Content[0]
	if mappingNode.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]string, 0, len(mappingNode.Content)/2)
	for i := 0; i+1 < len(mappingNode.Content); i += 2 {
		out = append(out, mappingNode.Content[i].Value)
	}
	return out
}
