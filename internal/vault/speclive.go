package vault

import (
	"fmt"
	"sort"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file answers "spec.lint" and "spec.delta.preview" (GIT-US-0132): the
// live grammar lint and the Spec Delta preview of the web editor. Both are pure
// reads over the text the editor holds — nothing is written — and both answer
// through the vault rather than through bespoke WASM exports, so browser-only
// mode (gintrackCore.call) and the companion (the REST routes of
// internal/server/specs.go) run the very same code: the lint rules exist once,
// in internal/core, and never in TypeScript.

// specLiveParams are the params of both methods. project names the project
// whose specs.lint applies (and routes the call in a workspace); id is the item
// being edited, whose key stands in for project when project is empty.
type specLiveParams struct {
	Project string `json:"project,omitempty"`
	ID      string `json:"id,omitempty"`
	// Type is the type of the item being edited: a spec body is linted block by
	// block, a story or task body through its Spec Delta, and any other body
	// has nothing to lint.
	Type string `json:"type,omitempty"`
	Body string `json:"body"`
}

// SpecLiveFinding is one finding of "spec.lint", on a 1-based line of the body
// the editor sent.
type SpecLiveFinding struct {
	Code     core.Code     `json:"code"`
	Severity core.Severity `json:"severity"`
	Line     int           `json:"line"`
	// Ref is the requirement or the Spec Delta target the finding is about.
	Ref     string `json:"ref,omitempty"`
	Message string `json:"message"`
}

// DeltaPreviewOperation is one operation of "spec.delta.preview": what the
// story proposes next to what the spec holds today.
type DeltaPreviewOperation struct {
	Op        core.DeltaOp `json:"op"`
	Spec      core.ItemID  `json:"spec"`
	SpecTitle string       `json:"specTitle,omitempty"`
	// Target is what the heading names: the ref of a MODIFIED or REMOVED
	// operation (or of an applied ADDED), the spec of an unapplied ADDED.
	Target     string `json:"target"`
	Title      string `json:"title"`
	Line       int    `json:"line"`
	Supersedes string `json:"supersedes,omitempty"`
	Reason     string `json:"reason,omitempty"`
	// Proposed is the block an ADDED or MODIFIED operation proposes, below its
	// heading; empty for REMOVED.
	Proposed string `json:"proposed,omitempty"`
	// Current is the requirement the operation changes, as the spec holds it
	// now: the target of MODIFIED and REMOVED, the Supersedes: target of an
	// ADDED move. Absent when the target is dangling or there is none.
	Current *DeltaPreviewCurrent `json:"current,omitempty"`
	// Dangling explains a target this repository does not hold
	// (W-DELTA-DANGLING); the preview then has no current text.
	Dangling string `json:"dangling,omitempty"`
}

// DeltaPreviewCurrent is the current text of one requirement.
type DeltaPreviewCurrent struct {
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
	Text   string `json:"text"`
}

// specLint answers "spec.lint": the grammar-lint findings of a body at the
// severities of the project's specs.lint. A rule at off does not run, so it
// yields nothing. For a story or a task the findings are those of its Spec
// Delta: the structural findings of the parser, the lint of every added and
// replacement block, and W-DELTA-DANGLING for a target this repository does
// not hold.
func (v *Vault) specLint(raw []byte) (any, error) {
	p, err := decodeParams[specLiveParams](raw)
	if err != nil {
		return nil, err
	}
	cfg, err := v.specLiveConfig(p)
	if err != nil {
		return nil, err
	}
	out := []SpecLiveFinding{}
	switch core.ItemType(p.Type) {
	case core.TypeSpec:
		spec := core.ItemID(p.ID)
		for _, f := range core.ParseSpecBody(spec, p.Body).Findings {
			out = append(out, SpecLiveFinding{Code: f.Code, Severity: f.Severity, Line: f.Line, Ref: f.Ref, Message: f.Message})
		}
		for _, f := range core.LintSpec(spec, p.Body, cfg.SpecLint()) {
			out = append(out, lintFinding(f))
		}
	case core.TypeStory, core.TypeTask:
		delta := core.ParseSpecDelta(p.Body)
		for _, f := range delta.Findings {
			out = append(out, SpecLiveFinding{Code: f.Code, Severity: f.Severity, Line: f.Line, Ref: f.Ref, Message: f.Message})
		}
		for _, f := range core.LintSpecDelta(delta, cfg.SpecLint()) {
			out = append(out, lintFinding(f))
		}
		for _, op := range delta.Operations {
			if msg := v.danglingTarget(op); msg != "" {
				out = append(out, SpecLiveFinding{
					Code: core.CodeWarnDeltaDangling, Severity: core.SeverityWarning,
					Line: op.Line, Ref: op.Target(), Message: msg,
				})
			}
		}
	case "":
		return nil, failf("invalid_request", "spec.lint needs type: spec, story or task")
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return map[string]any{"findings": out}, nil
}

// lintFinding renders a grammar-lint finding in the wire shape.
func lintFinding(f core.LintFinding) SpecLiveFinding {
	ref := f.Ref.String()
	if f.Ref.Number == 0 {
		// An unapplied ADDED block has no number yet: it is about its spec.
		ref = string(f.Ref.Spec)
	}
	return SpecLiveFinding{Code: f.Rule, Severity: f.Severity, Line: f.Line, Ref: ref, Message: f.Message}
}

// specDeltaPreview answers "spec.delta.preview": every operation of the Spec
// Delta of a body, with the current text of the requirement it changes.
func (v *Vault) specDeltaPreview(raw []byte) (any, error) {
	p, err := decodeParams[specLiveParams](raw)
	if err != nil {
		return nil, err
	}
	delta := core.ParseSpecDelta(p.Body)
	ops := make([]DeltaPreviewOperation, 0, len(delta.Operations))
	for _, op := range delta.Operations {
		row := DeltaPreviewOperation{
			Op: op.Op, Spec: op.Spec, Target: op.Target(), Title: op.Title, Line: op.Line,
			Reason: op.Reason, Proposed: op.ProposedText(), Dangling: v.danglingTarget(op),
		}
		if spec, err := v.index.Item(op.Spec); err == nil && spec.Type == core.TypeSpec {
			row.SpecTitle = spec.Title
		}
		if op.Supersedes != nil {
			row.Supersedes = op.Supersedes.String()
		}
		current := op.Ref
		if op.Op == core.DeltaAdded {
			// The number of an applied ADDED is the block it added, not one it
			// changes; a move changes the block it supersedes.
			current = op.Supersedes
		}
		if current != nil {
			if view, err := v.index.Requirement(*current); err == nil {
				row.Current = &DeltaPreviewCurrent{
					Ref: view.Ref.String(), Title: view.Title, Status: string(view.Status), Text: view.Text,
				}
			}
		}
		ops = append(ops, row)
	}
	return map[string]any{"operations": ops}, nil
}

// danglingTarget explains why this repository does not hold the target of an
// operation, or of its Supersedes: line, in the words of W-DELTA-DANGLING;
// empty when it holds both.
func (v *Vault) danglingTarget(op core.DeltaOperation) string {
	check := func(what string, spec core.ItemID, ref *core.RequirementRef) string {
		s, err := v.index.Item(spec)
		if err != nil || s.Type != core.TypeSpec {
			return fmt.Sprintf("%s names unknown spec %s", what, spec)
		}
		if ref != nil {
			if _, err := v.index.Requirement(*ref); err != nil {
				return fmt.Sprintf("%s names unknown requirement %s", what, ref)
			}
		}
		return ""
	}
	if msg := check(string(op.Op), op.Spec, op.Ref); msg != "" {
		return msg
	}
	if op.Supersedes != nil {
		return check("Supersedes: of "+string(op.Op)+" "+op.Target(), op.Supersedes.Spec, op.Supersedes)
	}
	return ""
}

// specLiveConfig returns the configuration of the project whose specs.lint
// applies: the one named by project, else the one of the item's key, else the
// only project of the repository. A repository that holds none lints at the
// defaults (every rule at warning).
func (v *Vault) specLiveConfig(p specLiveParams) (*core.ProjectConfig, error) {
	if p.Project == "" && p.ID != "" {
		if key, _, _, err := core.ParseItemID(p.ID); err == nil {
			p.Project = string(key)
		}
	}
	if p.Project == "" && len(v.projects) == 0 {
		return nil, nil
	}
	_, cfg, err := v.projectConfig(core.ProjectKey(p.Project))
	return cfg, err
}
