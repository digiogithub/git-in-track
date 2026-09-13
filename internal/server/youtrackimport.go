package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/syncengine"
	"github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// The `youtrack.import` job kind, story GIT-US-0050.
//
// Importing a query result is the one YouTrack operation that is unbounded by
// nature: a hundred issues are a hundred requests, each of them subject to the
// instance's rate limit, and a browser must not hold a request open across
// them. So the import runs here, in the background engine, and the endpoint
// that starts it answers with a job id.
//
// Three decisions shape this file:
//
//  1. **Paging is explicit and always ordered.** A $skip walk over a live
//     instance silently skips and duplicates rows unless the query pins a sort,
//     so every query goes through youtrack.ProjectQuery or
//     youtrack.EnsureOrderBy, both of which append `order by: created asc`.
//     Paging by hand rather than through SearchAllIssues is what lets the walk
//     stop at vault.YouTrackMaxIssues and observe cancellation between pages.
//
//  2. **One batch is one vault call and one commit.** The vault's import is
//     synchronous and produces one WriteSet per call, so batching is this
//     file's job. A cancelled import therefore keeps every batch that already
//     committed — which is the whole reason the batch size is small.
//
//  3. **A failed issue is not a failed job.** The vault records a per-issue
//     failure in its result and carries on; this handler accumulates those
//     across batches and reports them as progress. Only a failure of the batch
//     itself — a rejected token, an unreachable instance — fails the job.

// The shape of an import walk.
const (
	// importSearchPageSize is the $top of one search page. It is smaller than
	// the client's maximum on purpose: a cancelled import should stop within
	// one page, and a page is one request against the shared rate limit.
	importSearchPageSize = 100
	// defaultImportBatchSize is how many issues one vault call — and therefore
	// one commit — covers when the payload names no size of its own.
	defaultImportBatchSize = 20
	// maxImportBatchSize bounds a batch size a caller asks for, so that a
	// payload cannot turn the import back into one unbounded call.
	maxImportBatchSize = 100
)

// YouTrackImportParams is what one `youtrack.import` job is asked to do. It is
// the vault's own parameter shape minus the fields the envelope already
// carries, plus the batch size, so that a caller composes one object and this
// package splits it.
type YouTrackImportParams struct {
	// Query is a YouTrack issue query. Exactly one of Query and IDs is given;
	// with neither, every issue of the linked project is imported.
	Query string `json:"query,omitempty"`
	// IDs are readable issue ids such as "ACME-42".
	IDs []string `json:"ids,omitempty"`
	// Depth bounds the subtask recursion, 0 to vault.YouTrackMaxDepth.
	Depth int `json:"depth,omitempty"`
	// IncludeLinks imports the non-hierarchy relations as `links[]`.
	IncludeLinks bool `json:"includeLinks,omitempty"`
	// IncludeComments writes the issue's comment thread as comment files.
	IncludeComments bool `json:"includeComments,omitempty"`
	// IncludeAttachments records the attachment paths on the item and, unlike
	// the vault's own import, downloads the files (see downloadAttachments).
	IncludeAttachments bool `json:"includeAttachments,omitempty"`
	// BatchSize is how many issues one vault call covers. Zero means
	// defaultImportBatchSize.
	BatchSize int `json:"batchSize,omitempty"`
}

// YouTrackImportRequest is one queued import: where it runs and what it
// imports. It is the argument of [Server.EnqueueYouTrackImport], which is how
// every caller outside this package starts one.
type YouTrackImportRequest struct {
	// Repo is the mounted repository id. Empty resolves through Project.
	Repo string `json:"repo,omitempty"`
	// Project is the git-in-track project key the issues land in.
	Project string `json:"project,omitempty"`
	// Params is what to import.
	Params YouTrackImportParams `json:"params"`
}

// EnqueueYouTrackImport queues an import and returns the engine job id.
//
// The job is coalesced on the project: two imports asked for at once against
// one project are handed to the handler as one batch, which is what keeps a
// double-click from running the same walk twice. ctx is detached from the
// caller's, because an HTTP response that ends must not cancel the work it
// started.
func (s *Server) EnqueueYouTrackImport(ctx context.Context, req YouTrackImportRequest) (string, error) {
	envelope := youtrackJobPayload{Repo: req.Repo, Project: req.Project}
	return s.enqueueYouTrackJob(context.WithoutCancel(ctx), kindYouTrackImport,
		strings.TrimSpace(req.Project), envelope, req.Params)
}

// handleYouTrackImportJobs is the registered handler.
//
// Every job in the batch shares the kind and the coalescing key, so they are
// imports of one project: they run one after another rather than in parallel,
// because they write the same files and the rate limit is shared anyway.
func (s *Server) handleYouTrackImportJobs(ctx context.Context, batch []syncengine.Job) error {
	for _, job := range batch {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // cancellation must reach the engine unwrapped
		}
		if err := s.runYouTrackImport(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

// importRun is the running tally of one import, which is what the progress
// events and the final log line report.
type importRun struct {
	// total is how many issues the walk selected, done how many batches have
	// been committed for, and failed how many issues the vault could not write.
	total  int
	done   int
	failed int
	// lastID is the last issue the run touched, which is what a progress line
	// renders beside the bar.
	lastID string
}

// runYouTrackImport performs one import job.
func (s *Server) runYouTrackImport(ctx context.Context, job syncengine.Job) error {
	payload, err := decodeJobPayload(job)
	if err != nil {
		return err
	}
	params, err := decodeJobParams[YouTrackImportParams](payload)
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
	// The vault resolves its own client through the provider seam, which is
	// installed once in New. Reinstalling it here would race with another
	// mount's job for no gain, so the provider is trusted and only the client
	// this handler uses directly is resolved.

	ids, err := s.resolveImportIssues(ctx, client, link, params)
	if err != nil {
		return classifyYouTrackJobError(err)
	}

	run := &importRun{total: len(ids)}
	s.publishJobProgress(job, 0, run.total, 0, "")
	if run.total == 0 {
		return nil
	}

	size := importBatchSize(params.BatchSize, s.sync.current().BatchSize)
	for start := 0; start < len(ids); start += size {
		if err := ctx.Err(); err != nil {
			s.log.Info("the YouTrack import was cancelled",
				"job", job.ID, "project", project, "imported", run.done, "of", run.total)
			return err //nolint:wrapcheck // cancellation must reach the engine unwrapped
		}
		chunk := ids[start:min(start+size, len(ids))]
		if err := s.importBatch(ctx, job, m, client, project, params, chunk, run); err != nil {
			return classifyYouTrackJobError(err)
		}
	}
	s.log.Info("the YouTrack import finished",
		"job", job.ID, "project", project, "issues", run.done, "failed", run.failed)
	return nil
}

// importBatchSize picks the size of one vault call: what the payload asked for,
// then the engine's own batch size, then the shipped default, clamped.
func importBatchSize(requested, engine int) int {
	size := requested
	if size <= 0 {
		size = engine
	}
	if size <= 0 {
		size = defaultImportBatchSize
	}
	return min(size, maxImportBatchSize)
}

// importBatch imports one chunk of issues: one vault call, one commit, one
// progress event, and the attachments of everything it wrote.
func (s *Server) importBatch(
	ctx context.Context, job syncengine.Job, m *mount, client youtrackJobClient,
	project string, params YouTrackImportParams, chunk []string, run *importRun,
) error {
	result, err := m.vlt.YouTrackImportRun(ctx, vault.YouTrackImportParams{
		Project:            project,
		IDs:                chunk,
		Depth:              params.Depth,
		IncludeLinks:       params.IncludeLinks,
		IncludeComments:    params.IncludeComments,
		IncludeAttachments: params.IncludeAttachments,
	})
	if err != nil {
		return fmt.Errorf("import %d issues into %s: %w", len(chunk), project, err)
	}

	s.commitJobWrites(ctx, m, result.Writes, gitops.Fields{
		ItemID: project, Title: fmt.Sprintf("import %d issues from YouTrack", len(chunk)),
		Type: "import", Action: gitops.ActionUpdate,
	})

	// The batch is committed. From here on a failure must not undo it: the
	// attachments of an issue whose item already landed are downloaded
	// best-effort, and a cancellation stops the run with the batch intact.
	for _, issue := range result.Issues {
		if issue.Error != "" {
			run.failed++
			s.log.Warn("an issue could not be imported",
				"job", job.ID, "issue", issue.YouTrackID, "error", issue.Error)
			continue
		}
		run.lastID = issue.YouTrackID
	}
	run.done += len(chunk)
	s.publishJobProgress(job, run.done, run.total, run.failed, run.lastID)

	if !params.IncludeAttachments {
		return nil
	}
	return s.downloadBatchAttachments(ctx, m, client, project, result)
}

// downloadBatchAttachments fetches the binaries of everything one batch wrote.
//
// It runs after the commit on purpose: the item ids the files are filed under
// only exist once the batch has been written, and a download that fails must
// leave the imported item in place rather than rolling it back. A per-issue
// failure is logged and skipped; only cancellation stops the loop.
func (s *Server) downloadBatchAttachments(
	ctx context.Context, m *mount, client youtrackJobClient,
	project string, result vault.YouTrackImportResult,
) error {
	root := filepath.Join(m.path, filepath.FromSlash(docsFolderOf(m, project)),
		filepath.FromSlash(mapping.DefaultAttachmentPrefix))
	for _, issue := range result.Issues {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // cancellation must reach the engine unwrapped
		}
		if issue.Error != "" || issue.ItemID == "" {
			continue
		}
		if err := s.downloadAttachments(ctx, client, root, issue.YouTrackID, issue.ItemID); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			s.log.Warn("the attachments of an issue could not be downloaded",
				"issue", issue.YouTrackID, "item", issue.ItemID, "error", err)
		}
	}
	return nil
}

// resolveImportIssues turns the job parameters into the list of readable issue
// ids to import.
//
// An explicit id list is taken as given. A query is paged with $top and $skip
// over a query that always carries an ordering clause, because YouTrack is free
// to re-order between requests and an unordered $skip walk therefore skips and
// duplicates rows. The walk stops at vault.YouTrackMaxIssues, which is what
// keeps one query from turning into an unbounded walk of a tracker.
func (s *Server) resolveImportIssues(
	ctx context.Context, client youtrackJobClient, link vault.YouTrackLink, params YouTrackImportParams,
) ([]string, error) {
	if ids := trimIDs(params.IDs); len(ids) > 0 {
		if len(ids) > vault.YouTrackMaxIssues {
			s.log.Warn("the import id list was truncated",
				"asked", len(ids), "limit", vault.YouTrackMaxIssues)
			ids = ids[:vault.YouTrackMaxIssues]
		}
		return ids, nil
	}

	query := importQuery(link.Project, params.Query)
	page := youtrack.Page{Top: importSearchPageSize}
	out := make([]string, 0, importSearchPageSize)
	seen := make(map[string]bool, importSearchPageSize)
	for len(out) < vault.YouTrackMaxIssues {
		if err := ctx.Err(); err != nil {
			return nil, err //nolint:wrapcheck // cancellation must reach the engine unwrapped
		}
		issues, err := client.SearchIssues(ctx, query, page)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", query, err)
		}
		for _, issue := range issues {
			id := strings.TrimSpace(issue.IDReadable)
			if id == "" || seen[id] || len(out) == vault.YouTrackMaxIssues {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		if len(issues) < page.Top {
			break
		}
		page = page.Next()
	}
	return out, nil
}

// importQuery composes the query one import pages over.
//
// youtrack.ProjectQuery scopes an unscoped query to the linked project and
// appends the ordering clause; a query with no project to scope it to still
// goes through EnsureOrderBy, because the ordering is what makes the walk
// correct and it is never optional.
func importQuery(project, query string) string {
	if strings.TrimSpace(project) == "" {
		return youtrack.EnsureOrderBy(query)
	}
	return youtrack.ProjectQuery(project, query)
}

// trimIDs drops the blank entries of an id list.
func trimIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ----------------------------------------------------------- attachments ---

// downloadAttachments streams every file attached to one issue into the item's
// attachment folder, story GIT-T-0072.
//
// The vault's import records the paths on the item and warns that the bytes are
// somebody else's problem; this is that somebody. The rules are the ones a
// resumable download needs:
//
//   - a file that is already there at the size YouTrack reports is skipped, so
//     re-running an import — which retries, replays and dead-letter retries all
//     do — costs one listing and no transfers;
//   - the bytes go to a `.part` file carrying the process id and a timestamp,
//     so two companions importing the same issue cannot write the same
//     temporary file, and a partial download never looks complete;
//   - the rename happens only after the stream closed cleanly and the size on
//     disk matches, and the temporary file is removed on every other path,
//     cancellation included.
func (s *Server) downloadAttachments(
	ctx context.Context, client youtrackJobClient, root, issueID, itemID string,
) error {
	list, err := client.Attachments(ctx, issueID)
	if err != nil {
		return fmt.Errorf("list the attachments of %s: %w", issueID, err)
	}
	if len(list) == 0 {
		return nil
	}
	dir := filepath.Join(root, itemID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for _, attachment := range list {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // cancellation must reach the engine unwrapped
		}
		name := attachmentFileName(attachment.Name)
		if name == "" {
			continue
		}
		if err := s.downloadAttachment(ctx, client, filepath.Join(dir, name), attachment); err != nil {
			return err
		}
	}
	return nil
}

// attachmentFileName reduces the name YouTrack reports to a plain file name.
// A name carrying a separator, or naming a parent directory, would write
// outside the item's folder, so only the base name is ever used.
func attachmentFileName(raw string) string {
	name := filepath.Base(strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/")))
	switch name {
	case "", ".", "..", string(filepath.Separator):
		return ""
	}
	return name
}

// downloadAttachment fetches one file, skipping it when it is already present
// at the expected size.
func (s *Server) downloadAttachment(
	ctx context.Context, client youtrackJobClient, target string, attachment youtrack.Attachment,
) error {
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
		if attachment.Size <= 0 || info.Size() == attachment.Size {
			return nil
		}
	}

	body, err := client.DownloadAttachment(ctx, attachment)
	if err != nil {
		return fmt.Errorf("download %s of %s: %w", attachment.Name, attachment.ID, err)
	}
	defer func() { _ = body.Close() }()

	tmp := partFileName(target, s.now())
	written, err := writePartFile(tmp, body)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if attachment.Size > 0 && written != attachment.Size {
		_ = os.Remove(tmp)
		return fmt.Errorf("download %s: %d bytes arrived, YouTrack reported %d",
			attachment.Name, written, attachment.Size)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install %s: %w", target, err)
	}
	return nil
}

// partFileName is the temporary name a download streams to:
// `<name>.<pid>.<base36 timestamp>.part`. The process id and the timestamp
// together are what make two companions — or two attempts of one — unable to
// collide on the same temporary file.
func partFileName(target string, now time.Time) string {
	return fmt.Sprintf("%s.%d.%s.part", target, os.Getpid(), strconv.FormatInt(now.UnixNano(), 36))
}

// writePartFile streams a body into a temporary file and reports how many bytes
// landed. The file is closed before the caller renames it: a rename over an
// open handle is a Windows failure waiting to happen.
func writePartFile(path string, body io.Reader) (int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec // the name is derived from the target
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", path, err)
	}
	written, copyErr := io.Copy(file, body)
	closeErr := file.Close()
	if copyErr != nil {
		return written, fmt.Errorf("write %s: %w", path, copyErr)
	}
	if closeErr != nil {
		return written, fmt.Errorf("write %s: %w", path, closeErr)
	}
	return written, nil
}
