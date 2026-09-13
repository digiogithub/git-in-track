package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// GET /api/v1/youtrack/issues, story GIT-US-0054.
//
// The import dialog types into this endpoint. It is therefore a *search*, not a
// cache: nothing here is written to the index, and the remote is the authority
// on what matches. Three rules make it safe to call on a keystroke:
//
//  1. **The query always carries an ordering clause.** youtrack.ProjectQuery
//     scopes the search to the linked project and appends
//     `order by: created asc`, without which a $skip walk over a live instance
//     silently skips and duplicates rows between two keystrokes.
//
//  2. **The page is bounded twice.** The caller's `limit` is clamped here and
//     the client's own rate limiter throttles the instance-facing half, so a
//     fast typist cannot exhaust the instance's budget.
//
//  3. **`linked` is resolved locally.** Whether an issue has already been
//     imported is a fact about this repository, so it is answered from the
//     index by (external.system, external.id) rather than with a second
//     round trip.
//
// The route is companion-only. YouTrack is never reached through the CORS proxy
// (ADR-025): the proxy speaks git smart-HTTP to an allow-list and generalising
// it is exactly what that decision refuses.

// The bounds of one page of search results.
const (
	// defaultIssuesPerPage is the page an autosuggest gets when it asks for
	// none: enough to fill a dropdown, small enough to be one cheap request.
	defaultIssuesPerPage = 50
	// maxIssuesPerPage bounds what a caller may ask for.
	maxIssuesPerPage = 200
)

// The presets the import dialog offers. Four of them are issue-query clauses;
// `versions` is not a query at all and is answered from the project's version
// bundle, which is why it is handled apart.
const (
	presetEpics      = "epics"
	presetStories    = "stories"
	presetTasks      = "tasks"
	presetVersions   = "versions"
	presetUnresolved = "unresolved"
)

// presetClauses is the query fragment each issue preset adds.
//
// Braces are YouTrack's quoting for a value that may contain spaces. They are
// applied wherever a value could contain one rather than only where it does,
// because a project that renamed "User Story" is not a special case.
var presetClauses = map[string]string{
	presetEpics:      "Type: Epic",
	presetStories:    "Type: {User Story}",
	presetTasks:      "Type: Task",
	presetUnresolved: "#Unresolved",
}

// youtrackPresets lists the presets in the order the dialog shows them, for the
// error message and for the documentation.
func youtrackPresets() []string {
	return []string{presetEpics, presetStories, presetTasks, presetVersions, presetUnresolved}
}

// youtrackIssueRow is one row of the answer.
//
// It is a projection, not the issue: an import picker renders a title, a type,
// a state and an assignee, and everything else an issue carries is weight on
// the wire and a decision the mapping layer makes later, not here.
type youtrackIssueRow struct {
	// ID is the internal id and IDReadable the human one, "ACME-42".
	ID         string `json:"id"`
	IDReadable string `json:"idReadable"`
	// Summary is the issue title, or the version name for a `versions` row.
	Summary string `json:"summary"`
	// Type is the YouTrack issue type, or the literal "version" for a row that
	// came from a version bundle rather than from a query.
	Type string `json:"type,omitempty"`
	// State and Assignee are rendered as the instance presents them.
	State    string `json:"state,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	// Updated is when the issue last changed, RFC 3339, empty when the
	// instance sent none.
	Updated string `json:"updated,omitempty"`
	// URL addresses the issue, or the version bundle value, on the instance.
	URL string `json:"url,omitempty"`
	// Released, Archived and ReleaseDate are filled for a `versions` row only.
	Released    bool   `json:"released,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	// Linked names the git-in-track item that already mirrors this issue, and
	// is null when nothing does. It is what makes the picker able to say
	// "already imported" without a second call.
	Linked *youtrackLinkedItem `json:"linked"`
}

// youtrackLinkedItem is the local item an issue was imported as.
type youtrackLinkedItem struct {
	ItemID string `json:"itemId"`
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
	Title  string `json:"title,omitempty"`
}

// youtrackIssuesPage is the answer of GET /api/v1/youtrack/issues.
type youtrackIssuesPage struct {
	// ProjectKey is the git-in-track project the search ran for, and Project
	// the YouTrack short name it was scoped to.
	ProjectKey string `json:"projectKey"`
	Project    string `json:"project,omitempty"`
	// Query is the composed query, exactly as it was sent. It is echoed so a
	// user can see what their typing became and a support case can reproduce
	// it; it carries no credential.
	Query string `json:"query,omitempty"`
	// Preset is the preset that was applied, empty when none was.
	Preset string `json:"preset,omitempty"`
	// Items are the matching rows, in the instance's stable order.
	Items []youtrackIssueRow `json:"items"`
	// NextCursor resumes the walk; empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
	// Limit is the page size that was applied after clamping.
	Limit int `json:"limit"`
}

// handleYouTrackIssues serves GET /api/v1/youtrack/issues.
func (s *Server) handleYouTrackIssues(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackProjectKey(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	preset := strings.TrimSpace(query.Get("preset"))
	if preset != "" && preset != presetVersions && presetClauses[preset] == "" {
		failProblem(w, r, codeInvalidRequest,
			"Unknown preset "+preset+": use "+strings.Join(youtrackPresets(), ", ")+".")
		return
	}
	limit, ok := issuesLimit(w, r, query.Get("limit"))
	if !ok {
		return
	}
	skip, ok := issuesCursor(w, r, query.Get("cursor"))
	if !ok {
		return
	}

	client, link, err := s.youtrack.clientFor(key)
	if err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), youtrackProbeTimeout)
	defer cancel()

	page := youtrackIssuesPage{ProjectKey: key, Project: link.Project, Preset: preset, Limit: limit}
	if preset == presetVersions {
		if err := s.fillVersionRows(ctx, client, link, &page, query.Get("archived") == "true"); err != nil {
			s.failYouTrack(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, page)
		return
	}
	if err := s.fillIssueRows(ctx, client, link, &page, query.Get("q"), preset, skip); err != nil {
		s.failYouTrack(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, page)
}

// fillIssueRows runs one page of an issue search and projects it.
func (s *Server) fillIssueRows(
	ctx context.Context, client *youtrack.Client, link *config.YouTrackLink,
	page *youtrackIssuesPage, q, preset string, skip int,
) error {
	page.Query = composeIssueQuery(link.Project, q, preset)
	issues, err := client.SearchIssuesWithFields(ctx, page.Query,
		youtrack.Page{Top: page.Limit, Skip: skip}, youtrack.IssueFields)
	if err != nil {
		return err //nolint:wrapcheck // failYouTrack classifies the client's own error and never echoes its cause
	}
	fields := youtrackFieldMapping(link)
	linked := s.linkedItems(page.ProjectKey)
	page.Items = make([]youtrackIssueRow, 0, len(issues))
	for _, issue := range issues {
		page.Items = append(page.Items, issueRow(issue, fields, link.URL, linked))
	}
	// A full page means there may be another; a short one ends the walk. The
	// cursor is opaque so that the $skip behind it stays an implementation
	// detail the client cannot come to depend on.
	if len(issues) == page.Limit {
		page.NextCursor = encodeIssuesCursor(skip + page.Limit)
	}
	return nil
}

// composeIssueQuery builds the query one search sends.
//
// The project clause, the brace quoting and the ordering all come from
// youtrack.ProjectQuery, which is where they belong: composing them by hand in
// a handler is how an unordered $skip walk gets shipped.
func composeIssueQuery(project, q, preset string) string {
	parts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(q); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if clause := presetClauses[preset]; clause != "" {
		parts = append(parts, clause)
	}
	extra := strings.Join(parts, " ")
	if strings.TrimSpace(project) == "" {
		return youtrack.EnsureOrderBy(extra)
	}
	return youtrack.ProjectQuery(project, extra)
}

// issueRow projects one issue onto a picker row.
func issueRow(
	issue youtrack.Issue, fields mapping.FieldMap, baseURL string,
	linked map[core.ExternalRef]youtrackLinkedItem,
) youtrackIssueRow {
	id := strings.TrimSpace(issue.IDReadable)
	row := youtrackIssueRow{
		ID:         issue.ID,
		IDReadable: id,
		Summary:    issue.Summary,
		Type:       customFieldText(issue, fields.TypeField),
		State:      customFieldText(issue, fields.StateField),
		Assignee:   customFieldText(issue, fields.AssigneeField),
		URL:        issueURL(baseURL, id),
	}
	if !issue.Updated.IsZero() {
		row.Updated = issue.Updated.Time().UTC().Format(time.RFC3339)
	}
	if item, ok := linked[core.NewExternalRef(mapping.System, id)]; ok {
		row.Linked = &item
	}
	return row
}

// customFieldText renders one custom field as the single line a picker shows.
//
// The reduction is deliberately lossy and deliberately local: the mapping layer
// decides what a field *means* when an issue is imported, and repeating that
// here would be a second, quietly diverging copy of the rules. All this needs
// is something to print.
func customFieldText(issue youtrack.Issue, name string) string {
	field, ok := issue.CustomField(name)
	if !ok {
		return ""
	}
	values, err := field.Value.Values()
	if err != nil || len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if text := fieldValueText(value); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, ", ")
}

// fieldValueText picks the most human of the names a field value carries.
func fieldValueText(value youtrack.FieldValue) string {
	for _, candidate := range []string{
		value.LocalizedName, value.Name, value.FullName, value.Login,
		value.Presentation, value.IDReadable, value.Text,
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	if len(value.Scalar) > 0 {
		return strings.Trim(strings.TrimSpace(string(value.Scalar)), `"`)
	}
	return ""
}

// issueURL addresses one issue on an instance.
func issueURL(baseURL, id string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" || id == "" {
		return ""
	}
	return base + "/issue/" + id
}

// linkedItems maps every YouTrack reference this project's backlog claims onto
// the item that claims it.
//
// It is built once per request from the index rather than queried per row: a
// picker shows fifty rows and fifty index lookups through the dispatch layer
// would be fifty JSON round trips inside one process.
func (s *Server) linkedItems(projectKey string) map[core.ExternalRef]youtrackLinkedItem {
	out := map[core.ExternalRef]youtrackLinkedItem{}
	m, ok := s.repos.forProject(projectKey)
	if !ok || !m.ready() {
		return out
	}
	for _, item := range m.vlt.Items() {
		for _, ref := range item.External {
			if ref.Ref().System != mapping.System || ref.ID == "" {
				continue
			}
			out[ref.Ref()] = youtrackLinkedItem{
				ItemID: string(item.ID), Type: string(item.Type),
				Status: string(item.Status), Title: item.Title,
			}
		}
	}
	return out
}

// fillVersionRows answers the `versions` preset from the project's version
// bundle.
//
// A version is not an issue and there is no query that returns one: the bundle
// behind the milestone field is listed instead, and the rows are returned in
// the same envelope so the picker renders one list rather than two.
func (s *Server) fillVersionRows(
	ctx context.Context, client *youtrack.Client, link *config.YouTrackLink,
	page *youtrackIssuesPage, includeArchived bool,
) error {
	bundle, err := s.versionBundleID(ctx, client, link)
	if err != nil {
		return err
	}
	values, err := client.VersionBundleValues(ctx, bundle)
	if err != nil {
		return err //nolint:wrapcheck // failYouTrack classifies the client's own error and never echoes its cause
	}
	linked := s.linkedItems(page.ProjectKey)
	page.Items = make([]youtrackIssueRow, 0, len(values))
	for _, value := range values {
		if value.Archived && !includeArchived {
			continue
		}
		row := youtrackIssueRow{
			ID: value.ID, IDReadable: value.Name, Summary: value.Name,
			Type: "version", Released: value.Released, Archived: value.Archived,
		}
		if !value.ReleaseDate.IsZero() {
			row.ReleaseDate = value.ReleaseDate.Time().UTC().Format("2006-01-02")
		}
		if item, ok := linked[core.NewExternalRef(mapping.System, value.Name)]; ok {
			row.Linked = &item
		}
		page.Items = append(page.Items, row)
	}
	sort.SliceStable(page.Items, func(i, j int) bool {
		return page.Items[i].Summary < page.Items[j].Summary
	})
	return nil
}

// versionBundleID resolves the bundle behind the project's milestone field.
//
// The field is the one the project's field map names, defaulting to
// "Fix versions": a project that renamed it says so in project.yaml, and a
// project that has no such field at all is a clear failure rather than an empty
// list, because an empty list would read as "this project has no versions".
func (s *Server) versionBundleID(
	ctx context.Context, client *youtrack.Client, link *config.YouTrackLink,
) (string, error) {
	settings, err := client.CustomFieldSettings(ctx, link.Project)
	if err != nil {
		return "", err //nolint:wrapcheck // failYouTrack classifies the client's own error and never echoes its cause
	}
	want := youtrackFieldMapping(link).MilestoneField
	for _, setting := range settings {
		if strings.EqualFold(strings.TrimSpace(setting.Field.Name), want) && setting.Bundle.ID != "" {
			return setting.Bundle.ID, nil
		}
	}
	return "", fmt.Errorf("%w: project %s has no %q field with a version bundle",
		youtrack.ErrInvalidInput, link.Project, want)
}

// --------------------------------------------------------------- paging ---

// issuesLimit reads and clamps the page size.
func issuesLimit(w http.ResponseWriter, r *http.Request, raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return defaultIssuesPerPage, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		failProblem(w, r, codeInvalidRequest, "limit must be a positive whole number.")
		return 0, false
	}
	return min(n, maxIssuesPerPage), true
}

// issuesCursor decodes the opaque cursor into the $skip it stands for.
func issuesCursor(w http.ResponseWriter, r *http.Request, raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		failProblem(w, r, codeInvalidRequest, "The cursor is not one this endpoint issued; start the walk again.")
		return 0, false
	}
	skip, err := strconv.Atoi(string(decoded))
	if err != nil || skip < 0 {
		failProblem(w, r, codeInvalidRequest, "The cursor is not one this endpoint issued; start the walk again.")
		return 0, false
	}
	return skip, true
}

// encodeIssuesCursor renders a $skip as the opaque cursor a client resumes
// from. It is deliberately opaque: the paging behind it is this endpoint's to
// change, and a client that learned to do arithmetic on it would pin it.
func encodeIssuesCursor(skip int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(skip)))
}
