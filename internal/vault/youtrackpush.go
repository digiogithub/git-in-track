package vault

import (
	"context"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the comment half of the outbound YouTrack integration: the core
// API method "youtrack.comment.push" (story GIT-US-0079) and the automatic
// seam on "comment.add" (story GIT-US-0072).
//
// Two rules shape it:
//
//  1. Nothing is pushed inline. Both the explicit method and the automatic seam
//     queue a job and return; a comment saved in the web app or written by an
//     agent must not wait on a remote tracker, and an HTTP response that ends
//     must not cancel the push it started.
//
//  2. There is one decision, in one place. The automatic seam is the vault's
//     own "comment.add", which every surface goes through — REST, the web app,
//     the MCP add_comment tool and the CLI — so no surface carries a second
//     copy of the rule and no surface can forget it.

// YouTrackCommentPushParams is the input of "youtrack.comment.push".
type YouTrackCommentPushParams struct {
	// Project is the git-in-track project key. It may be empty: the item id
	// names its project.
	Project string `json:"project,omitempty"`
	// ItemID is the item whose thread is being pushed.
	ItemID string `json:"itemId"`
	// CommentPath selects one comment file. Exactly one of CommentPath and All
	// is given.
	CommentPath string `json:"commentPath,omitempty"`
	// All selects every comment of the item that carries no YouTrack reference
	// yet.
	All bool `json:"all,omitempty"`
}

// validate checks the parameters and names the offending field in every
// message, so that a form can mark the input that was wrong.
func (p YouTrackCommentPushParams) validate() error {
	if strings.TrimSpace(p.ItemID) == "" {
		return failf("invalid_request",
			`"itemId": name the item whose comments are being pushed, for example ACME-US-0042`)
	}
	comment := strings.TrimSpace(p.CommentPath)
	switch {
	case comment == "" && !p.All:
		return failf("invalid_request",
			`"commentPath" or "all": push one comment by path, or every comment of the item with "all"`)
	case comment != "" && p.All:
		return failf("invalid_request",
			`"commentPath" and "all": give one or the other, not both`)
	}
	return nil
}

// YouTrackCommentPushEntry is one comment of the answer.
type YouTrackCommentPushEntry struct {
	CommentPath string `json:"commentPath"`
	// YouTrackCommentID and URL are filled for a comment a previous push
	// already placed upstream, which is why it is skipped.
	YouTrackCommentID string `json:"youtrackCommentId,omitempty"`
	URL               string `json:"url,omitempty"`
	// Reason says why a comment was skipped, and Error why one could not be
	// queued.
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// YouTrackCommentPushResult is the answer of "youtrack.comment.push".
//
// Pushed is what was queued for the tracker, not what has already arrived
// there: the work happens in the job named by JobID, and the job reports its
// own outcome through the engine's events.
type YouTrackCommentPushResult struct {
	Project string `json:"project"`
	ItemID  string `json:"itemId"`
	// JobID is the last job queued, empty when nothing was.
	JobID   string                     `json:"jobId,omitempty"`
	Pushed  []YouTrackCommentPushEntry `json:"pushed"`
	Skipped []YouTrackCommentPushEntry `json:"skipped"`
	Failed  []YouTrackCommentPushEntry `json:"failed"`
}

// youtrackCommentDispatch answers "youtrack.comment.push". Dispatch routes it
// here before it takes the vault mutex, because resolving the project link goes
// through the host and the vault lock must not be held across it.
func (v *Vault) youtrackCommentDispatch(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[YouTrackCommentPushParams](raw)
	if err != nil {
		return nil, err
	}
	return v.YouTrackCommentPush(ctx, p)
}

// YouTrackCommentPush queues one push job per selected comment and reports what
// was queued, what was already upstream and what could not be queued.
func (v *Vault) YouTrackCommentPush(
	ctx context.Context, p YouTrackCommentPushParams,
) (YouTrackCommentPushResult, error) {
	if err := p.validate(); err != nil {
		return YouTrackCommentPushResult{}, err
	}
	enqueue := v.youtrackEnqueuer()
	if enqueue == nil {
		return YouTrackCommentPushResult{}, failf("unavailable",
			"this host runs no background jobs: a comment push is queued by the companion process")
	}

	id := core.ItemID(strings.TrimSpace(p.ItemID))
	v.mu.Lock()
	item, err := v.index.Item(id)
	var thread []core.Comment
	if err == nil && item != nil {
		thread = v.index.Comments(id)
	}
	v.mu.Unlock()
	if err != nil || item == nil {
		return YouTrackCommentPushResult{}, &Error{
			Code: "not_found", Message: "item " + string(id) + " is not indexed",
		}
	}
	if _, linked := youtrackRefOf(item.External); !linked {
		// Non-retryable on purpose: there is nothing to push a comment onto,
		// and no amount of waiting will create one.
		return YouTrackCommentPushResult{}, failf("invalid_request",
			"%s carries no YouTrack reference: import or link the item before pushing its comments",
			id)
	}
	if _, _, err := v.youtrackClient(ctx, p.Project); err != nil {
		return YouTrackCommentPushResult{}, err
	}

	out := YouTrackCommentPushResult{
		Project: p.Project, ItemID: string(id),
		Pushed:  []YouTrackCommentPushEntry{},
		Skipped: []YouTrackCommentPushEntry{},
		Failed:  []YouTrackCommentPushEntry{},
	}
	selected, err := selectCommentsToPush(thread, p)
	if err != nil {
		return YouTrackCommentPushResult{}, err
	}
	for _, comment := range selected {
		if ref, already := youtrackRefOf(comment.External); already {
			out.Skipped = append(out.Skipped, YouTrackCommentPushEntry{
				CommentPath:       comment.Path,
				YouTrackCommentID: ref.ID,
				URL:               ref.URL,
				Reason:            "the comment is already on the issue",
			})
			continue
		}
		jobID, queueErr := enqueue(ctx, YouTrackJob{
			Kind: JobKindCommentPush, Key: comment.Path, Project: p.Project,
			Payload: youtrackCommentJob{
				Project: p.Project, ItemID: string(id), CommentPath: comment.Path,
			},
		})
		if queueErr != nil {
			out.Failed = append(out.Failed, YouTrackCommentPushEntry{
				CommentPath: comment.Path, Error: queueErr.Error(),
			})
			continue
		}
		out.JobID = jobID
		out.Pushed = append(out.Pushed, YouTrackCommentPushEntry{CommentPath: comment.Path})
	}
	return out, nil
}

// youtrackCommentJob is the payload of a JobKindCommentPush job: enough for the
// handler to find the comment file and the issue it belongs on, and nothing
// that could go stale between queueing and running.
type youtrackCommentJob struct {
	Project     string `json:"project,omitempty"`
	ItemID      string `json:"itemId"`
	CommentPath string `json:"commentPath"`
}

// selectCommentsToPush applies the two selection modes: one comment by path, or
// every comment of the item.
func selectCommentsToPush(
	thread []core.Comment, p YouTrackCommentPushParams,
) ([]core.Comment, error) {
	if p.All {
		return thread, nil
	}
	want := strings.TrimSpace(p.CommentPath)
	for _, comment := range thread {
		if comment.Path == want {
			return []core.Comment{comment}, nil
		}
	}
	return nil, &Error{
		Code:    "not_found",
		Message: "no comment of " + p.ItemID + " is stored at " + want,
		Path:    want,
	}
}

// ------------------------------------------------------- the automatic seam --

// autoPushComment queues a push for a comment that has just been written, when
// the project asked for it. It is called by "comment.add" after the vault lock
// is released, so that every surface — REST, the web app, MCP and the CLI —
// reaches one decision through one path.
//
// It never fails the write that triggered it: a comment is saved whether or not
// it could be queued for the tracker, and a host with no engine or no client
// simply queues nothing.
func (v *Vault) autoPushComment(ctx context.Context, result any) {
	enqueue := v.youtrackEnqueuer()
	if enqueue == nil {
		return
	}
	comment, ok := commentOfResult(result)
	if !ok || comment.Path == "" || comment.Item == "" {
		return
	}
	// The write-back of the YouTrack comment id carries the reference, so a
	// comment that already has one is never queued again. That, and not a flag,
	// is what closes the feedback loop.
	if _, already := youtrackRefOf(comment.External); already {
		return
	}

	v.mu.Lock()
	item, err := v.index.Item(comment.Item)
	v.mu.Unlock()
	if err != nil || item == nil {
		return
	}
	if _, linked := youtrackRefOf(item.External); !linked {
		return
	}

	project := ""
	if key, _, _, parseErr := core.ParseItemID(string(comment.Item)); parseErr == nil {
		project = string(key)
	}
	_, link, err := v.youtrackClient(ctx, project)
	if err != nil || !strings.EqualFold(strings.TrimSpace(link.PushComments), YouTrackPushAuto) {
		return
	}
	// context.WithoutCancel: the caller's context ends with its HTTP response,
	// and the queued job must outlive it. The engine coalesces on Key, so a
	// burst of writes to the same comment path leaves one job behind.
	_, _ = enqueue(context.WithoutCancel(ctx), YouTrackJob{
		Kind: JobKindCommentPush, Key: comment.Path, Project: project,
		Payload: youtrackCommentJob{
			Project: project, ItemID: string(comment.Item), CommentPath: comment.Path,
		},
	})
}

// commentOfResult digs the comment out of what "comment.add" answered.
func commentOfResult(result any) (core.Comment, bool) {
	payload, ok := result.(map[string]any)
	if !ok {
		return core.Comment{}, false
	}
	comment, ok := payload["comment"].(*core.Comment)
	if !ok || comment == nil {
		return core.Comment{}, false
	}
	return *comment, true
}
