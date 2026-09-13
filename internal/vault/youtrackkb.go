package vault

import (
	"context"
	"path"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// This file is the knowledge-base half of the YouTrack integration: the three
// core API methods "youtrack.kb.status", "youtrack.kb.publish" and
// "youtrack.kb.pull" (story GIT-US-0090).
//
// Three rules shape it:
//
//  1. Status is cheap by default. A tree view asks about hundreds of pages at
//     once, so the remote side is read only when the caller sets `remote`.
//     Without it the answer comes from the page's own `external` entry and the
//     content the page would publish — no request leaves the process.
//
//  2. Change detection compares content, never raw bytes. core.PruneKbFeedback
//     rewrites the `## Feedback` block on every write and the block never
//     leaves the repository (ADR-030), so a byte comparison would report a
//     remote edit every time a note is added locally and "out of date" would
//     become permanent. The comparison is mapping.EqualContent, and the
//     fingerprint recorded in the external entry's `key` is taken over the
//     same normalized form.
//
//  3. Publishing and pulling are queued, never performed here. Both methods
//     validate, check that the project is linked and hand a job to the host's
//     engine, so that a click in the UI or a call from an agent returns at once
//     and the network work happens where retries, rate limiting and the
//     journal already live.

// The five states "youtrack.kb.status" reports for one page.
const (
	// KBStateUnlinked is a page that carries no YouTrack article reference.
	KBStateUnlinked = "unlinked"
	// KBStateInSync is a page whose content matches the article it mirrors.
	KBStateInSync = "in_sync"
	// KBStateLocalAhead is a page edited since it was last synchronized.
	KBStateLocalAhead = "local_ahead"
	// KBStateRemoteAhead is an article edited since the page was last
	// synchronized. It is only ever reported when the remote was read.
	KBStateRemoteAhead = "remote_ahead"
	// KBStateConflict is both sides changed since the last synchronization, or
	// the two differ with no evidence of which side moved. A publish or a pull
	// then writes `<page>.conflict.md` beside the page rather than merging.
	KBStateConflict = "conflict"
)

// YouTrackKBParams is the input of all three knowledge-base methods.
type YouTrackKBParams struct {
	// Project is the git-in-track project key. It may be empty when the vault
	// holds exactly one project.
	Project string `json:"project,omitempty"`
	// Path is the vault-relative path of a page or of a folder. Empty means the
	// project's whole documentation folder.
	Path string `json:"path,omitempty"`
	// Recursive includes the pages of every folder below Path. Without it a
	// folder answers for its direct pages only.
	Recursive bool `json:"recursive,omitempty"`
	// Remote asks status to read each linked article. It is off by default
	// because a tree view of a documentation folder would otherwise become one
	// request per page.
	Remote bool `json:"remote,omitempty"`
}

// validate checks the parameters and names the offending field, so that a form
// can mark the input that was wrong.
func (p YouTrackKBParams) validate() error {
	clean := strings.TrimSpace(p.Path)
	if clean == "" {
		return nil
	}
	if strings.HasPrefix(clean, "/") {
		return failf("invalid_request",
			`"path": %q is absolute: give a path relative to the vault root`, p.Path)
	}
	cleaned := path.Clean(clean)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return failf("invalid_request",
			`"path": %q leaves the vault: a knowledge-base path stays inside it`, p.Path)
	}
	return nil
}

// cleanPath is the parameter's path in the form the index stores paths in.
func (p YouTrackKBParams) cleanPath() string {
	trimmed := strings.Trim(strings.TrimSpace(p.Path), "/")
	if trimmed == "" {
		return ""
	}
	return path.Clean(trimmed)
}

// YouTrackKBPageStatus is what "youtrack.kb.status" says about one page.
type YouTrackKBPageStatus struct {
	Path string `json:"path"`
	// Linked reports an `external` entry for this system, whatever the state.
	Linked bool `json:"linked"`
	// ArticleID and URL are the article the page mirrors, empty when unlinked.
	ArticleID string `json:"articleId,omitempty"`
	URL       string `json:"url,omitempty"`
	// State is one of the five KBState constants.
	State string `json:"state"`
	// SyncedAt is the stamp of the last successful synchronization.
	SyncedAt core.Timestamp `json:"syncedAt,omitempty"`
	// Error is why the remote side could not be consulted, empty otherwise. It
	// never fails the call: one unreachable article must not hide the state of
	// every other page.
	Error string `json:"error,omitempty"`
}

// YouTrackKBStatusResult is the answer of "youtrack.kb.status".
type YouTrackKBStatusResult struct {
	Project string                 `json:"project"`
	Pages   []YouTrackKBPageStatus `json:"pages"`
	// Remote reports whether the remote side was actually consulted, so that a
	// caller can tell "in sync as far as the local side knows" from "in sync,
	// checked".
	Remote bool `json:"remote"`
}

// YouTrackKBJobResult is the answer of "youtrack.kb.publish" and
// "youtrack.kb.pull": the job that was queued and the pages it will touch.
type YouTrackKBJobResult struct {
	Project string `json:"project"`
	JobID   string `json:"jobId"`
	// Pages are the pages the job selected, in index order.
	Pages []string `json:"pages"`
}

// youtrackKBDispatch answers the three knowledge-base methods. Dispatch routes
// them here before it takes the vault mutex: a status call with `remote` reads
// articles over the network, and a lock held across that would stall every
// other reader of this repository.
func (v *Vault) youtrackKBDispatch(ctx context.Context, method string, raw []byte) (any, error) {
	p, err := decodeParams[YouTrackKBParams](raw)
	if err != nil {
		return nil, err
	}
	switch method {
	case "youtrack.kb.status":
		return v.YouTrackKBStatus(ctx, p)
	case "youtrack.kb.publish":
		return v.YouTrackKBPublish(ctx, p)
	default:
		return v.YouTrackKBPull(ctx, p)
	}
}

// YouTrackKBStatus reports the synchronization state of every selected page.
//
// The local half needs no network at all: the fingerprint an earlier
// synchronization recorded in the page's `external` entry is compared with the
// fingerprint of the content the page would publish now. The remote half is
// read only when the caller asked for it, one article per linked page.
func (v *Vault) YouTrackKBStatus(
	ctx context.Context, p YouTrackKBParams,
) (YouTrackKBStatusResult, error) {
	if err := p.validate(); err != nil {
		return YouTrackKBStatusResult{}, err
	}

	v.mu.Lock()
	pages, opts, err := v.youtrackKBSelect(p)
	v.mu.Unlock()
	if err != nil {
		return YouTrackKBStatusResult{}, err
	}

	out := YouTrackKBStatusResult{
		Project: p.Project,
		Pages:   make([]YouTrackKBPageStatus, 0, len(pages)),
	}

	// The client is resolved once, and only when the caller asked for the
	// remote side: a status call over a project with no client installed is
	// still a useful answer about the local side.
	var source YouTrackSource
	if p.Remote {
		resolved, link, resolveErr := v.youtrackClient(ctx, p.Project)
		if resolveErr != nil {
			return YouTrackKBStatusResult{}, resolveErr
		}
		source = resolved
		opts.BaseURL = link.BaseURL
		out.Remote = true
	}

	for _, page := range pages {
		out.Pages = append(out.Pages, v.youtrackKBPageStatus(ctx, source, page, opts))
	}
	return out, nil
}

// youtrackKBPageStatus decides the state of one page.
func (v *Vault) youtrackKBPageStatus(
	ctx context.Context, source YouTrackSource, page *core.KBPage, opts mapping.PageOptions,
) YouTrackKBPageStatus {
	out := YouTrackKBPageStatus{Path: page.Path, State: KBStateUnlinked}
	ref, linked := youtrackRefOf(page.ExternalRefs)
	if !linked {
		return out
	}
	out.Linked = true
	out.ArticleID = ref.ID
	out.URL = ref.URL
	out.SyncedAt = ref.SyncedAt

	payload, _ := mapping.PageToArticle(*page, opts)
	local := kbFingerprint(payload.Content)
	localChanged := ref.Key != "" && local != ref.Key
	if ref.Key == "" {
		// A page published before the fingerprint was recorded, or imported by
		// hand: fall back to the timestamps, which is all there is to compare.
		localChanged = !page.Updated.IsZero() && page.Updated.After(ref.SyncedAt.Time)
	}

	if source == nil {
		if localChanged {
			out.State = KBStateLocalAhead
		} else {
			out.State = KBStateInSync
		}
		return out
	}

	article, err := source.Article(ctx, ref.ID)
	if err != nil {
		// One unreadable article is reported on its own line; the rest of the
		// tree still gets an answer.
		out.Error = err.Error()
		if localChanged {
			out.State = KBStateLocalAhead
		} else {
			out.State = KBStateInSync
		}
		return out
	}

	// EqualContent, not a byte comparison: the feedback block is local-only and
	// is rewritten on every write, so comparing raw bodies would report a
	// remote change every time a note is added on this side (ADR-030).
	if mapping.EqualContent(payload.Content, article.Content) &&
		strings.TrimSpace(payload.Summary) == strings.TrimSpace(article.Summary) {
		out.State = KBStateInSync
		return out
	}

	remoteChanged := kbFingerprint(article.Content) != ref.Key
	if ref.Key == "" {
		remoteChanged = article.Updated.Time().After(ref.SyncedAt.Time)
	}
	switch {
	case localChanged && remoteChanged:
		out.State = KBStateConflict
	case localChanged:
		out.State = KBStateLocalAhead
	case remoteChanged:
		out.State = KBStateRemoteAhead
	default:
		// The two differ and neither side looks touched: the safe answer is the
		// one that refuses to overwrite either.
		out.State = KBStateConflict
	}
	return out
}

// YouTrackKBPublish queues the publication of the selected pages as articles
// and returns the job id. It writes nothing and talks to nothing: the job does
// both, where the retry policy and the rate limiter are.
func (v *Vault) YouTrackKBPublish(
	ctx context.Context, p YouTrackKBParams,
) (YouTrackKBJobResult, error) {
	return v.youtrackKBQueue(ctx, JobKindKBPublish, p)
}

// YouTrackKBPull queues writing the selected pages back from their articles and
// returns the job id.
func (v *Vault) YouTrackKBPull(
	ctx context.Context, p YouTrackKBParams,
) (YouTrackKBJobResult, error) {
	return v.youtrackKBQueue(ctx, JobKindKBPull, p)
}

// youtrackKBQueue is the shared half of publish and pull: validate, select,
// check the project link, enqueue.
func (v *Vault) youtrackKBQueue(
	ctx context.Context, kind string, p YouTrackKBParams,
) (YouTrackKBJobResult, error) {
	if err := p.validate(); err != nil {
		return YouTrackKBJobResult{}, err
	}
	enqueue := v.youtrackEnqueuer()
	if enqueue == nil {
		return YouTrackKBJobResult{}, failf("unavailable",
			"this host runs no background jobs: %s is queued by the companion process", kind)
	}

	v.mu.Lock()
	pages, _, err := v.youtrackKBSelect(p)
	v.mu.Unlock()
	if err != nil {
		return YouTrackKBJobResult{}, err
	}
	if len(pages) == 0 {
		return YouTrackKBJobResult{}, failf("not_found",
			`"path": no knowledge-base page matches %q`, p.Path)
	}
	// The project link is checked before anything is queued, so that an
	// unlinked project fails in the call the caller can still see rather than
	// inside a job nobody is watching.
	if _, _, err := v.youtrackClient(ctx, p.Project); err != nil {
		return YouTrackKBJobResult{}, err
	}

	paths := make([]string, 0, len(pages))
	for _, page := range pages {
		paths = append(paths, page.Path)
	}
	job := YouTrackKBParams{
		Project: p.Project, Path: p.cleanPath(), Recursive: p.Recursive,
	}
	// The coalescing key is the selection itself, so that two clicks on the
	// same folder queue one job and two different folders queue two.
	id, err := enqueue(ctx, YouTrackJob{
		Kind: kind, Key: kbCoalesceKey(p), Project: p.Project, Payload: job,
	})
	if err != nil {
		return YouTrackKBJobResult{}, failf("unavailable", "queue %s: %v", kind, err)
	}
	return YouTrackKBJobResult{Project: p.Project, JobID: id, Pages: paths}, nil
}

// kbCoalesceKey is the key two queued jobs are folded on: the selection, not
// the moment it was asked for.
func kbCoalesceKey(p YouTrackKBParams) string {
	key := p.Project + ":" + p.cleanPath()
	if p.Recursive {
		key += ":recursive"
	}
	return key
}

// ------------------------------------------------------------- selection ----

// youtrackKBSelect resolves the parameters to the pages they address and builds
// the mapping options the transform needs. The caller holds the vault lock.
//
// A path naming a page selects that page; a path naming a folder selects the
// pages directly inside it, or every page below it when Recursive is set. An
// empty path selects the project's whole documentation folder.
func (v *Vault) youtrackKBSelect(
	p YouTrackKBParams,
) ([]*core.KBPage, mapping.PageOptions, error) {
	opts := mapping.PageOptions{
		Options: mapping.Options{SyncedAt: core.NewTimestamp(v.now())},
		Pages:   v.youtrackPageIndex(),
	}
	prefix := p.cleanPath()
	if prefix != "" {
		if page, ok := v.index.Page(prefix); ok {
			return []*core.KBPage{page}, opts, nil
		}
	}
	if prefix == "" {
		if docs := v.docsPathOf(core.ProjectKey(p.Project)); docs != "" && docs != "." {
			prefix = docs
		}
	}
	var out []*core.KBPage
	for _, page := range v.index.Pages() {
		if p.Project != "" && page.Project != core.ProjectKey(p.Project) {
			continue
		}
		if !kbUnder(page.Path, prefix, p.Recursive) {
			continue
		}
		out = append(out, page)
	}
	return out, opts, nil
}

// kbUnder reports whether a page path belongs to a folder selection.
func kbUnder(pagePath, prefix string, recursive bool) bool {
	if prefix == "" {
		return true
	}
	rest, inside := strings.CutPrefix(pagePath, prefix+"/")
	if !inside {
		return false
	}
	if recursive {
		return true
	}
	return !strings.Contains(rest, "/")
}

// youtrackPageIndex builds the index the mapping resolves wikilinks through:
// every indexed page, with the article id of the ones already published. The
// caller holds the vault lock.
func (v *Vault) youtrackPageIndex() *mapping.PageIndex {
	pages := v.index.Pages()
	refs := make([]mapping.PageRef, 0, len(pages))
	for _, page := range pages {
		ref := mapping.PageRef{Slug: page.Slug(), Title: page.Title}
		if external, ok := youtrackRefOf(page.ExternalRefs); ok {
			ref.ArticleID, ref.URL = external.ID, external.URL
		}
		refs = append(refs, ref)
	}
	return mapping.NewPageIndex(refs)
}

// youtrackClient resolves the client and the link of one project, turning "no
// provider installed" and "project not linked" into the two errors every
// surface reports the same way.
func (v *Vault) youtrackClient(
	ctx context.Context, project string,
) (YouTrackSource, YouTrackLink, error) {
	provider := v.youtrackProvider()
	if provider == nil {
		return nil, YouTrackLink{}, failf("unavailable",
			"this host has no YouTrack client: the credentials live in the companion process")
	}
	source, link, err := provider(ctx, project)
	if err != nil {
		return nil, YouTrackLink{}, err
	}
	if source == nil {
		return nil, YouTrackLink{}, failf("unavailable",
			"project %q has no YouTrack client installed", project)
	}
	return source, link, nil
}

// ---------------------------------------------------------------- shared ----

// youtrackRefOf returns the YouTrack entry of an `external` list.
func youtrackRefOf(refs []core.External) (core.External, bool) {
	for _, ref := range refs {
		if strings.EqualFold(ref.System, mapping.System) && strings.TrimSpace(ref.ID) != "" {
			return ref, true
		}
	}
	return core.External{}, false
}

// kbFingerprint is the value recorded in the `key` of a page's external entry
// at the end of a successful synchronization: the revision of the article
// content, with surrounding blank lines ignored.
//
// It is taken over the content that actually crosses the boundary — front
// matter stripped, feedback block stripped, title in the summary — so the same
// value is computable from the page and from the article, which is what lets
// status tell "I changed it" from "they changed it" without keeping a copy of
// anything.
func kbFingerprint(content string) string {
	return string(core.ComputeRev([]byte(strings.TrimSpace(content))))
}
