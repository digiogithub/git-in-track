package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The diagnostic codes of the spec layer (ADR-037, docs/03 section 16). The
// grammar-lint LINT-REQ-* codes live in speclint.go and the Spec Delta codes in
// specdelta.go; the marker codes are emitted by the native trace engine.
const (
	CodeReqForeign     Code = "E-REQ-FOREIGN"
	CodeReqDuplicate   Code = "E-REQ-DUPLICATE"
	CodeReqStatus      Code = "E-REQ-STATUS"
	CodeReqField       Code = "E-REQ-FIELD"
	CodeSchemaFeature  Code = "E-SCHEMA-FEATURE"
	CodeLinkTargetType Code = "E-LINK-TARGET-TYPE"
	// CodeLinkComputedOnly is a link written with a kind that exists only as a
	// computed inverse: implemented_by or modified_by (R-LINK-8).
	CodeLinkComputedOnly Code = "E-LINK-COMPUTED-ONLY"

	CodeWarnReqSeparator   Code = "W-REQ-SEPARATOR"
	CodeWarnReqHeading     Code = "W-REQ-HEADING"
	CodeWarnReqNoEntry     Code = "W-REQ-NO-ENTRY"
	CodeWarnReqOrphanEntry Code = "W-REQ-ORPHAN-ENTRY"
)

// Requirements is the requirements: map of a spec, keyed by "R<n>" (docs/03
// section 21.4). A key that is not R<n> is kept as written, so that a file with
// a typo round-trips, and reported as E-REQ-FIELD by the validator.
type Requirements map[string]*Requirement

// Requirement is the metadata of one requirement. Its prose lives in the body;
// every key here is optional.
type Requirement struct {
	// Status is a status of the project workflow; absent reads as
	// workflow.initial (W-REQ-NO-ENTRY).
	Status   Status            `json:"status,omitempty"`
	Trace    *RequirementTrace `json:"trace,omitempty"`
	Verified *Verification     `json:"verified,omitempty"`
	// Links are the requirement-level relations: supersedes, superseded_by and
	// relates_to only (R-REQ-13).
	Links []Link `json:"links,omitempty"`
	// Extra preserves the keys this version does not know (R-FMT-6).
	Extra map[string]any `json:"extra,omitempty"`
}

// RequirementTrace lists the code and tests that realize a requirement when an
// in-code marker is impractical. Paths are repository-relative (R-REQ-8).
type RequirementTrace struct {
	Code  []string       `json:"code,omitempty"`
	Tests []string       `json:"tests,omitempty"`
	Extra map[string]any `json:"extra,omitempty"`
}

// Verification is the durable verification stamp of a requirement (R-REQ-11a):
// the block rev that was verified, the commit its tests passed at, when and by
// whom. It is the only state a tool writes back into a spec.
type Verification struct {
	Rev    Rev            `json:"rev,omitempty"`
	Commit string         `json:"commit,omitempty"`
	At     Timestamp      `json:"at,omitempty"`
	By     string         `json:"by,omitempty"`
	Extra  map[string]any `json:"extra,omitempty"`
}

// requirementKnownKeys, traceKnownKeys and verifiedKnownKeys are the keys a
// requirement entry understands; everything else is preserved in Extra.
var (
	requirementKnownKeys = map[string]bool{"status": true, "trace": true, "verified": true, "links": true}
	traceKnownKeys       = map[string]bool{"code": true, "tests": true}
	verifiedKnownKeys    = map[string]bool{"rev": true, "commit": true, "at": true, "by": true}
)

// Clone returns a deep copy of the map. Extra values are shared: they are only
// ever read and written back.
func (r Requirements) Clone() Requirements {
	if r == nil {
		return nil
	}
	out := make(Requirements, len(r))
	for k, v := range r {
		out[k] = v.Clone()
	}
	return out
}

// Keys returns the keys in emission order: R<n> keys by number, then any other
// key lexicographically (R-REQ-9).
func (r Requirements) Keys() []string {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, aok := ParseRequirementKey(keys[i])
		b, bok := ParseRequirementKey(keys[j])
		switch {
		case aok && bok:
			return a < b
		case aok != bok:
			return aok
		default:
			return keys[i] < keys[j]
		}
	})
	return keys
}

// Clone returns a deep copy of the entry, or nil.
func (e *Requirement) Clone() *Requirement {
	if e == nil {
		return nil
	}
	out := &Requirement{Status: e.Status, Links: append([]Link(nil), e.Links...), Extra: cloneMap(e.Extra)}
	if e.Trace != nil {
		out.Trace = &RequirementTrace{
			Code:  append([]string(nil), e.Trace.Code...),
			Tests: append([]string(nil), e.Trace.Tests...),
			Extra: cloneMap(e.Trace.Extra),
		}
	}
	if e.Verified != nil {
		v := *e.Verified
		v.Extra = cloneMap(e.Verified.Extra)
		out.Verified = &v
	}
	return out
}

// canonicalValue renders the entry as the generic value its canonical JSON is
// computed over, with the file's key names.
func (e *Requirement) canonicalValue() map[string]any {
	out := map[string]any{}
	if e == nil {
		return out
	}
	for k, v := range e.Extra {
		out[k] = v
	}
	if e.Status != "" {
		out["status"] = string(e.Status)
	}
	if t := e.Trace; t != nil {
		trace := map[string]any{}
		for k, v := range t.Extra {
			trace[k] = v
		}
		if len(t.Code) > 0 {
			trace["code"] = t.Code
		}
		if len(t.Tests) > 0 {
			trace["tests"] = t.Tests
		}
		out["trace"] = trace
	}
	if v := e.Verified; v != nil {
		verified := map[string]any{}
		for k, x := range v.Extra {
			verified[k] = x
		}
		if v.Rev != "" {
			verified["rev"] = string(v.Rev)
		}
		if v.Commit != "" {
			verified["commit"] = v.Commit
		}
		if !v.At.IsZero() {
			verified["at"] = v.At.String()
		}
		if v.By != "" {
			verified["by"] = v.By
		}
		out["verified"] = verified
	}
	if len(e.Links) > 0 {
		links := make([]any, 0, len(e.Links))
		for _, l := range e.Links {
			m := map[string]any{"kind": string(l.Kind), "target": l.Target}
			if l.Note != "" {
				m["note"] = l.Note
			}
			links = append(links, m)
		}
		out["links"] = links
	}
	return out
}

// CanonicalJSON renders the entry as the canonical JSON of R-REQ-REV-2: UTF-8,
// keys sorted, no insignificant whitespace, no HTML escaping, and "{}" for a
// nil entry.
func (e *Requirement) CanonicalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(jsonSafe(e.canonicalValue())); err != nil {
		return nil, fmt.Errorf("encode requirement entry: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// jsonSafe converts the shapes a YAML decoder produces into ones encoding/json
// renders deterministically: timestamps in the canonical form, and nested maps
// with non-string keys stringified.
func jsonSafe(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = jsonSafe(x)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[fmt.Sprint(k)] = jsonSafe(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = jsonSafe(x)
		}
		return out
	case time.Time:
		if lit, ok := scalarLiteral(t); ok {
			return lit
		}
		return t
	default:
		return v
	}
}

// requirements reads the requirements: map. A value whose shape cannot be
// modeled — an entry, a trace or a stamp that is not a mapping, a list that is
// not a list of strings, a timestamp that does not parse — is E-REQ-FIELD. Keys
// and values whose shape is right but whose content is not (a key that is not
// R<n>, a bad trace path, a stamp missing a field) are kept for the validator,
// so that the file still indexes and round-trips.
func (p *fieldReader) requirements(key string) Requirements {
	v, ok := p.value(key)
	if !ok {
		return nil
	}
	m, isMap := v.(map[string]any)
	if !isMap {
		p.fail(key, CodeReqField, fmt.Sprintf("want a mapping of R<n> to entries, got %T", v))
		return nil
	}
	if len(m) == 0 {
		return nil
	}
	out := make(Requirements, len(m))
	for k, raw := range m {
		entry, err := parseRequirementEntry(raw)
		if err != nil {
			p.fail(key, CodeReqField, fmt.Sprintf("%s: %v", k, err))
			continue
		}
		out[k] = entry
	}
	return out
}

// parseRequirementEntry decodes one entry of the requirements: map.
func parseRequirementEntry(raw any) (*Requirement, error) {
	if raw == nil {
		return &Requirement{}, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("want a mapping, got %T", raw)
	}
	e := &Requirement{}
	if v, has := m["status"]; has && v != nil {
		s, ok := scalarString(v)
		if !ok {
			return nil, fmt.Errorf("status: want a status id, got %T", v)
		}
		e.Status = Status(s)
	}
	if v, has := m["trace"]; has && v != nil {
		t, err := parseTrace(v)
		if err != nil {
			return nil, err
		}
		e.Trace = t
	}
	if v, has := m["verified"]; has && v != nil {
		ver, err := parseVerification(v)
		if err != nil {
			return nil, err
		}
		e.Verified = ver
	}
	if v, has := m["links"]; has && v != nil {
		links, err := parseRequirementLinks(v)
		if err != nil {
			return nil, err
		}
		e.Links = links
	}
	e.Extra = extraKeys(m, requirementKnownKeys)
	return e, nil
}

// parseTrace decodes the trace: mapping of an entry.
func parseTrace(raw any) (*RequirementTrace, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("trace: want a mapping with code and tests, got %T", raw)
	}
	t := &RequirementTrace{}
	var err error
	if t.Code, err = stringListOf(m["code"]); err != nil {
		return nil, fmt.Errorf("trace.code: %w", err)
	}
	if t.Tests, err = stringListOf(m["tests"]); err != nil {
		return nil, fmt.Errorf("trace.tests: %w", err)
	}
	t.Extra = extraKeys(m, traceKnownKeys)
	return t, nil
}

// parseVerification decodes the verified: stamp of an entry.
func parseVerification(raw any) (*Verification, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("verified: want a mapping with rev, commit, at and by, got %T", raw)
	}
	v := &Verification{
		Rev:    Rev(stringOf(m["rev"])),
		Commit: stringOf(m["commit"]),
		By:     stringOf(m["by"]),
	}
	if at, has := m["at"]; has && at != nil {
		switch t := at.(type) {
		case time.Time:
			v.At = NewTimestamp(t)
		default:
			ts, err := ParseTimestamp(stringOf(at))
			if err != nil {
				return nil, fmt.Errorf("verified.at: %q is not an ISO 8601 UTC timestamp (%s)", stringOf(at), TimestampLayout)
			}
			v.At = ts
		}
	}
	v.Extra = extraKeys(m, verifiedKnownKeys)
	return v, nil
}

// parseRequirementLinks decodes the links: list of an entry. The kind is not
// checked here: which kinds are allowed is the validator's call (R-REQ-13).
func parseRequirementLinks(raw any) ([]Link, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("links: want a list of relations, got %T", raw)
	}
	out := make([]Link, 0, len(list))
	for _, e := range list {
		m, isMap := e.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("links: want {kind, target}, got %T", e)
		}
		l := Link{Kind: LinkKind(stringOf(m["kind"])), Target: stringOf(m["target"]), Note: stringOf(m["note"])}
		if l.Kind == "" || l.Target == "" {
			return nil, fmt.Errorf("links: a relation needs both kind and target")
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// scalarString renders a YAML scalar as a string, refusing composite values.
func scalarString(v any) (string, bool) {
	switch v.(type) {
	case map[string]any, []any:
		return "", false
	default:
		return stringOf(v), true
	}
}

// stringListOf reads a list of strings; a single scalar is a list of one.
func stringListOf(raw any) ([]string, error) {
	switch t := raw.(type) {
	case nil:
		return nil, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := scalarString(e)
			if !ok {
				return nil, fmt.Errorf("want a list of strings, got an element of type %T", e)
			}
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	default:
		s, ok := scalarString(t)
		if !ok {
			return nil, fmt.Errorf("want a list of strings, got %T", raw)
		}
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	}
}

// extraKeys returns the entries of a mapping whose keys are not known.
func extraKeys(m map[string]any, known map[string]bool) map[string]any {
	var out map[string]any
	for k, v := range m {
		if known[k] || v == nil {
			continue
		}
		if out == nil {
			out = make(map[string]any)
		}
		out[k] = v
	}
	return out
}

// requirements writes the requirements: map in canonical form (R-REQ-9): keys
// in numeric order, inside an entry status, trace, verified, links and then the
// unknown keys sorted; the stamp as one flow mapping and each link on one line,
// so that a status change or a new stamp is a one-line diff.
func (w *fmWriter) requirements(key string, reqs Requirements) error {
	if len(reqs) == 0 {
		return nil
	}
	w.b.WriteString(key + ":\n")
	for _, k := range reqs.Keys() {
		e := reqs[k]
		if e.isEmpty() {
			w.b.WriteString("  " + yamlString(k) + ": {}\n")
			continue
		}
		w.b.WriteString("  " + yamlString(k) + ":\n")
		if e.Status != "" {
			w.b.WriteString("    status: " + yamlString(string(e.Status)) + "\n")
		}
		if t := e.Trace; t != nil && (len(t.Code) > 0 || len(t.Tests) > 0 || len(t.Extra) > 0) {
			w.b.WriteString("    trace:\n")
			w.stringListAt(6, "code", t.Code)
			w.stringListAt(6, "tests", t.Tests)
			if err := w.writeMapBody(6, t.Extra); err != nil {
				return err
			}
		}
		if err := w.verification(4, e.Verified); err != nil {
			return err
		}
		if len(e.Links) > 0 {
			w.b.WriteString("    links:\n")
			for _, l := range e.Links {
				parts := []string{
					"kind: " + yamlFlowString(string(l.Kind)),
					"target: " + yamlFlowString(l.Target),
				}
				if l.Note != "" {
					parts = append(parts, "note: "+yamlFlowString(l.Note))
				}
				w.b.WriteString("      - { " + strings.Join(parts, ", ") + " }\n")
			}
		}
		if err := w.writeMapBody(4, e.Extra); err != nil {
			return err
		}
	}
	return nil
}

// isEmpty reports whether the entry holds nothing at all.
func (e *Requirement) isEmpty() bool {
	if e == nil {
		return true
	}
	traceEmpty := e.Trace == nil || (len(e.Trace.Code) == 0 && len(e.Trace.Tests) == 0 && len(e.Trace.Extra) == 0)
	return e.Status == "" && traceEmpty && e.Verified == nil && len(e.Links) == 0 && len(e.Extra) == 0
}

// stringListAt writes a list of scalars at an indentation, compact when it fits.
func (w *fmWriter) stringListAt(indent int, key string, items []string) {
	if len(items) == 0 {
		return
	}
	pad := strings.Repeat(" ", indent)
	rendered := make([]string, 0, len(items))
	for _, s := range items {
		rendered = append(rendered, yamlFlowString(s))
	}
	flow := "[" + strings.Join(rendered, ", ") + "]"
	if indent+len(key)+2+len(flow) <= flowWidth {
		w.b.WriteString(pad + key + ": " + flow + "\n")
		return
	}
	w.b.WriteString(pad + key + ":\n")
	for _, r := range rendered {
		w.b.WriteString(pad + "  - " + r + "\n")
	}
}

// verification writes a stamp as a single flow mapping in the order rev,
// commit, at, by, followed by unknown scalar keys sorted. A stamp carrying an
// unknown composite value falls back to a block mapping, which still keeps it.
func (w *fmWriter) verification(indent int, v *Verification) error {
	if v == nil {
		return nil
	}
	pad := strings.Repeat(" ", indent)
	var parts []string
	if v.Rev != "" {
		// Always quoted, as in docs/03 section 21.4: a rev reads as a string
		// to every YAML parser, flow context or not.
		parts = append(parts, "rev: "+quoteYAML(string(v.Rev)))
	}
	if v.Commit != "" {
		parts = append(parts, "commit: "+yamlFlowString(v.Commit))
	}
	if !v.At.IsZero() {
		parts = append(parts, "at: "+v.At.String())
	}
	if v.By != "" {
		parts = append(parts, "by: "+yamlFlowString(v.By))
	}
	for _, k := range sortedKeys(v.Extra) {
		lit, ok := scalarLiteral(v.Extra[k])
		if !ok {
			w.b.WriteString(pad + "verified:\n")
			for _, p := range parts {
				w.b.WriteString(pad + "  " + p + "\n")
			}
			return w.writeMapBody(indent+2, v.Extra)
		}
		parts = append(parts, yamlFlowString(k)+": "+lit)
	}
	if len(parts) == 0 {
		w.b.WriteString(pad + "verified: {}\n")
		return nil
	}
	w.b.WriteString(pad + "verified: {" + strings.Join(parts, ", ") + "}\n")
	return nil
}

// quoteYAML renders a double-quoted YAML string.
func quoteYAML(s string) string {
	data, _ := json.Marshal(s) //nolint:errchkjson // a string always encodes
	return string(data)
}

// -------------------------------------------------------------- validation --

// commitRE is a full hex git commit id, SHA-1 or SHA-256 (docs/03 21.4).
var commitRE = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// specOnlyAbsentFields are the planning fields a spec never carries: it is a
// living description, not scheduled work (ADR-037 section 1).
var specOnlyAbsentFields = []string{"epic", "milestone", "sprint", "estimate", "effort", "spent", "due", "inbox"}

// validateSpec applies the spec rules of docs/03 section 21 to one item: the
// planning fields a spec has no use for, the inbox exclusion, the requirement
// blocks and the requirements: map. A requirements: map on any other type is
// E-REQ-FIELD.
func validateSpec(d *diagSet, item *Item, cfg *ProjectConfig) {
	if item.Type != TypeSpec {
		if len(item.Requirements) > 0 {
			d.errorf("requirements", CodeReqField, "only a spec carries requirements, not a %s", item.Type)
		}
		return
	}
	for _, field := range specOnlyAbsentFields {
		if specFieldSet(item, field) {
			d.errorf(field, CodeFieldType, "a spec has no %s: plan on the stories that implement or modify it", field)
		}
	}
	if cfg != nil && cfg.IsTriageStatus(item.Status) {
		d.errorf("status", CodeReqStatus, "a spec is not an inbox target and cannot take the triage status %q", item.Status)
	}
	d.out = append(d.out, RequirementDiagnostics(item, cfg)...)
}

// specFieldSet reports whether a planning field is present on an item.
func specFieldSet(item *Item, field string) bool {
	switch field {
	case "epic":
		return item.Epic != ""
	case "milestone":
		return item.Milestone != ""
	case "sprint":
		return item.Sprint != ""
	case "estimate":
		return item.Estimate != nil
	case "effort":
		return item.Effort != nil
	case "spent":
		return item.Spent != nil
	case "due":
		return !item.Due.IsZero()
	case "inbox":
		return item.Inbox != nil
	default:
		return false
	}
}

// RequirementDiagnostics returns the findings about the requirement blocks and
// the requirements: map of a spec (docs/03 sections 21.2 to 21.4): the parser's
// findings, E-REQ-DUPLICATE, E-REQ-FIELD, E-REQ-STATUS, E-STATUS-UNKNOWN,
// E-LINK-TARGET-TYPE, W-REQ-NO-ENTRY, W-REQ-ORPHAN-ENTRY and the LINT-REQ-*
// grammar lint at the severity specs.lint gives each rule. A nil cfg skips the
// status checks and lints at warning. The index runs it over every spec so that doctor reports it;
// ValidateItem runs it before every write.
func RequirementDiagnostics(item *Item, cfg *ProjectConfig) []Diagnostic {
	if item == nil || item.Type != TypeSpec {
		return nil
	}
	d := &diagSet{path: item.Path}
	lines := &fileLines{item: item}
	body := ParseSpecBody(item.ID, item.Body)
	for _, f := range body.Findings {
		field := "body"
		if f.Ref != "" {
			field = "body." + f.Ref
		}
		d.addAt(f.Code, f.Severity, field, lines.body(f.Line), f.Message)
	}

	seen := map[int]int{}
	for _, blk := range body.Blocks {
		if first, dup := seen[blk.Ref.Number]; dup {
			d.addAt(CodeReqDuplicate, SeverityError, "body."+blk.Ref.String(), lines.body(blk.Line),
				fmt.Sprintf("%s is declared twice, on lines %d and %d", blk.Ref, lines.body(first), lines.body(blk.Line)))
			continue
		}
		seen[blk.Ref.Number] = blk.Line
		lintDiagnostics(d, lines, blk, cfg.SpecLint())
		entry, ok := item.Requirements[blk.Ref.Key()]
		switch {
		case !ok:
			// Nothing in the front matter to point at: the block is.
			d.addAt(CodeWarnReqNoEntry, SeverityWarning, "requirements."+blk.Ref.Key(), lines.body(blk.Line),
				fmt.Sprintf("%s has no requirements: entry; its status reads as the workflow's initial status", blk.Ref))
		case entry == nil || entry.Status == "":
			d.warnf("requirements."+blk.Ref.Key()+".status", CodeWarnReqNoEntry,
				"%s has no status; it reads as the workflow's initial status", blk.Ref)
		}
	}

	for _, key := range item.Requirements.Keys() {
		entry := item.Requirements[key]
		field := "requirements." + key
		n, ok := ParseRequirementKey(key)
		if !ok {
			d.errorf(field, CodeReqField, "%q is not a requirement key: want R<n> with an unpadded number", key)
		} else if _, has := seen[n]; !has {
			d.warnf(field, CodeWarnReqOrphanEntry,
				"%s.%s has an entry but no block in the body; its number stays reserved", item.ID, key)
		}
		validateRequirementEntry(d, field, entry, cfg)
	}
	// Every finding about a requirements: entry points at its node, or at
	// the closest one the file holds.
	for i := range d.out {
		if d.out[i].Line == 0 && strings.HasPrefix(d.out[i].Field, "requirements.") {
			d.out[i].Line = lines.field(d.out[i].Field)
		}
	}
	orderDiagnostics(d.out)
	return d.out
}

// validateRequirementEntry checks the values of one requirements: entry.
func validateRequirementEntry(d *diagSet, field string, e *Requirement, cfg *ProjectConfig) {
	if e == nil {
		return
	}
	if e.Status != "" && cfg != nil && len(cfg.Workflow.Statuses) > 0 {
		switch {
		case !statusDeclared(cfg, e.Status):
			d.errorf(field+".status", CodeStatusUnknown, "%q is not declared in the workflow", e.Status)
		case cfg.IsTriageStatus(e.Status):
			d.errorf(field+".status", CodeReqStatus, "%q is a triage status; a requirement is never in the inbox", e.Status)
		}
	}
	if t := e.Trace; t != nil {
		for i, ref := range t.Code {
			if err := checkTraceRef(ref); err != nil {
				d.errorf(fmt.Sprintf("%s.trace.code[%d]", field, i), CodeReqField, "%s", err)
			}
		}
		for i, ref := range t.Tests {
			if err := checkTraceRef(ref); err != nil {
				d.errorf(fmt.Sprintf("%s.trace.tests[%d]", field, i), CodeReqField, "%s", err)
			}
		}
	}
	if v := e.Verified; v != nil {
		f := field + ".verified"
		if !v.Rev.Valid() {
			d.errorf(f+".rev", CodeReqField, "%q is not a block rev (sha256:<16 hex>)", v.Rev)
		}
		if !commitRE.MatchString(v.Commit) {
			d.errorf(f+".commit", CodeReqField, "%q is not a full hex commit id (40 or 64 hex digits)", v.Commit)
		}
		if v.At.IsZero() {
			d.errorf(f+".at", CodeReqField, "missing: want the UTC timestamp of the verified run")
		}
		if strings.TrimSpace(v.By) == "" {
			d.errorf(f+".by", CodeReqField, "missing: want the handle that ran the verification")
		}
	}
	for i, l := range e.Links {
		f := fmt.Sprintf("%s.links[%d]", field, i)
		switch l.Kind {
		case LinkSupersedes, LinkSupersededBy:
			if validateLinkTarget(d, cfg, f+".target", l.Target) && classifyLinkTarget(l.Target) != targetRequirement {
				d.errorf(f+".target", CodeLinkTargetType, "%s must target a requirement ref, not %q", l.Kind, l.Target)
			}
		case LinkRelatesTo:
			validateLinkTarget(d, cfg, f+".target", l.Target)
		default:
			d.errorf(f+".kind", CodeReqField,
				"a requirement's links allow supersedes, superseded_by and relates_to, not %q", l.Kind)
		}
	}
}

// statusDeclared reports whether the workflow declares a status.
func statusDeclared(cfg *ProjectConfig, s Status) bool {
	_, ok := cfg.StatusDef(s)
	return ok
}

// checkTraceRef applies the trace-ref grammar of R-REQ-8: a repository-relative,
// "/"-separated path with no ".." segment and no leading "/", optionally
// followed by "#<symbol>".
func checkTraceRef(ref string) error {
	p, symbol, hasSymbol := strings.Cut(ref, "#")
	switch {
	case strings.TrimSpace(p) == "":
		return fmt.Errorf("%q has no path: want <path>[#<symbol>]", ref)
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("%q is absolute: trace paths are relative to the repository root", ref)
	case strings.Contains(p, "\\"):
		return fmt.Errorf("%q uses \\: trace paths are /-separated", ref)
	case hasSymbol && strings.TrimSpace(symbol) == "":
		return fmt.Errorf("%q has an empty symbol after #", ref)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("%q leaves the repository through a .. segment", ref)
		}
	}
	return nil
}

// ----------------------------------------------------------------- schema --

// HasSpecConstruct reports whether an item holds a spec construct of
// R-SCHEMA-2-1: it is a spec, or it carries a link — item-level or in a
// requirements: entry — of one of the six kinds ADR-037 adds, or whose target is
// a spec id or a requirement ref. Spec Delta sections, wikilinks and markers are
// not spec constructs.
func HasSpecConstruct(item *Item) bool {
	if item == nil {
		return false
	}
	if item.Type == TypeSpec {
		return true
	}
	if linksHaveSpecConstruct(item.Links) {
		return true
	}
	for _, e := range item.Requirements {
		if e != nil && linksHaveSpecConstruct(e.Links) {
			return true
		}
	}
	return false
}

// linksHaveSpecConstruct reports whether any link is a spec construct: one of
// the six spec kinds, or a spec or requirement target.
func linksHaveSpecConstruct(links []Link) bool {
	for _, l := range links {
		if l.Kind.Spec() || isSpecTarget(l.Target) {
			return true
		}
	}
	return false
}

// isSpecTarget reports whether a link target names a spec or a requirement.
func isSpecTarget(target string) bool {
	t := classifyLinkTarget(target)
	return t == targetSpec || t == targetRequirement
}

// validateSchemaFeature applies E-SCHEMA-FEATURE: a spec construct in a project
// whose schema predates the spec layer (R-SCHEMA-2-2).
func validateSchemaFeature(d *diagSet, item *Item, cfg *ProjectConfig) {
	if diag, ok := SchemaFeatureDiagnostic(item, cfg); ok {
		d.out = append(d.out, diag)
	}
}

// SchemaFeatureDiagnostic returns the E-SCHEMA-FEATURE finding of an item, if
// it holds a spec construct in a project below SpecSchema.
func SchemaFeatureDiagnostic(item *Item, cfg *ProjectConfig) (Diagnostic, bool) {
	if cfg == nil || cfg.Schema == 0 || cfg.Schema >= SpecSchema || !HasSpecConstruct(item) {
		return Diagnostic{}, false
	}
	return Diagnostic{
		Code: CodeSchemaFeature, Severity: SeverityError, Path: item.Path, Field: "schema",
		Message: fmt.Sprintf("a spec construct requires schema %d, and project.yaml declares schema %d; "+
			"run gintrack doctor --fix to raise it", SpecSchema, cfg.Schema),
	}, true
}
