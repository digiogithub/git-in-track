package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The "comment.update" method: the write path for a comment that already
// exists (story GIT-US-0068).
//
// Until this existed, `comment.add` was the only way a comment file was ever
// written, and every caller that needed to *change* one — the YouTrack comment
// push, which records the remote comment id on the local file after it posted
// — had to open the file itself, hash it, re-read it and write it back, then
// reindex by hand. That is a layering break: the vault owns the index and the
// write set, and a writer that goes around it owns neither.
//
// So this method does what every other vault write does and nothing more:
// it takes the rev of the bytes the caller read, refuses the write when the
// file moved underneath it, writes through the tracking file system, and
// returns the WriteSet that commit-on-save and the browser host persist. The
// index is folded forward by the same commit, so the next `comment.list`
// already sees the change.
//
// What it deliberately does not do is delete. A comment is removed from a
// thread by removing its file; there is no method for it here because no
// surface has asked for one, and a thread is a conversation rather than a
// record the tool may quietly rewrite.

// commentUpdateParams is the sparse patch of one comment file.
//
// Path addresses the file rather than an (item, index) pair because a comment
// has no id of its own: the file name is its identity, which is exactly what
// keeps two concurrent replies from ever conflicting in git.
type commentUpdateParams struct {
	// Path is the vault-relative path of the comment file.
	Path string `json:"path"`
	// ID is the item the comment belongs to. Optional; when it is given it is
	// checked against the comment's own `item` field, so that a caller working
	// from a stale path cannot write into the wrong thread.
	ID string `json:"id,omitempty"`
	// Rev is the revision of the comment file as the caller read it. It is
	// required: a blind write to a file somebody else may have edited is the
	// failure mode this method exists to prevent. "*" is the wildcard of
	// If-Match, an explicit write against whatever the file holds now.
	Rev string `json:"rev"`
	// Body replaces the comment text when it is present. Absent leaves the
	// text alone, which is what a caller recording an external reference
	// wants.
	Body *string `json:"body,omitempty"`
	// External replaces the whole reference list when it is present, including
	// with an empty list, which is how a caller unlinks a comment.
	External *[]core.External `json:"external,omitempty"`
	// SetExternal upserts one entry per system: an entry whose system is
	// already referenced replaces that entry rather than appending a second
	// one. This is the shape a tracker push wants — it has just learned the
	// remote id of this comment and wants it to be the only one recorded for
	// that system.
	SetExternal []core.External `json:"setExternal,omitempty"`
}

// commentUpdate applies a sparse patch to one comment file under an optimistic
// lock, and returns the comment as it now stands together with the write set.
func (v *Vault) commentUpdate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[commentUpdateParams](raw)
	if err != nil {
		return nil, err
	}
	rel := strings.TrimSpace(p.Path)
	if rel == "" {
		return nil, failf("invalid_request", "comment.update needs the path of the comment file")
	}
	rel = path.Clean(rel)
	if strings.TrimSpace(p.Rev) == "" {
		return nil, failf("invalid_request",
			"comment.update needs the rev of %s; pass \"*\" to write over whatever it holds now", rel)
	}
	if p.Body == nil && p.External == nil && len(p.SetExternal) == 0 {
		return nil, failf("invalid_request", "comment.update was asked to change nothing about %s", rel)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("update comment %s: %w", rel, err)
	}

	current, err := v.fs.ReadFile(rel)
	if err != nil {
		return nil, &Error{
			Code: "not_found", Message: fmt.Sprintf("read %s: %v", rel, err), Path: rel,
		}
	}
	if p.Rev != "*" {
		if got := core.ComputeRev(current); got != core.Rev(p.Rev) {
			return nil, &core.StaleRevisionError{
				Path: rel, Expected: core.Rev(p.Rev), Current: got,
			}
		}
	}
	comment, err := core.ParseComment(rel, current)
	if err != nil {
		return nil, failf("invalid_request", "parse %s: %v", rel, err)
	}
	if want := strings.TrimSpace(p.ID); want != "" && !strings.EqualFold(want, string(comment.Item)) {
		return nil, failf("invalid_request",
			"%s belongs to %s, not to %s", rel, comment.Item, want)
	}

	if p.Body != nil {
		body := strings.Trim(*p.Body, "\n")
		if strings.TrimSpace(body) == "" {
			// An empty body would leave a file that parses but says nothing.
			// Removing a comment is removing its file, not blanking it.
			return nil, failf("invalid_request", "comment.update was given an empty body for %s", rel)
		}
		if body != comment.Body {
			comment.Body = body
			// `updated` means the text was edited: a caller that only records
			// an external reference has not edited anything a reader would see,
			// so the stamp is moved here and nowhere else.
			comment.Updated = core.NewTimestamp(v.now())
		}
	}
	if p.External != nil {
		comment.External = normalizeExternalList(*p.External)
	}
	comment.External = upsertExternalBySystem(comment.External, p.SetExternal)

	data, err := core.SerializeComment(comment)
	if err != nil {
		return nil, fmt.Errorf("serialize %s: %w", rel, err)
	}
	v.fs.begin()
	if err := v.fs.WriteFile(rel, data); err != nil {
		return nil, fmt.Errorf("write %s: %w", rel, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	comment.Rev = core.ComputeRev(data)
	return map[string]any{"comment": comment, "writes": writes}, nil
}

// normalizeExternalList trims every entry and drops the ones that name no
// system or no id, which a caller assembling a list by hand can easily produce.
func normalizeExternalList(list []core.External) []core.External {
	out := make([]core.External, 0, len(list))
	for _, raw := range list {
		ref := core.NormalizeExternal(raw)
		if ref.System == "" || ref.ID == "" {
			continue
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// upsertExternalBySystem replaces the entry of each named system, in place, and
// appends the ones whose system is not referenced yet.
//
// Replacing by *system* rather than by (system, id) is the point: a second push
// of the same comment must update the reference it wrote the first time, not
// grow the list by one entry per attempt.
func upsertExternalBySystem(list, set []core.External) []core.External {
	for _, raw := range set {
		ref := core.NormalizeExternal(raw)
		if ref.System == "" || ref.ID == "" {
			continue
		}
		replaced := false
		out := make([]core.External, 0, len(list)+1)
		for _, entry := range list {
			if core.NormalizeExternal(entry).System == ref.System {
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
		list = out
	}
	return list
}

// commentTaskParams is the input of "comment.task.set".
type commentTaskParams struct {
	// Path is the vault-relative path of the comment file.
	Path string `json:"path"`
	// ID is the item the comment belongs to, checked like comment.update's.
	ID string `json:"id,omitempty"`
	// Line is the 1-based line of the checkbox inside the comment body, as the
	// renderer stamped it on the rendered input.
	Line    int    `json:"line"`
	Checked bool   `json:"checked"`
	Rev     string `json:"rev"`
}

// commentTaskSet flips one task-list checkbox in the body of a comment.
//
// It is the comment twin of "item.task.set": the core rewrites the one byte
// between the brackets on the body read under the caller's `rev`, and the
// result goes back through "comment.update", so the write is rev-guarded,
// stamps `updated` and returns the same write set any other comment edit does.
func (v *Vault) commentTaskSet(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[commentTaskParams](raw)
	if err != nil {
		return nil, err
	}
	rel := path.Clean(strings.TrimSpace(p.Path))
	if strings.TrimSpace(p.Path) == "" {
		return nil, failf("invalid_request", "a comment task toggle needs the path of the comment file")
	}
	if p.Rev == "" || p.Rev == "*" {
		return nil, failf("invalid_request", "a comment task toggle needs the rev the body was read at")
	}
	current, err := v.fs.ReadFile(rel)
	if err != nil {
		return nil, &Error{
			Code: "not_found", Message: fmt.Sprintf("read %s: %v", rel, err), Path: rel,
		}
	}
	if got := core.ComputeRev(current); got != core.Rev(p.Rev) {
		return nil, &core.StaleRevisionError{Path: rel, Expected: core.Rev(p.Rev), Current: got}
	}
	comment, err := core.ParseComment(rel, current)
	if err != nil {
		return nil, failf("invalid_request", "parse %s: %v", rel, err)
	}

	body, err := core.SetTaskListItem(comment.Body, p.Line, p.Checked)
	if err != nil {
		if errors.Is(err, core.ErrNotTaskListItem) {
			return nil, failf(core.TaskListItemMismatchCode,
				"line %d of %s is not a task-list item; the comment has changed since it was rendered",
				p.Line, rel)
		}
		return nil, fmt.Errorf("toggle %s: %w", rel, err)
	}

	patch, err := json.Marshal(commentUpdateParams{Path: rel, ID: p.ID, Rev: p.Rev, Body: &body})
	if err != nil {
		return nil, fmt.Errorf("toggle %s: %w", rel, err)
	}
	return v.commentUpdate(ctx, patch)
}
