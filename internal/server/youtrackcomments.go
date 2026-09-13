package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/syncengine"
	"github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// The `youtrack.comment.push` job kind, story GIT-US-0068.
//
// A comment saved in the web app must not wait on a tracker, so the push never
// happens in the write path: the vault decides a push is needed and hands a job
// to the engine, and this is what the engine runs.
//
// The idempotence of this handler is the whole point of the `external` field on
// core.Comment. The engine re-delivers a job after a retryable error, after a
// journal replay and when a user retries it from the dead-letter list, so a
// handler that simply posted would leave three copies of one comment on the
// issue. Instead:
//
//   - a comment with no YouTrack `external` entry is created remotely, and the
//     id that comes back is written into the comment before the job is done;
//   - a comment that already carries one is *edited*, so the second delivery
//     updates the same remote comment rather than adding another;
//   - a comment deleted locally is never deleted remotely. There is no job for
//     it and there is deliberately none: a repository is not the authority on
//     an issue's conversation, and a mistaken `rm` must not erase a thread
//     other people are reading. The divergence is documented in docs/07.

// defaultCommentAttribution is the trailing line appended to every pushed
// comment when the project declares no template of its own.
//
// The item id is emitted bare. YouTrack auto-links anything shaped like an
// issue id, and the git-in-track id is *not* one, so wrapping it in a Markdown
// link would produce a dead link; leaving it bare keeps it copyable and lets
// YouTrack's own linking apply where it applies.
const defaultCommentAttribution = "\n\n---\n_{{.Author}} · git-in-track {{.ItemID}}_"

// commentAttribution is what a template renders against.
type commentAttribution struct {
	// Author is the comment's author handle, and AuthorName the git identity
	// when the file recorded one.
	Author     string
	AuthorName string
	// ItemID is the git-in-track item the comment belongs to, and IssueID the
	// YouTrack issue it is being pushed to.
	ItemID  string
	IssueID string
	// CommentRef is the "<ITEM-ID>#<file-stem>" reference of the comment.
	CommentRef string
}

// YouTrackCommentPushParams is what one `youtrack.comment.push` job is asked to
// do: the item whose thread the comment belongs to, and the comment file.
type YouTrackCommentPushParams struct {
	// ItemID is the git-in-track item id, for example ACME-US-0042.
	ItemID string `json:"itemId"`
	// CommentPath is the vault-relative path of the comment file.
	CommentPath string `json:"commentPath"`
}

// YouTrackCommentPushRequest is one queued push.
type YouTrackCommentPushRequest struct {
	Repo    string                    `json:"repo,omitempty"`
	Project string                    `json:"project,omitempty"`
	Params  YouTrackCommentPushParams `json:"params"`
}

// EnqueueYouTrackCommentPush queues the push of one comment and returns the
// engine job id.
//
// The coalescing key is the comment path, so a burst of edits to one comment
// produces exactly one push — which is also why the handler reads the comment
// when it runs rather than carrying its text in the payload: the payload would
// be the text as it was when the first edit was made.
func (s *Server) EnqueueYouTrackCommentPush(ctx context.Context, req YouTrackCommentPushRequest) (string, error) {
	envelope := youtrackJobPayload{Repo: req.Repo, Project: req.Project}
	return s.enqueueYouTrackJob(context.WithoutCancel(ctx), kindYouTrackCommentPush,
		strings.TrimSpace(req.Params.CommentPath), envelope, req.Params)
}

// handleYouTrackCommentJobs is the registered handler. Jobs in one batch share
// a comment path, so pushing the last of them would be enough; they are pushed
// in order anyway, because the handler is idempotent and an ordered replay is
// easier to read in a log than a clever one.
func (s *Server) handleYouTrackCommentJobs(ctx context.Context, batch []syncengine.Job) error {
	for _, job := range batch {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // cancellation must reach the engine unwrapped
		}
		if err := s.runYouTrackCommentPush(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

// errCommentItemUnlinked is the failure of a push whose item mirrors no
// YouTrack issue. It is terminal: there is nowhere to post, and no number of
// attempts creates a link that only a user can create.
var errCommentItemUnlinked = errors.New("the item has no YouTrack reference")

// errCommentStale is the failure of a write-back whose file changed underneath
// it. It is retryable: the next attempt reads the new content and pushes that,
// which is what a comment that was edited while it was being pushed wants.
var errCommentStale = errors.New("the comment changed while it was being pushed")

// runYouTrackCommentPush performs one push.
func (s *Server) runYouTrackCommentPush(ctx context.Context, job syncengine.Job) error {
	payload, err := decodeJobPayload(job)
	if err != nil {
		return err
	}
	params, err := decodeJobParams[YouTrackCommentPushParams](payload)
	if err != nil {
		return err
	}
	if strings.TrimSpace(params.CommentPath) == "" || strings.TrimSpace(params.ItemID) == "" {
		return terminalf("job %s names no comment: itemId and commentPath are both required", job.ID)
	}
	m, project, err := s.jobMount(payload)
	if err != nil {
		return err
	}
	client, link, err := s.jobClient(project)
	if err != nil {
		return classifyYouTrackJobError(fmt.Errorf("resolve the YouTrack client of %s: %w", project, err))
	}

	issueID, err := s.issueOfItem(ctx, m, params.ItemID)
	if err != nil {
		return err
	}

	file := filepath.Join(m.path, filepath.FromSlash(params.CommentPath))
	comment, rev, err := readComment(file, params.CommentPath)
	if err != nil {
		return err
	}

	text, err := renderCommentText(comment, s.youtrack.commentTemplate(project), issueID)
	if err != nil {
		return err
	}

	existing, linked := youtrackExternalOf(comment.External)
	if linked {
		return s.editRemoteComment(ctx, client, issueID, existing.ID, text)
	}

	created, err := client.AddComment(ctx, issueID, text)
	if err != nil {
		return classifyYouTrackJobError(fmt.Errorf("post a comment on %s: %w", issueID, err))
	}
	ref := commentExternalRef(link.BaseURL, issueID, created.ID, core.NewTimestamp(s.now()))
	writes, err := s.recordCommentExternal(ctx, m, params, rev, ref)
	if err != nil {
		return err
	}
	s.commitJobWrites(ctx, m, writes, gitops.Fields{
		ItemID: params.ItemID, Title: params.CommentPath,
		Type: "comment", Action: gitops.ActionUpdate,
	})
	s.log.Info("a comment was pushed to YouTrack",
		"job", job.ID, "item", params.ItemID, "issue", issueID, "comment", created.ID)
	return nil
}

// recordCommentExternal writes the remote comment id onto the local comment,
// through the vault, and returns the write set the committer wants.
//
// It goes through "comment.update" rather than touching the file, which is what
// keeps internal/server out of the business of writing repository files: the
// vault takes the same optimistic lock this used to hand-roll (rev is the hash
// of the bytes the push was built from, so a comment edited in flight is
// reported rather than clobbered), upserts the reference by system so a
// re-delivered job replaces its entry instead of appending a second one, and
// folds the index forward itself.
func (s *Server) recordCommentExternal(
	ctx context.Context, m *mount, params YouTrackCommentPushParams, rev core.Rev, ref core.External,
) (vault.WriteSet, error) {
	var out struct {
		Comment core.Comment   `json:"comment"`
		Writes  vault.WriteSet `json:"writes"`
	}
	err := dispatchVault(ctx, m, "comment.update", map[string]any{
		"id":          params.ItemID,
		"path":        params.CommentPath,
		"rev":         string(rev),
		"setExternal": []core.External{ref},
	}, &out)
	if err != nil {
		// A stale rev is retryable on purpose: the next attempt reads the text
		// that is actually on disk and pushes that.
		if errors.Is(err, core.ErrRevMismatch) {
			return vault.WriteSet{}, fmt.Errorf("%w: %s", errCommentStale, params.CommentPath)
		}
		return vault.WriteSet{}, err
	}
	return out.Writes, nil
}

// editRemoteComment updates a comment that was already pushed.
//
// Posting again is not an option: it would leave a second copy of the comment
// on the issue, which is exactly what the `external` field exists to prevent.
func (s *Server) editRemoteComment(
	ctx context.Context, client youtrackJobClient, issueID, commentID, text string,
) error {
	if _, err := client.UpdateComment(ctx, issueID, commentID, text); err != nil {
		return classifyYouTrackJobError(fmt.Errorf("edit comment %s on %s: %w", commentID, issueID, err))
	}
	s.log.Info("a pushed comment was updated in YouTrack", "issue", issueID, "comment", commentID)
	return nil
}

// issueOfItem resolves the YouTrack issue an item mirrors.
func (s *Server) issueOfItem(ctx context.Context, m *mount, itemID string) (string, error) {
	var item struct {
		External []core.External `json:"external"`
	}
	if err := dispatchVault(ctx, m, "item.get", map[string]any{"id": itemID}, &item); err != nil {
		return "", terminalf("read %s: %w", itemID, err)
	}
	ref, ok := youtrackExternalOf(item.External)
	if !ok {
		return "", terminalf("%w: %s mirrors no issue, so its comments have nowhere to go",
			errCommentItemUnlinked, itemID)
	}
	return ref.ID, nil
}

// youtrackExternalOf picks the YouTrack entry out of an external list.
func youtrackExternalOf(refs []core.External) (core.External, bool) {
	for _, ref := range refs {
		if ref.Ref().System == mapping.System && ref.ID != "" {
			return ref, true
		}
	}
	return core.External{}, false
}

// commentExternalRef builds the reference a pushed comment records.
//
// The URL is the issue with the comment as a fragment, which is the shape
// internal/youtrack/mapping records for an imported comment, so that a comment
// that traveled in either direction is addressed the same way.
func commentExternalRef(baseURL, issueID, commentID string, at core.Timestamp) core.External {
	ref := core.External{System: mapping.System, ID: strings.TrimSpace(commentID), SyncedAt: at}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base != "" && issueID != "" && ref.ID != "" {
		ref.URL = base + "/issue/" + issueID + "#focus=Comments-" + ref.ID
	}
	return core.NormalizeExternal(ref)
}

// ------------------------------------------------------------ the file ---

// readComment reads one comment file and the rev of the bytes that were read.
func readComment(file, rel string) (*core.Comment, core.Rev, error) {
	data, err := os.ReadFile(file) //nolint:gosec // the path comes from the job payload, inside the mounted repository
	if err != nil {
		return nil, "", terminalf("read %s: %w", rel, err)
	}
	comment, err := core.ParseComment(rel, data)
	if err != nil {
		return nil, "", terminalf("parse %s: %w", rel, err)
	}
	return comment, core.ComputeRev(data), nil
}

// upsertExternal replaces the entry with the same system and appends it when
// there is none, which is what makes a second write update the reference
// instead of growing the list. A comment goes through "comment.update" and
// leaves this to the vault; the knowledge-base publish, which assembles a whole
// page before writing it, still needs it here.
func upsertExternal(list []core.External, ref core.External) []core.External {
	out := make([]core.External, 0, len(list)+1)
	replaced := false
	for _, entry := range list {
		if entry.Ref().System == ref.Ref().System {
			if !replaced {
				out = append(out, ref)
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, ref)
	}
	return out
}

// -------------------------------------------------------- attribution ---

// commentTemplate returns the attribution template of one project: the one
// project.yaml declares, or the shipped default.
func (y *youtrackState) commentTemplate(project string) string {
	found, err := y.linkFor(project)
	if err != nil || found.link == nil {
		return defaultCommentAttribution
	}
	if tmpl := found.link.CommentTemplate; strings.TrimSpace(tmpl) != "" {
		return tmpl
	}
	return defaultCommentAttribution
}

// renderCommentText renders the body a push posts: the comment as written, then
// the attribution line the project's template produces.
//
// A template that does not parse or does not render is a configuration error,
// so it fails the job terminally and names the project.yaml key: retrying a
// broken template five times helps nobody.
func renderCommentText(comment *core.Comment, tmpl, issueID string) (string, error) {
	parsed, err := template.New("attribution").Parse(tmpl)
	if err != nil {
		return "", terminalf(
			"integrations.youtrack.comment_template does not parse: %w", err)
	}
	var line strings.Builder
	data := commentAttribution{
		Author:     comment.Author,
		AuthorName: comment.AuthorName,
		ItemID:     string(comment.Item),
		IssueID:    issueID,
		CommentRef: comment.Ref(),
	}
	if err := parsed.Execute(&line, data); err != nil {
		return "", terminalf(
			"integrations.youtrack.comment_template does not render: %w", err)
	}
	body := strings.TrimRight(comment.Body, "\n")
	return body + line.String(), nil
}
