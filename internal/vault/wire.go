package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file holds the wire types of the bridge: the exact JSON shapes declared
// by the CoreApi method map in web/src/core-bridge/api.ts. They are deliberately
// separate from the core model so that a rename inside internal/core cannot
// silently change the contract the web app compiles against; every field the
// contract does not declare is additive and optional.

// stringList decodes both spellings the contract allows for a repeated filter
// field: a bare string and an array of strings.
type stringList []string

// UnmarshalJSON accepts `"todo"`, `["todo","doing"]` and null.
func (s *stringList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*s = nil
		return nil
	}
	if trimmed[0] == '[' {
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("decode string list: %w", err)
		}
		*s = list
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err != nil {
		return fmt.Errorf("decode string list: %w", err)
	}
	if one == "" {
		*s = nil
		return nil
	}
	*s = []string{one}
	return nil
}

// File is one file pushed into, or reported out of, the in-memory vault.
type File struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// WriteSet is what the host must persist after a mutating call.
type WriteSet struct {
	Written []File   `json:"written"`
	Removed []string `json:"removed"`
}

// vaultLoadParams replaces the whole in-memory vault.
type vaultLoadParams struct {
	Files     []File `json:"files"`
	RootLabel string `json:"rootLabel,omitempty"`
}

// fileEventParams is one incremental change, carrying the new text so that the
// core never has to call back into an asynchronous browser API.
type fileEventParams struct {
	Op   string `json:"op"`
	Path string `json:"path"`
	Text string `json:"text,omitempty"`
	From string `json:"from,omitempty"`
}

// vaultApplyParams is a batch of incremental changes.
type vaultApplyParams struct {
	Events []fileEventParams `json:"events"`
}

// IndexStats is the contract's IndexStats. The fields after Diagnostics are
// additive: they carry what core.IndexStats knows and the UI can use, without
// changing the shape the contract declares.
type IndexStats struct {
	Projects    int               `json:"projects"`
	Items       int               `json:"items"`
	Pages       int               `json:"pages"`
	Comments    int               `json:"comments"`
	DurationMs  float64           `json:"durationMs"`
	Fingerprint string            `json:"fingerprint"`
	Diagnostics []core.Diagnostic `json:"diagnostics"`

	Files    int                   `json:"files"`
	Parsed   int                   `json:"parsed"`
	Full     bool                  `json:"full"`
	Errors   int                   `json:"errors"`
	Warnings int                   `json:"warnings"`
	ByType   map[core.ItemType]int `json:"byType,omitempty"`
	BuiltAt  string                `json:"builtAt,omitempty"`
	// Delta is present on vault.apply only: exactly what changed, so the UI can
	// invalidate the affected queries instead of everything.
	Delta *core.IndexDelta `json:"delta,omitempty"`
}

// newIndexStats projects a core build summary onto the wire shape.
func newIndexStats(s core.IndexStats, fingerprint string, diags []core.Diagnostic) IndexStats {
	if diags == nil {
		diags = []core.Diagnostic{}
	}
	out := IndexStats{
		Projects:    s.Projects,
		Items:       s.Items,
		Pages:       s.Pages,
		Comments:    s.Comments,
		DurationMs:  float64(s.Duration) / float64(time.Millisecond),
		Fingerprint: fingerprint,
		Diagnostics: diags,
		Files:       s.Files,
		Parsed:      s.Parsed,
		Full:        s.Full,
		Errors:      s.Errors,
		Warnings:    s.Warnings,
		ByType:      s.ByType,
		BuiltAt:     s.BuiltAt.String(),
	}
	return out
}

// snapshotBlob is the serialized index the IndexedDB cache stores.
type snapshotBlob struct {
	Fingerprint string `json:"fingerprint"`
	JSON        string `json:"json"`
}

// statusSummary is one workflow status of a project.
type statusSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Terminal bool   `json:"terminal,omitempty"`
	WIP      int    `json:"wip,omitempty"`
}

// labelSummary is one entry of a project's label catalog.
type labelSummary struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// projectSummary is everything the UI needs to render a project's chrome.
type projectSummary struct {
	Key        string                `json:"key"`
	Name       string                `json:"name"`
	DocsPath   string                `json:"docsPath"`
	Statuses   []statusSummary       `json:"statuses"`
	Labels     []labelSummary        `json:"labels"`
	Priorities []core.Priority       `json:"priorities"`
	ItemCounts map[core.ItemType]int `json:"itemCounts"`

	BacklogPath string            `json:"backlogPath,omitempty"`
	Writable    bool              `json:"writable"`
	Diagnostics []core.Diagnostic `json:"diagnostics,omitempty"`

	Workflow     *workflowSummary     `json:"workflow,omitempty"`
	Estimation   *estimationSummary   `json:"estimation,omitempty"`
	CustomFields []customFieldSummary `json:"customFields,omitempty"`
}

// workflowSummary exposes the parts of the workflow the editor needs beyond the
// status list: the initial status and the transition map.
type workflowSummary struct {
	Initial     string              `json:"initial,omitempty"`
	Transitions map[string][]string `json:"transitions,omitempty"`
}

// estimationSummary mirrors project.yaml `estimation`.
type estimationSummary struct {
	Scale      string    `json:"scale,omitempty"`
	Values     []float64 `json:"values,omitempty"`
	TrackHours bool      `json:"trackHours"`
}

// customFieldSummary mirrors one entry of project.yaml `custom_fields`.
type customFieldSummary struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Values      []string `json:"values,omitempty"`
	Items       string   `json:"items,omitempty"`
	AppliesTo   []string `json:"appliesTo,omitempty"`
	Default     any      `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
}

// itemFilterParams is the contract's ItemFilter.
type itemFilterParams struct {
	Project        string     `json:"project,omitempty"`
	Type           stringList `json:"type,omitempty"`
	Status         stringList `json:"status,omitempty"`
	Category       stringList `json:"category,omitempty"`
	Priority       stringList `json:"priority,omitempty"`
	Assignee       string     `json:"assignee,omitempty"`
	Label          stringList `json:"label,omitempty"`
	Parent         string     `json:"parent,omitempty"`
	Milestone      string     `json:"milestone,omitempty"`
	UpdatedSince   string     `json:"updatedSince,omitempty"`
	Text           string     `json:"text,omitempty"`
	IncludeDeleted bool       `json:"includeDeleted,omitempty"`
	Sort           string     `json:"sort,omitempty"`
	Order          string     `json:"order,omitempty"`
	Limit          int        `json:"limit,omitempty"`
	Cursor         string     `json:"cursor,omitempty"`
	Fields         []string   `json:"fields,omitempty"`
}

// itemPage is one page of items plus its continuation token.
type itemPage struct {
	Items      []core.Item `json:"items"`
	NextCursor string      `json:"nextCursor,omitempty"`
	Total      int         `json:"total"`
}

// itemDraftParams is the contract's ItemDraft: a new item plus the project it
// belongs to.
type itemDraftParams struct {
	Project   string         `json:"project,omitempty"`
	Type      core.ItemType  `json:"type"`
	Title     string         `json:"title"`
	Status    core.Status    `json:"status,omitempty"`
	Priority  core.Priority  `json:"priority,omitempty"`
	Parent    core.ItemID    `json:"parent,omitempty"`
	Milestone core.ItemID    `json:"milestone,omitempty"`
	Assignees []string       `json:"assignees,omitempty"`
	Author    string         `json:"author,omitempty"`
	Labels    []string       `json:"labels,omitempty"`
	Estimate  *float64       `json:"estimate,omitempty"`
	Due       core.Date      `json:"due,omitempty"`
	Links     []core.Link    `json:"links,omitempty"`
	Custom    map[string]any `json:"custom,omitempty"`
	Body      string         `json:"body,omitempty"`
}

// draft turns the wire form into the core input.
func (p itemDraftParams) draft() core.ItemDraft {
	return core.ItemDraft{
		Type:      p.Type,
		Title:     p.Title,
		Status:    p.Status,
		Priority:  p.Priority,
		Parent:    p.Parent,
		Milestone: p.Milestone,
		Assignees: p.Assignees,
		Author:    p.Author,
		Labels:    p.Labels,
		Estimate:  p.Estimate,
		Due:       p.Due,
		Links:     p.Links,
		Custom:    p.Custom,
		Body:      p.Body,
	}
}

// patchSet is the `set` half of the contract's ItemPatch: a sparse Item where a
// present key means "assign this value".
type patchSet struct {
	Title     *string        `json:"title,omitempty"`
	Status    *core.Status   `json:"status,omitempty"`
	Priority  *core.Priority `json:"priority,omitempty"`
	Parent    *core.ItemID   `json:"parent,omitempty"`
	Milestone *core.ItemID   `json:"milestone,omitempty"`
	Sprint    *string        `json:"sprint,omitempty"`
	Assignees *[]string      `json:"assignees,omitempty"`
	Author    *string        `json:"author,omitempty"`
	Owner     *string        `json:"owner,omitempty"`
	Labels    *[]string      `json:"labels,omitempty"`
	Estimate  *float64       `json:"estimate,omitempty"`
	Effort    *float64       `json:"effort,omitempty"`
	Spent     *float64       `json:"spent,omitempty"`
	Start     *core.Date     `json:"start,omitempty"`
	Due       *core.Date     `json:"due,omitempty"`
	Links     *[]core.Link   `json:"links,omitempty"`
	Custom    map[string]any `json:"custom,omitempty"`
	Deleted   *bool          `json:"deleted,omitempty"`
}

// itemPatchParams is the contract's ItemPatch.
type itemPatchParams struct {
	Set   *patchSet `json:"set,omitempty"`
	Unset []string  `json:"unset,omitempty"`
	Body  *string   `json:"body,omitempty"`
}

// patch turns the wire form into the core input.
func (p itemPatchParams) patch() core.ItemPatch {
	out := core.ItemPatch{Unset: p.Unset, Body: p.Body}
	if p.Set == nil {
		return out
	}
	s := p.Set
	out.Title, out.Status, out.Priority = s.Title, s.Status, s.Priority
	out.Parent, out.Milestone, out.Sprint = s.Parent, s.Milestone, s.Sprint
	out.Author, out.Owner = s.Author, s.Owner
	out.Assignees, out.Labels, out.Links = s.Assignees, s.Labels, s.Links
	out.Estimate, out.Effort, out.Spent = s.Estimate, s.Effort, s.Spent
	out.Start, out.Due = s.Start, s.Due
	out.Custom, out.Deleted = s.Custom, s.Deleted
	return out
}

// kbNode is one node of the knowledge-base tree.
type kbNode struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title,omitempty"`
	Children []kbNode `json:"children,omitempty"`
}

// kbPageResult is one knowledge-base page with its link neighborhood.
type kbPageResult struct {
	Path        string         `json:"path"`
	Title       string         `json:"title"`
	FrontMatter map[string]any `json:"frontMatter"`
	Body        string         `json:"body"`
	Rev         string         `json:"rev"`
	Outgoing    []string       `json:"outgoing"`
	Backlinks   []string       `json:"backlinks"`

	Project string `json:"project,omitempty"`
	RelPath string `json:"relPath,omitempty"`
}

// searchHit is one ranked search result.
type searchHit struct {
	Kind    string  `json:"kind"`
	ID      string  `json:"id,omitempty"`
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`

	Project string `json:"project,omitempty"`
}
