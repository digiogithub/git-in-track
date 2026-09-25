package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// This file is the requirement grammar lint of ADR-037 section 10 (docs/03
// section 21.9) and its project.yaml key, specs.lint. It is pure text
// processing over a block the parser already cut out, so the web editor runs
// the very same rules live through WebAssembly (R-LINT-3).

// The lint rules. Their codes are prefixed LINT-, not E-/W-, because their
// severity is the project's choice.
const (
	LintReqStatement Code = "LINT-REQ-STATEMENT"
	LintReqScenario  Code = "LINT-REQ-SCENARIO"
	LintReqWhenThen  Code = "LINT-REQ-WHEN-THEN"
	LintReqVague     Code = "LINT-REQ-VAGUE"
	LintReqMulti     Code = "LINT-REQ-MULTI"
)

// LintRules lists every lint rule, in the order docs/03 section 21.9 does.
var LintRules = []Code{LintReqStatement, LintReqScenario, LintReqWhenThen, LintReqVague, LintReqMulti}

// isLintRule reports whether a code names a lint rule.
func isLintRule(c Code) bool {
	for _, r := range LintRules {
		if r == c {
			return true
		}
	}
	return false
}

// DefaultVagueWords is the built-in list of LINT-REQ-VAGUE. A specs.lint
// vague_words key, when present, replaces it.
var DefaultVagueWords = []string{
	"fast", "quickly", "user-friendly", "easy", "as appropriate", "as needed", "etc", "robust",
}

// LintLevel is the severity a project gives a lint rule.
type LintLevel string

// The three lint levels. At off a rule does not run at all.
const (
	LintOff     LintLevel = "off"
	LintWarning LintLevel = "warning"
	LintError   LintLevel = "error"
)

// Valid reports whether the level is one of off, warning and error.
func (l LintLevel) Valid() bool { return l == LintOff || l == LintWarning || l == LintError }

// SpecsConfig is the specs: block of project.yaml (docs/03 section 6).
type SpecsConfig struct {
	Lint *SpecLintConfig `yaml:"lint,omitempty" json:"lint,omitempty"`

	// problems are the shape errors found while decoding, reported as
	// E-PROJ-SPECS by Validate: a bad value never makes project.yaml unreadable.
	problems []specsProblem
}

// SpecLintConfig is specs.lint: a global severity, per-rule overrides and the
// vague-word list.
type SpecLintConfig struct {
	// Severity sets every rule; empty reads as warning.
	Severity LintLevel `json:"severity,omitempty"`
	// Rules overrides the severity of single rules, in both directions.
	Rules map[Code]LintLevel `json:"rules,omitempty"`
	// VagueWords replaces DefaultVagueWords when it is not nil, even empty.
	VagueWords []string `json:"vagueWords,omitempty"`
	// Shorthand records that the file wrote specs.lint as a scalar; a writer
	// keeps that form while nothing but the severity is set.
	Shorthand bool `json:"shorthand,omitempty"`
}

// specsProblem is one E-PROJ-SPECS finding recorded while decoding.
type specsProblem struct {
	field, message string
}

// UnmarshalYAML reads specs:. Anything but a mapping is kept as a problem.
func (s *SpecsConfig) UnmarshalYAML(node *yaml.Node) error {
	*s = SpecsConfig{}
	if node.Kind != yaml.MappingNode {
		if !isNullNode(node) {
			s.problems = append(s.problems, specsProblem{"specs", "want a mapping with lint"})
		}
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value != "lint" {
			continue
		}
		lint, problems := decodeSpecLint(node.Content[i+1])
		s.Lint, s.problems = lint, append(s.problems, problems...)
	}
	return nil
}

// isNullNode reports whether a node is an explicit or implicit YAML null.
func isNullNode(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "" || n.Value == "~" || n.Value == "null")
}

// decodeSpecLint reads specs.lint in either form: the scalar shorthand or the
// mapping. A value of the wrong shape becomes a problem, never a decode error.
func decodeSpecLint(node *yaml.Node) (*SpecLintConfig, []specsProblem) {
	var problems []specsProblem
	switch {
	case isNullNode(node):
		return nil, nil
	case node.Kind == yaml.ScalarNode:
		return &SpecLintConfig{Severity: LintLevel(node.Value), Shorthand: true}, nil
	case node.Kind != yaml.MappingNode:
		return nil, []specsProblem{{"specs.lint", "want off, warning, error or a mapping with severity, rules and vague_words"}}
	}
	cfg := &SpecLintConfig{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "severity":
			if val.Kind != yaml.ScalarNode {
				problems = append(problems, specsProblem{"specs.lint.severity", "want off, warning or error"})
				continue
			}
			cfg.Severity = LintLevel(val.Value)
		case "rules":
			if isNullNode(val) {
				continue
			}
			if val.Kind != yaml.MappingNode {
				problems = append(problems, specsProblem{"specs.lint.rules", "want a mapping of lint rule to off, warning or error"})
				continue
			}
			cfg.Rules = map[Code]LintLevel{}
			for j := 0; j+1 < len(val.Content); j += 2 {
				rule, level := val.Content[j].Value, val.Content[j+1]
				if level.Kind != yaml.ScalarNode {
					problems = append(problems, specsProblem{"specs.lint.rules." + rule, "want off, warning or error"})
					continue
				}
				cfg.Rules[Code(rule)] = LintLevel(level.Value)
			}
		case "vague_words":
			if isNullNode(val) {
				continue
			}
			if val.Kind != yaml.SequenceNode {
				problems = append(problems, specsProblem{"specs.lint.vague_words", "want a list of words"})
				continue
			}
			cfg.VagueWords = []string{}
			for _, w := range val.Content {
				if w.Kind != yaml.ScalarNode {
					problems = append(problems, specsProblem{"specs.lint.vague_words", "want a list of words, got a composite value"})
					continue
				}
				cfg.VagueWords = append(cfg.VagueWords, w.Value)
			}
		}
	}
	return cfg, problems
}

// MarshalYAML writes specs.lint back in the form it was read in: the scalar
// shorthand while only the severity is set, the mapping otherwise.
func (c *SpecLintConfig) MarshalYAML() (any, error) {
	if c.Shorthand && len(c.Rules) == 0 && c.VagueWords == nil {
		return string(c.Severity), nil
	}
	out := struct {
		Severity   string            `yaml:"severity,omitempty"`
		Rules      map[string]string `yaml:"rules,omitempty"`
		VagueWords *[]string         `yaml:"vague_words,omitempty,flow"`
	}{Severity: string(c.Severity)}
	if len(c.Rules) > 0 {
		out.Rules = make(map[string]string, len(c.Rules))
		for k, v := range c.Rules {
			out.Rules[string(k)] = string(v)
		}
	}
	if c.VagueWords != nil {
		words := c.VagueWords
		out.VagueWords = &words
	}
	return out, nil
}

// SpecLint returns the specs.lint configuration of a project, or nil, which
// reads as every rule at warning with the built-in vague words.
func (p *ProjectConfig) SpecLint() *SpecLintConfig {
	if p == nil || p.Specs == nil {
		return nil
	}
	return p.Specs.Lint
}

// Level resolves the severity of one rule: a valid per-rule value wins over
// the global one in both directions, and anything unset or invalid (already
// E-PROJ-SPECS) reads as warning. A nil configuration is all warnings.
func (c *SpecLintConfig) Level(rule Code) LintLevel {
	if c != nil {
		if l, ok := c.Rules[rule]; ok && l.Valid() {
			return l
		}
		if c.Severity.Valid() {
			return c.Severity
		}
	}
	return LintWarning
}

// Words returns the vague-word list in force.
func (c *SpecLintConfig) Words() []string {
	if c == nil || c.VagueWords == nil {
		return DefaultVagueWords
	}
	return c.VagueWords
}

// validate returns the E-PROJ-SPECS findings of the specs: block.
func (s *SpecsConfig) validate() []Diagnostic {
	if s == nil {
		return nil
	}
	var out []Diagnostic
	add := func(field, msg string) {
		out = append(out, Diagnostic{Code: CodeProjSpecs, Severity: SeverityError, Path: ProjectFileName, Field: field, Message: msg})
	}
	for _, p := range s.problems {
		add(p.field, p.message)
	}
	c := s.Lint
	if c == nil {
		return out
	}
	if c.Severity != "" && !c.Severity.Valid() {
		field := "specs.lint.severity"
		if c.Shorthand {
			field = "specs.lint"
		}
		add(field, fmt.Sprintf("%q is not off, warning or error", c.Severity))
	}
	rules := make([]string, 0, len(c.Rules))
	for r := range c.Rules {
		rules = append(rules, string(r))
	}
	sort.Strings(rules)
	for _, r := range rules {
		field := "specs.lint.rules." + r
		if !isLintRule(Code(r)) {
			add(field, fmt.Sprintf("%q is not a lint rule: want one of %s", r, joinCodes(LintRules)))
			continue
		}
		if l := c.Rules[Code(r)]; !l.Valid() {
			add(field, fmt.Sprintf("%q is not off, warning or error", l))
		}
	}
	for i, w := range c.VagueWords {
		if strings.TrimSpace(w) == "" {
			add(fmt.Sprintf("specs.lint.vague_words[%d]", i), "an empty word matches nothing")
		}
	}
	return out
}

// joinCodes renders codes as a comma-separated list.
func joinCodes(codes []Code) string {
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = string(c)
	}
	return strings.Join(parts, ", ")
}

// LintFinding is one grammar-lint finding: the requirement it is about, the
// rule, the severity the project gives that rule and the 1-based body line.
type LintFinding struct {
	Ref      RequirementRef `json:"ref"`
	Rule     Code           `json:"rule"`
	Severity Severity       `json:"severity"`
	Line     int            `json:"line"`
	Message  string         `json:"message"`
}

// LintSpec parses a spec body and lints every requirement block in it.
func LintSpec(spec ItemID, body string, cfg *SpecLintConfig) []LintFinding {
	var out []LintFinding
	for _, blk := range ParseSpecBody(spec, body).Blocks {
		out = append(out, LintRequirement(blk, cfg)...)
	}
	return out
}

// LintRequirement checks one block against the requirement grammar of ADR-037
// section 2: a statement in one EARS pattern or a plain SHALL sentence with
// uppercase keywords, at least one scenario, WHEN before THEN in every
// scenario, no vague words and a single SHALL. A rule at off does not run. The
// findings are ordered by line, then rule.
func LintRequirement(blk RequirementBlock, cfg *SpecLintConfig) []LintFinding {
	return lintBlockText(blk.Ref, blk.Ref.String(), blk.Text, blk.Line, cfg)
}

// lintBlockText runs the grammar lint over the text of one block whose heading is
// on body line headingLine. ref goes into the findings and name into their
// messages, so that a Spec Delta block, which has no number until it is
// applied, is linted by exactly the rules a spec block is (R-DELTA-5).
func lintBlockText(ref RequirementRef, name, text string, headingLine int, cfg *SpecLintConfig) []LintFinding {
	lines := splitSpecLines(text)
	if len(lines) == 0 {
		return nil
	}
	scan := scanBlockContent(lines[1:], headingLine)
	l := &linter{ref: ref, name: name, cfg: cfg}

	if l.on(LintReqStatement) || l.on(LintReqMulti) {
		l.statement(headingLine, scan.statement)
	}
	if l.on(LintReqScenario) && len(scan.scenarios) == 0 {
		l.add(LintReqScenario, headingLine, "%s has no \"#### Scenario:\": want at least one, with **WHEN** and **THEN** steps", l.name)
	}
	if l.on(LintReqWhenThen) {
		for _, sc := range scan.scenarios {
			l.steps(sc)
		}
	}
	if l.on(LintReqVague) {
		words := l.cfg.Words()
		for _, ln := range scan.statement {
			l.vague(ln, words)
		}
		for _, sc := range scan.scenarios {
			l.vague(numberedLine{text: sc.name, line: sc.line}, words)
			for _, st := range sc.steps {
				l.vague(st, words)
			}
		}
	}
	sort.SliceStable(l.out, func(i, j int) bool {
		if l.out[i].Line != l.out[j].Line {
			return l.out[i].Line < l.out[j].Line
		}
		return ruleOrder(l.out[i].Rule) < ruleOrder(l.out[j].Rule)
	})
	return l.out
}

// ruleOrder is the position of a rule in LintRules.
func ruleOrder(c Code) int {
	for i, r := range LintRules {
		if r == c {
			return i
		}
	}
	return len(LintRules)
}

// linter collects the findings of one block.
type linter struct {
	ref RequirementRef
	// name is how the messages call the block: the ref of a spec block, the
	// operation and target of a Spec Delta block.
	name string
	cfg  *SpecLintConfig
	out  []LintFinding
}

// on reports whether a rule runs.
func (l *linter) on(rule Code) bool { return l.cfg.Level(rule) != LintOff }

// add records a finding of a rule that is on.
func (l *linter) add(rule Code, line int, format string, args ...any) {
	level := l.cfg.Level(rule)
	if level == LintOff {
		return
	}
	sev := SeverityWarning
	if level == LintError {
		sev = SeverityError
	}
	l.out = append(l.out, LintFinding{Ref: l.ref, Rule: rule, Severity: sev, Line: line, Message: fmt.Sprintf(format, args...)})
}

var (
	// shallRE is the SHALL keyword, uppercase and a whole word.
	shallRE = regexp.MustCompile(`\bSHALL\b`)
	// shallAnyCaseRE is the same word in any case.
	shallAnyCaseRE = regexp.MustCompile(`(?i)\bshall\b`)
	// thenRE is the THEN of an unwanted-behavior statement.
	thenRE = regexp.MustCompile(`\bTHEN\b`)
	// earsLeadRE is a statement opening with an EARS keyword, in any case.
	earsLeadRE = regexp.MustCompile(`^(?i:(when|while|where|if))\b`)
	// codeSpanRE is an inline code span; its content is never prose.
	codeSpanRE = regexp.MustCompile("`[^`]*`")
	// stepKeywordRE is the bold keyword that opens a scenario step.
	stepKeywordRE = regexp.MustCompile(`^\*\*([A-Za-z]+)\*\*`)
)

// stripCode blanks the inline code spans of a line, keeping its length, so a
// literal like `SHALL` or `fast` inside backticks is not read as prose.
func stripCode(s string) string {
	return codeSpanRE.ReplaceAllStringFunc(s, func(m string) string { return strings.Repeat(" ", len(m)) })
}

// statement applies LINT-REQ-STATEMENT and LINT-REQ-MULTI.
func (l *linter) statement(headingLine int, stmt []numberedLine) {
	if len(stmt) == 0 {
		l.add(LintReqStatement, headingLine, "%s has no statement: want one EARS pattern or a SHALL sentence right after the heading", l.name)
		return
	}
	first := stmt[0].line
	parts := make([]string, len(stmt))
	count, secondLine := 0, 0
	for i, ln := range stmt {
		parts[i] = stripCode(ln.text)
		for range shallRE.FindAllStringIndex(parts[i], -1) {
			count++
			if count == 2 {
				secondLine = ln.line
			}
		}
	}
	text := strings.TrimLeft(strings.Join(parts, " "), " *_")

	if l.on(LintReqStatement) {
		switch {
		case count == 0 && shallAnyCaseRE.MatchString(text):
			l.add(LintReqStatement, first, "the statement of %s writes %q in lower case: EARS keywords are uppercase (SHALL)",
				l.name, shallAnyCaseRE.FindString(text))
		case count == 0:
			l.add(LintReqStatement, first, "the statement of %s has no SHALL: want one EARS pattern "+
				"(The <system> SHALL …; WHEN/WHILE/WHERE <condition>, the <system> SHALL …; IF <condition>, THEN the <system> SHALL …) "+
				"or a plain SHALL sentence", l.name)
		default:
			l.earsShape(first, text)
		}
	}
	if count > 1 && l.on(LintReqMulti) {
		l.add(LintReqMulti, secondLine, "the statement of %s has %d SHALLs: one requirement per block", l.name, count)
	}
}

// earsShape checks the shape of a statement that opens with an EARS keyword.
func (l *linter) earsShape(line int, text string) {
	m := earsLeadRE.FindStringSubmatch(text)
	if m == nil {
		return
	}
	kw, upper := m[1], strings.ToUpper(m[1])
	if kw != upper {
		l.add(LintReqStatement, line, "the statement of %s opens with %q: EARS keywords are uppercase (%s)", l.name, kw, upper)
		return
	}
	shallAt := shallRE.FindStringIndex(text)[0]
	head := text[:shallAt]
	if upper == "IF" {
		if !thenRE.MatchString(head) {
			l.add(LintReqStatement, line, "the statement of %s opens with IF but has no THEN before SHALL: want IF <condition>, THEN the <system> SHALL …", l.name)
		}
		return
	}
	if !strings.Contains(head, ",") {
		l.add(LintReqStatement, line, "the statement of %s opens with %s but has no comma before SHALL: want %s <condition>, the <system> SHALL …", l.name, upper, upper)
	}
}

// stepKeywords are the keywords a scenario step opens with.
var stepKeywords = map[string]bool{"GIVEN": true, "WHEN": true, "AND": true, "THEN": true}

// steps applies LINT-REQ-WHEN-THEN to one scenario: at least one WHEN and one
// THEN, the first WHEN before the first THEN, GIVEN only before the first
// WHEN, and every top-level step opened by an uppercase keyword. Indented
// bullets are sub-points of a step and are not checked.
func (l *linter) steps(sc scannedScenario) {
	whenAt, thenAt := -1, -1
	for _, st := range sc.steps {
		if st.indent >= 2 {
			continue
		}
		m := stepKeywordRE.FindStringSubmatch(st.text)
		kw := ""
		if m != nil {
			kw = m[1]
		}
		switch {
		case kw == "":
			l.add(LintReqWhenThen, st.line, "a step of scenario %q has no **GIVEN**, **WHEN**, **AND** or **THEN** keyword", sc.name)
		case !stepKeywords[kw] && stepKeywords[strings.ToUpper(kw)]:
			l.add(LintReqWhenThen, st.line, "step keyword **%s** of scenario %q is not uppercase: write **%s**", kw, sc.name, strings.ToUpper(kw))
		case !stepKeywords[kw]:
			l.add(LintReqWhenThen, st.line, "step keyword **%s** of scenario %q is not GIVEN, WHEN, AND or THEN", kw, sc.name)
		case kw == "GIVEN" && whenAt >= 0:
			l.add(LintReqWhenThen, st.line, "a **GIVEN** step of scenario %q follows a **WHEN**: GIVEN comes first", sc.name)
		case kw == "WHEN" && whenAt < 0:
			whenAt = st.line
		case kw == "THEN" && thenAt < 0:
			thenAt = st.line
		}
	}
	switch {
	case whenAt < 0 && thenAt < 0:
		l.add(LintReqWhenThen, sc.line, "scenario %q has no **WHEN** and no **THEN** step", sc.name)
	case whenAt < 0:
		l.add(LintReqWhenThen, sc.line, "scenario %q has no **WHEN** step", sc.name)
	case thenAt < 0:
		l.add(LintReqWhenThen, sc.line, "scenario %q has no **THEN** step", sc.name)
	case thenAt < whenAt:
		l.add(LintReqWhenThen, thenAt, "scenario %q has a **THEN** before its first **WHEN**", sc.name)
	}
}

// vague applies LINT-REQ-VAGUE to one line: each word of the list found in it,
// whole word and case-insensitive, is one finding. Inline code is skipped.
func (l *linter) vague(ln numberedLine, words []string) {
	text := normalizeSpace(strings.ToLower(stripCode(ln.text)))
	for _, w := range words {
		w = normalizeSpace(strings.ToLower(strings.TrimSpace(w)))
		if w == "" {
			continue
		}
		if containsWholeWord(text, w) {
			l.add(LintReqVague, ln.line, "vague word %q in %s: state a measurable, observable condition instead", w, l.name)
		}
	}
}

// normalizeSpace collapses every run of white space to one space.
func normalizeSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// containsWholeWord reports whether w occurs in s with no letter, digit or
// underscore on either side.
func containsWholeWord(s, w string) bool {
	for from := 0; from <= len(s)-len(w); {
		i := strings.Index(s[from:], w)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(w)
		before, _ := utf8.DecodeLastRuneInString(s[:start])
		after, _ := utf8.DecodeRuneInString(s[end:])
		if (start == 0 || !isWordRune(before)) && (end == len(s) || !isWordRune(after)) {
			return true
		}
		_, size := utf8.DecodeRuneInString(s[start:])
		from = start + size
	}
	return false
}

// isWordRune reports whether a rune is part of a word.
func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// lintDiagnostics renders the lint findings of a block as diagnostics of the
// spec file, in the same "line N of the body" form as the parser's findings.
func lintDiagnostics(d *diagSet, blk RequirementBlock, cfg *SpecLintConfig) {
	for _, f := range LintRequirement(blk, cfg) {
		d.add(f.Rule, f.Severity, "body."+f.Ref.String(), fmt.Sprintf("line %d of the body: %s", f.Line, f.Message))
	}
}
