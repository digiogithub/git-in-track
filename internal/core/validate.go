package core

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Diagnostic codes of docs/03-data-model.md section 16 that the validator raises
// and errors.go does not already name, plus the two codes this file adds for
// rules section 16 leaves without one. New codes are marked "new" and follow the
// same convention: "E-" blocks a write, "W-" is only reported.
//
//	| Code                   | Sev | Condition                                                          |
//	|------------------------|-----|--------------------------------------------------------------------|
//	| E-CF-TYPE              | E   | Custom field value has the wrong declared type                     |
//	| E-REF-PARENT-TYPE      | E   | parent points at a type that cannot be a parent                    |
//	| E-REF-CYCLE            | E   | parent chain contains a cycle (self-parent, at the item level)     |
//	| E-FIELD-REQUIRED       | E   | new: a field required for the item type is absent                  |
//	| E-REF-TARGET-TYPE      | E   | new: milestone/epic names an id whose type code is wrong           |
//	| W-REF-DANGLING         | W   | parent/milestone/links.target points at an unknown id              |
//	| W-REF-CYCLE-BLOCK      | W   | cycle in blocks/blocked_by                                         |
//	| W-WORKFLOW-TRANSITION  | W   | status unreachable per the declared transitions                    |
//	| W-PERSON-UNKNOWN       | W   | handle not found in project.yaml people                            |
//	| W-LABEL-UNDECLARED     | W   | label not in the catalog                                           |
//	| W-CF-UNDECLARED        | W   | key under custom: not declared for this type                       |
//	| W-ESTIMATE-SCALE       | W   | estimate not in estimation.values                                  |
//
// E-REF-CYCLE and W-REF-DANGLING/W-REF-CYCLE-BLOCK also exist as vault-wide
// rules; only what a single item plus the project configuration can decide is
// checked here. Resolving a reference to another file is the index's job.
const (
	CodeCustomFieldType Code = "E-CF-TYPE"
	CodeRefParentType   Code = "E-REF-PARENT-TYPE"
	CodeRefCycle        Code = "E-REF-CYCLE"
	CodeFieldRequired   Code = "E-FIELD-REQUIRED"
	CodeRefTargetType   Code = "E-REF-TARGET-TYPE"

	CodeWarnRefDangling        Code = "W-REF-DANGLING"
	CodeWarnRefCycleBlock      Code = "W-REF-CYCLE-BLOCK"
	CodeWarnWorkflowTransition Code = "W-WORKFLOW-TRANSITION"
	CodeWarnPersonUnknown      Code = "W-PERSON-UNKNOWN"
	CodeWarnLabelUndeclared    Code = "W-LABEL-UNDECLARED"
	CodeWarnCustomUndeclared   Code = "W-CF-UNDECLARED"
	CodeWarnEstimateScale      Code = "W-ESTIMATE-SCALE"
)

// maxTitleBytes is the documented upper bound of a title (docs/03 section 7.1).
const maxTitleBytes = 200

// Validator enforces the schema of docs/03-data-model.md and the workflow
// declared in project.yaml. Implementations are stateless and safe for
// concurrent use; everything they need is passed in, so the same code runs in
// the CLI and in the browser.
type Validator interface {
	// ValidateItem returns every problem of one item, errors and warnings alike,
	// in a deterministic order. A nil configuration limits the check to the rules
	// that need no project: grammar, required fields, dates and enumerations.
	ValidateItem(item *Item, cfg *ProjectConfig) []Diagnostic
	// ValidateProject returns every problem of a project configuration.
	ValidateProject(cfg *ProjectConfig) []Diagnostic
}

// SchemaValidator is the default Validator: it applies the consolidated rules of
// docs/03 section 16 that a single item plus its project configuration can
// decide. Rules that need the whole vault — duplicate ids, dangling references,
// parent cycles across files — belong to the index. The zero value is ready to
// use.
type SchemaValidator struct{}

// NewValidator returns the default Validator.
func NewValidator() Validator { return SchemaValidator{} }

// ValidateItem implements Validator.
func (SchemaValidator) ValidateItem(item *Item, cfg *ProjectConfig) []Diagnostic {
	return ValidateItem(item, cfg)
}

// ValidateProject implements Validator.
func (SchemaValidator) ValidateProject(cfg *ProjectConfig) []Diagnostic {
	return ValidateProject(cfg)
}

// ValidateItem checks one item against the schema and against the project
// configuration, collecting every problem instead of stopping at the first one.
// The result is ordered by severity, then code, then field, then message, so two
// runs over the same item report identically.
//
// It is safe to call on an item that was never read from a file: the file-name
// rules (E-ID-FILENAME, W-SLUG-STALE, the folder check of E-FM-TYPE) are skipped
// when Item.Path is empty.
func ValidateItem(item *Item, cfg *ProjectConfig) []Diagnostic {
	if item == nil {
		return nil
	}
	d := &diagSet{path: item.Path}
	validateIdentity(d, item, cfg)
	validateItemStatus(d, item, cfg)
	validateHierarchy(d, item, cfg)
	validateItemLinks(d, item, cfg)
	validateDates(d, item)
	validateNumbers(d, item, cfg)
	validatePeople(d, item, cfg)
	validateLabels(d, item, cfg)
	validateCustom(d, item, cfg)
	orderDiagnostics(d.out)
	return d.out
}

// ValidateProject checks a project configuration: the E-PROJ-* rules of docs/03
// section 6.3, plus the enumerations the rest of the file depends on.
func ValidateProject(cfg *ProjectConfig) []Diagnostic {
	if cfg == nil {
		return []Diagnostic{{
			Code:     CodeProjMissing,
			Severity: SeverityError,
			Path:     ProjectFileName,
			Message:  "the backlog folder has no project.yaml",
		}}
	}
	d := &diagSet{path: ProjectFileName, out: cfg.Validate()}
	validateProjectEnums(d, cfg)
	validateProjectCustomFields(d, cfg)
	validateProjectDefaults(d, cfg)
	orderDiagnostics(d.out)
	return d.out
}

// validateIdentity applies the id, type, title and file-name rules.
func validateIdentity(d *diagSet, item *Item, cfg *ProjectConfig) {
	switch {
	case item.Type == "":
		d.errorf("type", CodeFMType, "missing")
	case !item.Type.Valid():
		d.errorf("type", CodeFMType, "unknown item type %q", item.Type)
	case item.Type == TypeComment:
		d.errorf("type", CodeFMType, "a comment is not an item; comments live under comments/<ITEM-ID>/")
	default:
		if folder, ok := folderType(item.Path); ok && folder != item.Type {
			d.errorf("type", CodeFMType, "type %q does not match the %s/ folder it lives in", item.Type, folderOf(item.Path))
		}
	}

	if item.ID == "" {
		d.errorf("id", CodeIDMissing, "missing")
	} else if key, code, _, err := ParseItemID(string(item.ID)); err != nil {
		d.errorf("id", CodeIDGrammar, "%q does not match <KEY>-<EP|US|T|M>-<NNNN>", item.ID)
	} else {
		if cfg != nil && cfg.Key != "" && key != cfg.Key {
			d.errorf("id", CodeIDKey, "project key %q does not match the project key %q", key, cfg.Key)
		}
		if want, ok := TypeCodeFor(item.Type); ok && want != code {
			d.errorf("id", CodeIDTypeCode, "type code %q does not match type %q", code, item.Type)
		}
	}

	switch title := strings.TrimSpace(item.Title); {
	case title == "":
		d.errorf("title", CodeTitle, "missing or empty")
	case len(item.Title) > maxTitleBytes:
		d.errorf("title", CodeTitle, "longer than %d characters", maxTitleBytes)
	}

	validateFileName(d, item)

	if item.Created.IsZero() {
		d.errorf("created", CodeFieldRequired, "missing")
	}
	if item.Updated.IsZero() {
		d.errorf("updated", CodeFieldRequired, "missing")
	}
}

// validateFileName applies R-SLUG-1 and R-SLUG-3: the id prefix of the file name
// must match the id field (E-ID-FILENAME), and a stale slug is only a warning.
func validateFileName(d *diagSet, item *Item) {
	if item.Path == "" || !strings.HasSuffix(item.Path, ".md") {
		return
	}
	named := IDFromFileName(item.Path)
	switch {
	case named == "":
		d.errorf("id", CodeIDFilename, "file name %q does not start with an item id", baseName(item.Path))
		return
	case item.ID != "" && named != item.ID:
		d.errorf("id", CodeIDFilename, "file name claims %q but the id field says %q", named, item.ID)
		return
	}
	if want := Slugify(item.Title); want != slugFromPath(item.Path) {
		d.warnf("title", CodeWarnSlugStale, "file name slug %q does not match the title slug %q", slugFromPath(item.Path), want)
	}
}

// validateItemStatus applies E-STATUS-UNKNOWN and the informational
// W-WORKFLOW-TRANSITION rule for a status the workflow cannot reach.
func validateItemStatus(d *diagSet, item *Item, cfg *ProjectConfig) {
	if item.Status == "" {
		d.errorf("status", CodeFieldRequired, "missing")
		return
	}
	if cfg == nil || len(cfg.Workflow.Statuses) == 0 {
		return
	}
	if _, ok := cfg.StatusDef(item.Status); !ok {
		d.errorf("status", CodeStatusUnknown, "%q is not declared in the workflow", item.Status)
		return
	}
	if !statusReachable(cfg, item.Status) {
		d.warnf("status", CodeWarnWorkflowTransition, "%q cannot be reached from %q through the declared transitions", item.Status, cfg.InitialStatus())
	}
}

// validateHierarchy applies the parent rules (R-STORY-1, R-TASK-1) and the
// grammar of the dedicated reference fields parent, epic and milestone. The
// rules that depend on the item type are skipped when the type itself is already
// reported as invalid, so one bad type does not cascade into unrelated findings.
func validateHierarchy(d *diagSet, item *Item, cfg *ProjectConfig) {
	known := itemTypeKnown(item.Type)
	if item.Parent != "" {
		allowed := parentCodes(item.Type)
		switch {
		case known && len(allowed) == 0:
			d.errorf("parent", CodeRefParentType, "a %s has no parent", item.Type)
		case item.Parent == item.ID:
			d.errorf("parent", CodeRefCycle, "an item cannot be its own parent")
		default:
			if code, ok := validateReference(d, cfg, "parent", string(item.Parent)); ok && known && !containsTypeCode(allowed, code) {
				d.errorf("parent", CodeRefParentType, "a %s cannot be the parent of a %s", typeName(code), item.Type)
			}
		}
	}
	if item.Epic != "" {
		if code, ok := validateReference(d, cfg, "epic", string(item.Epic)); ok && code != CodeEpic {
			d.errorf("epic", CodeRefTargetType, "%q is not an epic id", item.Epic)
		}
	}
	if item.Milestone != "" {
		if code, ok := validateReference(d, cfg, "milestone", string(item.Milestone)); ok && code != CodeMilestone {
			d.errorf("milestone", CodeRefTargetType, "%q is not a milestone id", item.Milestone)
		}
	}
}

// validateReference checks the grammar of an id written in a dedicated reference
// field and that it belongs to this project. Cross-project references are only
// allowed in links (R-LINK-2), never in parent, epic or milestone.
func validateReference(d *diagSet, cfg *ProjectConfig, field, raw string) (TypeCode, bool) {
	key, code, _, err := ParseItemID(raw)
	if err != nil {
		d.errorf(field, CodeIDGrammar, "%q does not match <KEY>-<EP|US|T|M>-<NNNN>", raw)
		return "", false
	}
	if cfg != nil && cfg.Key != "" && key != cfg.Key {
		d.errorf(field, CodeIDKey, "%q belongs to project %q, not to %q", raw, key, cfg.Key)
		return code, false
	}
	return code, true
}

// validateItemLinks applies the rules of docs/03 section 12 to the links list.
func validateItemLinks(d *diagSet, item *Item, cfg *ProjectConfig) {
	for i, l := range item.Links {
		field := fmt.Sprintf("links[%d]", i)
		switch {
		case l.Kind == "":
			d.errorf(field, CodeFieldType, "a relation needs a kind")
		case !l.Kind.Valid():
			d.errorf(field+".kind", CodeEnum, "unknown relation kind %q", l.Kind)
		}
		if l.Target == "" {
			d.errorf(field, CodeFieldType, "a relation needs a target")
			continue
		}
		validateLinkTarget(d, cfg, field+".target", l.Target)
	}
}

// validateLinkTarget accepts the bare form "ACME-US-0042", which implies the
// current project, and the qualified form "WEB/WEB-US-0031" (R-LINK-2).
func validateLinkTarget(d *diagSet, cfg *ProjectConfig, field, target string) {
	qualifier := ProjectKey("")
	id := target
	if before, after, found := strings.Cut(target, "/"); found {
		qualifier, id = ProjectKey(before), after
		if !ValidProjectKey(qualifier) {
			d.errorf(field, CodeIDGrammar, "%q does not match <KEY>/<ID>", target)
			return
		}
	}
	key, _, _, err := ParseItemID(id)
	if err != nil {
		d.errorf(field, CodeIDGrammar, "%q does not match <KEY>-<EP|US|T|M>-<NNNN>", target)
		return
	}
	if qualifier != "" {
		if key != qualifier {
			d.errorf(field, CodeIDKey, "%q is qualified with project %q but its id belongs to %q", target, qualifier, key)
		}
		return
	}
	if cfg != nil && cfg.Key != "" && key != cfg.Key {
		d.errorf(field, CodeIDKey, "%q belongs to project %q; qualify it as %q to link across projects", target, key, key.String()+"/"+id)
	}
}

// validateDates applies R-TIME-1 (canonical instants) and E-DATE-ORDER.
func validateDates(d *diagSet, item *Item) {
	stamps := []struct {
		field string
		value Timestamp
	}{
		{"created", item.Created},
		{"updated", item.Updated},
		{"started", item.Started},
		{"closed", item.Closed},
	}
	for _, s := range stamps {
		if s.value.IsZero() {
			continue
		}
		if s.value.Nanosecond() != 0 || s.value.Location() != time.UTC {
			d.errorf(s.field, CodeDateFormat, "%s is not UTC with second precision (%s)", s.value.Format(time.RFC3339Nano), TimestampLayout)
		}
	}

	order := []struct {
		earlyField, lateField string
		early, late           Timestamp
	}{
		{"created", "updated", item.Created, item.Updated},
		{"created", "started", item.Created, item.Started},
		{"created", "closed", item.Created, item.Closed},
		{"started", "closed", item.Started, item.Closed},
	}
	for _, o := range order {
		if o.early.IsZero() || o.late.IsZero() || !o.late.Before(o.early.Time) {
			continue
		}
		d.errorf(o.lateField, CodeDateOrder, "%s (%s) is before %s (%s)", o.lateField, o.late, o.earlyField, o.early)
	}

	if !item.Start.IsZero() && !item.Due.IsZero() && item.Due.Before(item.Start.Time) {
		d.errorf("due", CodeDateOrder, "due (%s) is before start (%s)", item.Due, item.Start)
	}
}

// validateNumbers applies the priority enumeration and the estimation scale.
func validateNumbers(d *diagSet, item *Item, cfg *ProjectConfig) {
	if item.Priority != "" && !priorityAllowed(cfg, item.Priority) {
		d.errorf("priority", CodeEnum, "unknown priority %q, want one of %s", item.Priority, strings.Join(priorityNames(cfg), ", "))
	}

	numbers := []struct {
		field string
		value *float64
	}{
		{"estimate", item.Estimate},
		{"effort", item.Effort},
		{"spent", item.Spent},
	}
	for _, n := range numbers {
		if n.value != nil && *n.value < 0 {
			d.errorf(n.field, CodeFieldType, "%s must not be negative", n.field)
		}
	}

	if item.Estimate == nil || cfg == nil || len(cfg.Estimation.Values) == 0 {
		return
	}
	// Only a numeric scale has values an estimate can be checked against.
	if scale := cfg.Estimation.Scale; scale != "" && scale != "fibonacci" && scale != "linear" {
		return
	}
	for _, v := range cfg.Estimation.Values {
		if v == *item.Estimate {
			return
		}
	}
	d.warnf("estimate", CodeWarnEstimateScale, "%s is not a value of the %s scale", trimFloat(*item.Estimate), scaleName(cfg))
}

// validatePeople applies R-PEOPLE-2: an unknown handle is never an error.
func validatePeople(d *diagSet, item *Item, cfg *ProjectConfig) {
	if cfg == nil || len(cfg.People) == 0 {
		return
	}
	if item.Author != "" && !knownHandle(cfg, item.Author) {
		d.warnf("author", CodeWarnPersonUnknown, "handle %q is not declared in people", item.Author)
	}
	if item.Owner != "" && !knownHandle(cfg, item.Owner) {
		d.warnf("owner", CodeWarnPersonUnknown, "handle %q is not declared in people", item.Owner)
	}
	for _, handle := range item.Assignees {
		if !knownHandle(cfg, handle) {
			d.warnf("assignees", CodeWarnPersonUnknown, "handle %q is not declared in people", handle)
		}
	}
}

// validateLabels applies R-LBL-1: a label outside the catalog is a warning.
func validateLabels(d *diagSet, item *Item, cfg *ProjectConfig) {
	if cfg == nil || len(cfg.Labels) == 0 {
		return
	}
	for _, label := range item.Labels {
		if !cfg.HasLabel(label) {
			d.warnf("labels", CodeWarnLabelUndeclared, "label %q is not in the catalog", label)
		}
	}
}

// validateCustom applies R-CF-2 and R-CF-3 to the custom mapping.
func validateCustom(d *diagSet, item *Item, cfg *ProjectConfig) {
	if cfg == nil {
		return
	}
	for _, key := range sortedKeys(item.Custom) {
		value := item.Custom[key]
		def, ok := customField(cfg, key)
		if !ok {
			d.warnf("custom."+key, CodeWarnCustomUndeclared, "%q is not declared in custom_fields", key)
			continue
		}
		if itemTypeKnown(item.Type) && len(def.AppliesTo) > 0 && !containsType(def.AppliesTo, item.Type) {
			d.warnf("custom."+key, CodeWarnCustomUndeclared, "%q is declared for %s, not for %s", key, typeList(def.AppliesTo), item.Type)
			continue
		}
		if err := checkCustomValue(def.Type, def.Values, def.Items, value); err != nil {
			d.errorf("custom."+key, CodeCustomFieldType, "%s", err)
		}
	}
}

// checkCustomValue reports whether a value matches a declared custom-field type
// (docs/03 section 13.2). An unknown declared type is reported by ValidateProject
// and accepted here, so one bad declaration does not flag every item.
func checkCustomValue(kind string, values []string, items string, value any) error {
	switch kind {
	case "string", "text", "person":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("want a %s, got %T", kind, value)
		}
	case "number":
		if _, err := toFloat(value); err != nil {
			return fmt.Errorf("want a number, got %T", value)
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("want a bool, got %T", value)
		}
	case "date":
		return checkCustomDate(value, false)
	case "timestamp":
		return checkCustomDate(value, true)
	case "enum":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("want one of %s, got %T", strings.Join(values, ", "), value)
		}
		for _, v := range values {
			if v == s {
				return nil
			}
		}
		return fmt.Errorf("%q is not one of %s", s, strings.Join(values, ", "))
	case "url":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("want a url, got %T", value)
		}
		u, err := url.Parse(s)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%q is not an absolute url", s)
		}
	case "list":
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("want a list, got %T", value)
		}
		element := items
		if element == "" {
			element = "string"
		}
		for i, e := range list {
			if err := checkCustomValue(element, values, "", e); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
	default:
		// An unknown declared type is a project.yaml problem, reported by
		// ValidateProject; accepting the value here keeps one bad declaration
		// from flagging every item that uses it.
	}
	return nil
}

// checkCustomDate accepts both the YAML timestamp form, which decodes to a
// time.Time, and the string form (R-TIME-4).
func checkCustomDate(value any, withTime bool) error {
	layout, kind := DateLayout, "date"
	if withTime {
		layout, kind = TimestampLayout, "timestamp"
	}
	switch t := value.(type) {
	case time.Time:
		return nil
	case string:
		var err error
		if withTime {
			_, err = ParseTimestamp(t)
		} else {
			_, err = ParseDate(t)
		}
		if err != nil {
			return fmt.Errorf("%q is not a %s (%s)", t, kind, layout)
		}
	default:
		return fmt.Errorf("want a %s, got %T", kind, value)
	}
	return nil
}

// validateProjectEnums checks the enumerations project.yaml declares.
func validateProjectEnums(d *diagSet, cfg *ProjectConfig) {
	for _, p := range cfg.Priorities {
		if !p.Valid() {
			d.errorf("priorities", CodeEnum, "unknown priority %q; priorities may be reordered, not renamed", p)
		}
	}
	switch cfg.Estimation.Scale {
	case "", "fibonacci", "linear", "tshirt", "none":
	default:
		d.errorf("estimation.scale", CodeEnum, "unknown scale %q, want fibonacci, linear, tshirt or none", cfg.Estimation.Scale)
	}
}

// validateProjectCustomFields checks the custom-field declarations.
func validateProjectCustomFields(d *diagSet, cfg *ProjectConfig) {
	for i, f := range cfg.CustomFields {
		field := fmt.Sprintf("custom_fields[%d]", i)
		if f.Key == "" {
			d.errorf(field, CodeFieldRequired, "a custom field needs a key")
		}
		if !validCustomType(f.Type) {
			d.errorf(field, CodeEnum, "unknown custom field type %q", f.Type)
		}
		if f.Type == "enum" && len(f.Values) == 0 {
			d.errorf(field, CodeFieldRequired, "an enum custom field needs values")
		}
		if f.Type == "list" && f.Items != "" && !validCustomType(f.Items) {
			d.errorf(field, CodeEnum, "unknown list element type %q", f.Items)
		}
		for _, t := range f.AppliesTo {
			if !t.Valid() {
				d.errorf(field, CodeEnum, "unknown item type %q in applies_to", t)
			}
		}
	}
}

// validateProjectDefaults checks that the per-type defaults name declared values.
func validateProjectDefaults(d *diagSet, cfg *ProjectConfig) {
	for _, t := range sortedTypes(cfg.Defaults) {
		def := cfg.Defaults[t]
		field := "defaults." + string(t)
		if !t.Valid() {
			d.errorf("defaults", CodeEnum, "unknown item type %q", t)
			continue
		}
		if def.Status != "" && len(cfg.Workflow.Statuses) > 0 {
			if _, ok := cfg.StatusDef(def.Status); !ok {
				d.errorf(field+".status", CodeStatusUnknown, "%q is not declared in the workflow", def.Status)
			}
		}
		if def.Priority != "" && !priorityAllowed(cfg, def.Priority) {
			d.errorf(field+".priority", CodeEnum, "unknown priority %q", def.Priority)
		}
		for _, label := range def.Labels {
			if len(cfg.Labels) > 0 && !cfg.HasLabel(label) {
				d.warnf(field+".labels", CodeWarnLabelUndeclared, "label %q is not in the catalog", label)
			}
		}
	}
}

// diagSet collects diagnostics about one file.
type diagSet struct {
	path string
	out  []Diagnostic
}

func (d *diagSet) errorf(field string, code Code, format string, args ...any) {
	d.add(code, SeverityError, field, fmt.Sprintf(format, args...))
}

func (d *diagSet) warnf(field string, code Code, format string, args ...any) {
	d.add(code, SeverityWarning, field, fmt.Sprintf(format, args...))
}

func (d *diagSet) add(code Code, severity Severity, field, message string) {
	d.out = append(d.out, Diagnostic{
		Code:     code,
		Severity: severity,
		Path:     d.path,
		Field:    field,
		Message:  message,
	})
}

// orderDiagnostics sorts findings by severity, then code, then field, then
// message, then path, so that a report is byte-identical between two runs and
// between the native and the WASM build.
func orderDiagnostics(diags []Diagnostic) {
	sort.SliceStable(diags, func(i, j int) bool {
		a, b := diags[i], diags[j]
		if a.Severity != b.Severity {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Message != b.Message {
			return a.Message < b.Message
		}
		return a.Path < b.Path
	})
}

// severityRank orders errors before warnings, and anything unknown last.
func severityRank(s Severity) int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

// HasErrors reports whether any diagnostic has error severity, which is what
// decides the exit code of gintrack validate and doctor.
func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// JoinDiagnostics turns the error-severity diagnostics of a validation run into a
// single error, joined with errors.Join so that a caller sees every problem of a
// file at once instead of one per save. Warnings are dropped: they never block a
// write. It returns nil when nothing is wrong.
func JoinDiagnostics(diags []Diagnostic) error {
	var errs []error
	for _, d := range diags {
		if d.Severity == SeverityError {
			errs = append(errs, &DiagnosticError{Diagnostic: d})
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// parentCodes returns the type codes an item of this type may point at with
// parent. An empty result means the type has no parent at all (R-EPIC, R-TASK-1).
func parentCodes(t ItemType) []TypeCode {
	switch t {
	case TypeStory:
		return []TypeCode{CodeEpic}
	case TypeTask:
		return []TypeCode{CodeStory, CodeEpic}
	case TypeEpic, TypeMilestone, TypeComment:
		return nil
	default:
		return nil
	}
}

// itemTypeKnown reports whether a type is one this validator can apply per-type
// rules to. A comment is a known type but never an item.
func itemTypeKnown(t ItemType) bool { return t.Valid() && t != TypeComment }

// typeName renders a type code as the item type it stands for.
func typeName(c TypeCode) string {
	if t, ok := ItemTypeFor(c); ok {
		return string(t)
	}
	return string(c)
}

func containsTypeCode(codes []TypeCode, c TypeCode) bool {
	for _, candidate := range codes {
		if candidate == c {
			return true
		}
	}
	return false
}

// typeList renders a list of item types for a message.
func typeList(types []ItemType) string {
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, string(t))
	}
	return strings.Join(names, ", ")
}

// priorityAllowed reports whether a priority is in the configured set, falling
// back to the four documented priorities when the project declares none.
func priorityAllowed(cfg *ProjectConfig, p Priority) bool {
	if cfg == nil || len(cfg.Priorities) == 0 {
		return p.Valid()
	}
	for _, candidate := range cfg.Priorities {
		if candidate == p {
			return true
		}
	}
	return false
}

// priorityNames lists the accepted priorities for an error message.
func priorityNames(cfg *ProjectConfig) []string {
	source := []Priority{PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow}
	if cfg != nil && len(cfg.Priorities) > 0 {
		source = cfg.Priorities
	}
	names := make([]string, 0, len(source))
	for _, p := range source {
		names = append(names, string(p))
	}
	return names
}

// scaleName returns the configured estimation scale, defaulting to fibonacci.
func scaleName(cfg *ProjectConfig) string {
	if cfg == nil || cfg.Estimation.Scale == "" {
		return "fibonacci"
	}
	return cfg.Estimation.Scale
}

// knownHandle reports whether a handle is declared in project.yaml people.
func knownHandle(cfg *ProjectConfig, handle string) bool {
	for _, p := range cfg.People {
		if p.Handle == handle {
			return true
		}
	}
	return false
}

// customField returns the declaration of a key under custom:.
func customField(cfg *ProjectConfig, key string) (CustomField, bool) {
	if cfg == nil {
		return CustomField{}, false
	}
	for _, f := range cfg.CustomFields {
		if f.Key == key {
			return f, true
		}
	}
	return CustomField{}, false
}

// validCustomType reports whether a custom-field type is one of the documented
// ones (docs/03 section 13.2).
func validCustomType(kind string) bool {
	switch kind {
	case "string", "text", "number", "bool", "date", "timestamp", "enum", "person", "list", "url":
		return true
	default:
		return false
	}
}

// folderType returns the item type the folder of a path may hold, reusing the
// layout table of the indexer (itemFolders). Paths are vault-relative and use
// forward slashes, so no host path handling is needed.
func folderType(path string) (ItemType, bool) {
	folder := folderOf(path)
	for _, f := range itemFolders {
		if f.Dir == folder {
			return f.Type, true
		}
	}
	return "", false
}

// folderOf returns the name of the folder a file lives in, or "".
func folderOf(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return ""
	}
	rest := path[:i]
	if j := strings.LastIndex(rest, "/"); j >= 0 {
		return rest[j+1:]
	}
	return rest
}

// baseName returns the file name of a vault-relative path.
func baseName(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// slugFromPath returns the cosmetic part of a file name: everything after the id
// prefix, without the ".md" extension.
func slugFromPath(path string) string {
	stem := strings.TrimSuffix(baseName(path), ".md")
	if id := IDFromFileName(path); id != "" {
		return strings.TrimPrefix(strings.TrimPrefix(stem, string(id)), "-")
	}
	return stem
}

// sortedTypes returns the keys of a per-type mapping in a deterministic order.
func sortedTypes(m map[ItemType]ItemDefaults) []ItemType {
	types := make([]ItemType, 0, len(m))
	for t := range m {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}
