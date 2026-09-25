package mcp

import (
	"context"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The spec tools: create a spec, and list, create and update one requirement
// of it at a time (ADR-037, GIT-US-0122). A requirement is addressed by its ref
// (ACME-SP-0003.R2) and locked by its requirement rev, so an agent reads and
// writes only the block it owns and never races a sibling requirement of the
// same spec. Everything below is a shim over the vault's requirement.* methods
// (docs/07 section 6.7); allocation, validation and rev computation stay in
// internal/core.

// -------------------------------------------------------------- wire shape --

// Requirement is one requirement as a tool returns it. `ref` and `rev` are the
// two fields no projection drops: the first addresses it, the second is the
// write token update_requirement quotes.
type Requirement struct {
	Ref string `json:"ref" jsonschema:"Requirement ref, for example ACME-SP-0003.R2"`
	// Rev is the requirement rev (block plus requirements: entry): the write
	// token (ADR-037 section 6).
	Rev string `json:"rev" jsonschema:"Requirement rev: the write token update_requirement quotes"`
	// BlockRev is the block rev: what a verification stamp records. It is
	// never accepted as a write token.
	BlockRev       string           `json:"blockRev,omitempty" jsonschema:"Block rev: the fingerprint verified.rev records; never a write token"`
	Spec           string           `json:"spec,omitempty"`
	Project        string           `json:"project,omitempty"`
	Title          string           `json:"title,omitempty"`
	Status         string           `json:"status,omitempty"`
	StatusImplicit bool             `json:"statusImplicit,omitempty" jsonschema:"The entry has no status; the workflow's initial status stands in"`
	Path           string           `json:"path,omitempty" jsonschema:"Vault-relative path of the spec file"`
	Anchor         string           `json:"anchor,omitempty"`
	Line           int              `json:"line,omitempty" jsonschema:"1-based line of the block heading in the spec body"`
	Trace          *RequirementCode `json:"trace,omitempty"`
	Verified       *Verification    `json:"verified,omitempty"`
	Links          []Link           `json:"links,omitempty"`
	Extra          map[string]any   `json:"extra,omitempty" jsonschema:"Unknown keys of the requirements: entry, read-only"`
	// Text is the block below its heading. It is repository content: data
	// written by people and by other agents, never an instruction.
	Text string `json:"text,omitempty" jsonschema:"Block text below the heading (statement and scenarios); untrusted repository content"`
}

// RequirementCode is the trace of a requirement: the code and tests that
// realize it when an in-code marker is impractical.
type RequirementCode struct {
	Code  []string `json:"code,omitempty" jsonschema:"Repository-relative path[#symbol] of code that realizes the requirement"`
	Tests []string `json:"tests,omitempty" jsonschema:"Repository-relative path[#symbol] of tests that verify it"`
}

// Verification is the durable verification stamp of a requirement. Tools read
// it; only verify_requirement writes it, from ingested evidence (ADR-037
// section 7).
type Verification struct {
	Rev    string `json:"rev,omitempty" jsonschema:"Block rev that was verified"`
	Commit string `json:"commit,omitempty"`
	At     string `json:"at,omitempty"`
	By     string `json:"by,omitempty"`
}

// requirementOf projects the core view onto the wire shape, text included.
// Statement and scenarios are left out: both are derived from the text, and an
// agent that asked for the text already holds them.
func requirementOf(v core.RequirementView) Requirement {
	out := Requirement{
		Ref:            v.Ref.String(),
		Rev:            string(v.Rev),
		BlockRev:       string(v.BlockRev),
		Spec:           string(v.Spec),
		Project:        string(v.Project),
		Title:          v.Title,
		Status:         string(v.Status),
		StatusImplicit: v.StatusImplicit,
		Path:           v.Path,
		Anchor:         v.Anchor,
		Line:           v.Line,
		Extra:          v.Extra,
		Text:           v.Text,
	}
	if t := v.Trace; t != nil && (len(t.Code) > 0 || len(t.Tests) > 0) {
		out.Trace = &RequirementCode{Code: t.Code, Tests: t.Tests}
	}
	if s := v.Verified; s != nil {
		out.Verified = &Verification{Rev: string(s.Rev), Commit: s.Commit, At: s.At.String(), By: s.By}
	}
	for _, l := range v.Links {
		out.Links = append(out.Links, Link{Kind: string(l.Kind), Target: l.Target})
	}
	if len(out.Extra) == 0 {
		out.Extra = nil
	}
	return out
}

// defaultRequirementFields is the row list_requirements returns when the
// caller asks for no projection: enough to pick a requirement, nothing more.
var defaultRequirementFields = []string{"ref", "spec", "title", "status", "rev"}

// writeRequirementFields is what a requirement write answers with: both hashes
// and the entry, never the text the agent just sent.
var writeRequirementFields = []string{"spec", "title", "status", "blockRev", "trace", "verified", "links"}

// projectRequirement keeps the requested fields. `ref` and `rev` always
// survive; an unknown field name is ignored, as projectItem does.
func projectRequirement(r Requirement, fields []string) Requirement {
	if len(fields) == 0 {
		fields = defaultRequirementFields
	}
	want := make(map[string]bool, len(fields))
	for _, f := range fields {
		want[strings.ToLower(strings.TrimSpace(f))] = true
	}
	out := Requirement{Ref: r.Ref, Rev: r.Rev}
	keep := func(name string, apply func()) {
		if want[strings.ToLower(name)] {
			apply()
		}
	}
	keep("blockRev", func() { out.BlockRev = r.BlockRev })
	keep("spec", func() { out.Spec = r.Spec })
	keep("project", func() { out.Project = r.Project })
	keep("title", func() { out.Title = r.Title })
	keep("status", func() { out.Status, out.StatusImplicit = r.Status, r.StatusImplicit })
	keep("path", func() { out.Path = r.Path })
	keep("anchor", func() { out.Anchor = r.Anchor })
	keep("line", func() { out.Line = r.Line })
	keep("trace", func() { out.Trace = r.Trace })
	keep("verified", func() { out.Verified = r.Verified })
	keep("links", func() { out.Links = r.Links })
	keep("extra", func() { out.Extra = r.Extra })
	keep("text", func() { out.Text = r.Text })
	return out
}

// ------------------------------------------------------------------ input ---

// ListRequirementsInput filters the requirements of the workspace's specs.
type ListRequirementsInput struct {
	Project string   `json:"project,omitempty" jsonschema:"Project key, for example ACME"`
	Spec    string   `json:"spec,omitempty" jsonschema:"Spec id, for example ACME-SP-0003"`
	Status  []string `json:"status,omitempty" jsonschema:"Workflow statuses declared by the project"`
	Text    string   `json:"text,omitempty" jsonschema:"Keep requirements whose ref, title or text hold every word"`
	Limit   int      `json:"limit,omitempty" jsonschema:"Page size, 1 to 100; default 20"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"nextCursor from the previous page"`
	Fields  []string `json:"fields,omitempty" jsonschema:"Fields to project: spec, project, title, status, blockRev, path, anchor, line, trace, verified, links, extra, text. ref and rev are always returned; default ref, spec, title, status, rev"`
}

// RequirementPage is one page of requirement rows.
type RequirementPage struct {
	Requirements []Requirement `json:"requirements"`
	Total        int           `json:"total" jsonschema:"Number of requirements the filter matches"`
	NextCursor   string        `json:"nextCursor,omitempty" jsonschema:"Pass back as cursor to fetch the next page"`
}

// CreateSpecInput is the draft of a new spec. A spec is a living description,
// not scheduled work, so it has no parent, milestone, estimate or due date.
type CreateSpecInput struct {
	Project   string   `json:"project,omitempty" jsonschema:"Project key; required when the workspace holds more than one"`
	Title     string   `json:"title" jsonschema:"The capability the spec describes"`
	Body      string   `json:"body,omitempty" jsonschema:"Markdown body: ## Purpose, ## Scope, ## Requirements; add requirements with create_requirement"`
	Status    string   `json:"status,omitempty" jsonschema:"A status the project declares, never a triage one; default is the initial status of its workflow"`
	Priority  string   `json:"priority,omitempty"`
	Assignees []string `json:"assignees,omitempty" jsonschema:"Owners of the spec"`
	Labels    []string `json:"labels,omitempty"`
	Author    string   `json:"author,omitempty" jsonschema:"Defaults to the agent name the server was started with"`
}

// CreateRequirementInput is a new requirement of an existing spec.
type CreateRequirementInput struct {
	Spec   string           `json:"spec" jsonschema:"Spec id, for example ACME-SP-0003"`
	Title  string           `json:"title" jsonschema:"One line, 1 to 200 characters"`
	Text   string           `json:"text,omitempty" jsonschema:"Block text below the heading: the SHALL statement and its #### Scenario sections; no level 1-3 heading"`
	Status string           `json:"status,omitempty" jsonschema:"Default is the initial status of the project workflow"`
	Trace  *RequirementCode `json:"trace,omitempty"`
	Links  []Link           `json:"links,omitempty" jsonschema:"Requirement-level links: supersedes, superseded_by or relates_to"`
}

// UpdateRequirementInput is a sparse patch of one requirement: only the keys
// present change, and only this requirement's block and entry are touched.
type UpdateRequirementInput struct {
	Ref string `json:"ref" jsonschema:"Requirement ref, for example ACME-SP-0003.R2"`
	// Rev is the requirement rev, never the block rev: the block rev does not
	// move on a status or trace change, so it cannot detect a lost update.
	Rev    string           `json:"rev" jsonschema:"Required. The requirement rev (rev, not blockRev) of the read this change is based on; \"*\" overwrites unconditionally"`
	Title  string           `json:"title,omitempty" jsonschema:"Rewrites the heading title"`
	Text   *string          `json:"text,omitempty" jsonschema:"Replaces the block text below the heading"`
	Status string           `json:"status,omitempty" jsonschema:"Target status; the project workflow validates the transition"`
	Trace  *RequirementCode `json:"trace,omitempty" jsonschema:"Replaces the trace"`
	Links  []Link           `json:"links,omitempty" jsonschema:"Replaces the requirement-level links"`
	Unset  []string         `json:"unset,omitempty" jsonschema:"Entry keys to clear: trace or links"`
}

// RequirementWriteResult is what a requirement write answers with: the
// requirement as it now stands, without its text, and the files that changed.
type RequirementWriteResult struct {
	Requirement    Requirement `json:"requirement"`
	SpecRev        string      `json:"specRev,omitempty" jsonschema:"New file rev of the spec"`
	Changed        []string    `json:"changed,omitempty" jsonschema:"Vault-relative paths written by this call"`
	SchemaUpgraded int         `json:"schemaUpgraded,omitempty" jsonschema:"Set when this write raised project.yaml's schema"`
	// Similar is set by create_requirement only (GIT-US-0111).
	Similar []SimilarRequirement `json:"similar,omitempty" jsonschema:"create_requirement only: existing requirements that read like the new one, best first; advisory, never blocking, and empty without Pando"`
}

// SimilarRequirement is one near-duplicate create_requirement reports: enough
// to decide whether to keep the new block, without a read.
type SimilarRequirement struct {
	Ref    string  `json:"ref"`
	Title  string  `json:"title"`
	Status string  `json:"status,omitempty"`
	Score  float64 `json:"score" jsonschema:"Semantic score on the backend's own scale; compare only within one list"`
}

// ---------------------------------------------------------------- registry --

// registerSpecTools declares the spec half of the surface.
func registerSpecTools(s *Server) {
	register(s, toolDef{
		Name:  "list_requirements",
		Title: "List spec requirements",
		Description: "List the requirements of the workspace's specs as compact rows (ref, spec, title, " +
			"status and rev by default), filtered by project, spec, status or words. Project more fields " +
			"with fields and walk the cursor; the block text is returned only when fields asks for text. " +
			"Read one requirement with get_item on its ref.",
		Untrusted: true,
	}, listRequirements)

	register(s, toolDef{
		Name:  "create_spec",
		Title: "Create a spec",
		Description: "Create a spec, the living description of one capability whose requirements are " +
			"addressed one at a time as ACME-SP-0003.R2. The id is allocated by the tool; never propose " +
			"one. The first spec of a project raises project.yaml to schema 2 in the same write and " +
			"reports schemaUpgraded: 2.",
		Write:     true,
		Untrusted: true,
	}, createSpec)

	register(s, toolDef{
		Name:  "create_requirement",
		Title: "Add a requirement to a spec",
		Description: "Append one requirement block to a spec. Its number R<n> is allocated by the tool " +
			"and never reused; never propose one. Needs no rev: it touches no requirement that exists. " +
			"Returns the requirement's rev, the token a later update_requirement quotes, and similar[]: " +
			"up to three existing requirements that read like the new one (Pando only; advisory — " +
			"review them and, if one is a duplicate, cancel the new block rather than keep both).",
		Write:     true,
		Untrusted: true,
	}, createRequirement)

	register(s, toolDef{
		Name:  "update_requirement",
		Title: "Update one requirement",
		Description: "Apply a sparse patch to one requirement: its title, block text, status, trace or " +
			"links. Only that block and its requirements: entry change, so a concurrent write to another " +
			"requirement of the same spec neither conflicts nor is lost. rev is required and is the " +
			"requirement rev (rev, never blockRev) the read returned. A rev that is no longer current is " +
			"refused with stale_revision, carrying currentRev and the fields still in conflict; an empty " +
			"conflicts list means your change is already there. A status change follows the project " +
			"workflow. The verification stamp is not writable here: verify_requirement writes it.",
		Write:     true,
		Untrusted: true,
	}, updateRequirement)
}

// ---------------------------------------------------------------- handlers --

// isRequirementRef reports whether get_item was handed a requirement ref
// rather than an item id. Item ids never hold a dot, so the dot is enough to
// route the call; the vault then applies the strict grammar and reports a
// malformed ref as invalid_request.
func isRequirementRef(id string) bool {
	return strings.Contains(strings.TrimSpace(id), ".")
}

// getRequirement answers get_item for a requirement ref: the one block and its
// entry, never the rest of the spec.
func getRequirement(ctx context.Context, s *Server, ref string, fields []string) (ItemResult, error) {
	got, err := dispatch[struct {
		Requirement core.RequirementView `json:"requirement"`
		SpecRev     string               `json:"specRev"`
	}](ctx, s, "requirement.get", map[string]any{"ref": strings.TrimSpace(ref)})
	if err != nil {
		return ItemResult{}, err
	}
	req := requirementOf(got.Requirement)
	if len(fields) > 0 {
		req = projectRequirement(req, fields)
	}
	return ItemResult{Requirement: &req, SpecRev: got.SpecRev}, nil
}

// listRequirements answers a filtered, paginated query over requirement rows.
// The vault answers the whole list at once, so the page is cut here against a
// cursor bound to the filter.
func listRequirements(ctx context.Context, s *Server, in ListRequirementsInput) (RequirementPage, error) {
	limit := boundedLimit(in.Limit)
	filter := fingerprint("list_requirements", in.Project, in.Spec, in.Status, in.Text)
	offset, err := decodeCursor(in.Cursor, filter)
	if err != nil {
		return RequirementPage{}, err
	}
	got, err := dispatch[struct {
		Requirements []core.RequirementView `json:"requirements"`
		Total        int                    `json:"total"`
	}](ctx, s, "requirement.list", map[string]any{
		"project": in.Project,
		"spec":    in.Spec,
		"status":  in.Status,
		"q":       in.Text,
		// The block text is the bulk of a spec: fetch it only when projected.
		"text": includes(in.Fields, "text"),
	})
	if err != nil {
		return RequirementPage{}, err
	}
	page, next := slice(got.Requirements, offset, limit, filter)
	out := RequirementPage{Requirements: make([]Requirement, 0, len(page)), Total: len(got.Requirements), NextCursor: next}
	for _, v := range page {
		out.Requirements = append(out.Requirements, projectRequirement(requirementOf(v), in.Fields))
	}
	return out, nil
}

// createSpec creates a spec item. It is not createTool(core.TypeSpec) because a
// spec carries none of the scheduling fields the other create tools accept.
func createSpec(ctx context.Context, s *Server, in CreateSpecInput) (WriteResult, error) {
	if strings.TrimSpace(in.Title) == "" {
		return WriteResult{}, invalidField("title", "a new spec needs a title", "Checkout address validation")
	}
	draft := map[string]any{
		"project":   in.Project,
		"type":      string(core.TypeSpec),
		"title":     in.Title,
		"body":      in.Body,
		"status":    in.Status,
		"priority":  in.Priority,
		"assignees": in.Assignees,
		"labels":    in.Labels,
		"author":    s.authorName(in.Author),
	}
	result, err := s.dispatchRaw(ctx, "item.create", draft)
	if err != nil {
		return WriteResult{}, err
	}
	out, err := writeResultOf(result)
	if err != nil {
		return WriteResult{}, err
	}
	s.announce(ctx, WriteEvent{
		Tool: "create_spec", Method: "item.create",
		ItemID: out.Item.ID, Op: "created", Result: result,
	})
	return out, nil
}

// createRequirement appends one requirement to a spec.
// Implements: GIT-SP-0002.R7
func createRequirement(ctx context.Context, s *Server, in CreateRequirementInput) (RequirementWriteResult, error) {
	if strings.TrimSpace(in.Spec) == "" {
		return RequirementWriteResult{}, invalidField("spec", "create_requirement needs the spec id", "ACME-SP-0003")
	}
	if strings.TrimSpace(in.Title) == "" {
		return RequirementWriteResult{}, invalidField("title", "a new requirement needs a title",
			"Trim pasted addresses")
	}
	params := map[string]any{
		"spec":   strings.TrimSpace(in.Spec),
		"title":  in.Title,
		"text":   in.Text,
		"status": in.Status,
	}
	if in.Trace != nil {
		params["trace"] = in.Trace
	}
	if len(in.Links) > 0 {
		params["links"] = in.Links
	}
	return requirementWrite(ctx, s, "create_requirement", "requirement.create", params)
}

// updateRequirement applies a sparse patch to one requirement under its
// requirement rev.
func updateRequirement(ctx context.Context, s *Server, in UpdateRequirementInput) (RequirementWriteResult, error) {
	if strings.TrimSpace(in.Ref) == "" {
		return RequirementWriteResult{}, invalidField("ref", "update_requirement needs a requirement ref", "ACME-SP-0003.R2")
	}
	rev, err := requiredRev("rev", in.Rev)
	if err != nil {
		return RequirementWriteResult{}, err
	}
	if rev == "" {
		// requiredRev spells the wildcard as "", which the vault refuses as a
		// missing rev; hand it the explicit waiver instead.
		rev = wildcardRev
	}
	for _, key := range in.Unset {
		if k := strings.TrimSpace(key); k != "trace" && k != "links" {
			return RequirementWriteResult{}, invalidField("unset",
				"update_requirement can clear trace or links; "+k+" is not clearable here", []string{"trace"})
		}
	}
	patch := map[string]any{}
	if in.Title != "" {
		patch["title"] = in.Title
	}
	if in.Text != nil {
		patch["text"] = *in.Text
	}
	if in.Status != "" {
		patch["status"] = in.Status
	}
	if in.Trace != nil {
		patch["trace"] = in.Trace
	}
	if in.Links != nil {
		patch["links"] = in.Links
	}
	if len(in.Unset) > 0 {
		patch["unset"] = in.Unset
	}
	if len(patch) == 0 {
		return RequirementWriteResult{}, invalidField("patch", "update_requirement was given nothing to change",
			map[string]any{"status": "in_progress"})
	}
	return requirementWrite(ctx, s, "update_requirement", "requirement.update",
		map[string]any{"ref": strings.TrimSpace(in.Ref), "rev": rev, "patch": patch})
}

// requirementWrite dispatches a requirement write and projects its answer. The
// requirement comes back without its text — the agent just wrote it — but with
// both hashes: rev is the token the next write quotes.
func requirementWrite(ctx context.Context, s *Server, tool, method string, params map[string]any) (RequirementWriteResult, error) {
	result, err := s.dispatchRaw(ctx, method, params)
	if err != nil {
		return RequirementWriteResult{}, err
	}
	payload, err := decodeResult[struct {
		Requirement    core.RequirementView `json:"requirement"`
		SpecRev        string               `json:"specRev"`
		Writes         writeSet             `json:"writes"`
		SchemaUpgraded int                  `json:"schemaUpgraded"`
		Similar        []SimilarRequirement `json:"similar"`
	}](result)
	if err != nil {
		return RequirementWriteResult{}, err
	}
	// The path is already in changed, and anchor and line are for a reader:
	// a write answers with what the next decision needs.
	req := projectRequirement(requirementOf(payload.Requirement), writeRequirementFields)
	// Either write changes the spec's file, so the host sees the spec updated.
	s.announce(ctx, WriteEvent{
		Tool: tool, Method: method, ItemID: string(payload.Requirement.Spec), Op: "updated", Result: result,
	})
	return RequirementWriteResult{
		Requirement:    req,
		SpecRev:        payload.SpecRev,
		Changed:        payload.Writes.paths(),
		SchemaUpgraded: payload.SchemaUpgraded,
		Similar:        payload.Similar,
	}, nil
}

// requirementRev reads the requirement rev and status of one ref, the token a
// later update_requirement quotes, so acting on a requirement search hit does
// not cost an extra read. A ref that no longer resolves answers empty strings.
func (s *Server) requirementRev(ctx context.Context, ref string) (rev, status string) {
	got, err := dispatch[struct {
		Requirement core.RequirementView `json:"requirement"`
	}](ctx, s, "requirement.get", map[string]any{"ref": strings.TrimSpace(ref)})
	if err != nil {
		return "", ""
	}
	return string(got.Requirement.Rev), string(got.Requirement.Status)
}
