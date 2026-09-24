package core

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// lintBlock builds a one-block spec body from a statement and the lines after
// it, and lints it.
func lintBlock(t *testing.T, rest string, cfg *SpecLintConfig) []LintFinding {
	t.Helper()
	body := "## Requirements\n\n### ACME-SP-0001.R1 — Title\n\n" + rest
	return LintSpec("ACME-SP-0001", body, cfg)
}

// lintSummary renders findings as "RULE@line" for compact comparisons.
func lintSummary(fs []LintFinding) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%s@%d", f.Rule, f.Line))
	}
	return out
}

const goodScenario = "\n#### Scenario: s\n- **WHEN** it happens\n- **THEN** it is observed\n"

func TestLintStatement(t *testing.T) {
	t.Parallel()
	// Line 5 is the statement line, line 3 the heading.
	tests := []struct {
		name      string
		statement string
		want      []string
	}{
		{"ubiquitous", "The system SHALL do one thing.", nil},
		{"shall not", "The system SHALL NOT drop a write.", nil},
		{"event", "WHEN a file changes, the index SHALL refresh.", nil},
		{"state", "WHILE offline, the app SHALL queue writes.", nil},
		{"optional feature", "WHERE git is present, the tool SHALL commit.", nil},
		{"unwanted", "IF the rev is stale, THEN the store SHALL refuse.", nil},
		{"complex", "WHILE online, WHEN a push fails, the tool SHALL retry.", nil},
		{"emphasized keyword", "**WHEN** a file changes, the index SHALL refresh.", nil},
		{"no shall", "The system does one thing.", []string{"LINT-REQ-STATEMENT@5"}},
		{"lower-case shall", "The system shall do one thing.", []string{"LINT-REQ-STATEMENT@5"}},
		{"lower-case EARS keyword", "When a file changes, the index SHALL refresh.", []string{"LINT-REQ-STATEMENT@5"}},
		{"WHEN without comma", "WHEN a file changes the index SHALL refresh.", []string{"LINT-REQ-STATEMENT@5"}},
		{"IF without THEN", "IF the rev is stale, the store SHALL refuse.", []string{"LINT-REQ-STATEMENT@5"}},
		{"lower-case then", "IF the rev is stale, then the store SHALL refuse.", []string{"LINT-REQ-STATEMENT@5"}},
		{"shall only in code", "The system writes `SHALL` into the file.", []string{"LINT-REQ-STATEMENT@5"}},
		{"shallow is not shall", "The system keeps a shallow copy.", []string{"LINT-REQ-STATEMENT@5"}},
		{"multi on one line", "The system SHALL read and SHALL write.", []string{"LINT-REQ-MULTI@5"}},
		{"multi on the next line", "The system SHALL read\nand SHALL write.", []string{"LINT-REQ-MULTI@6"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lintSummary(lintBlock(t, tt.statement+"\n"+goodScenario, nil))
			want := tt.want
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("findings = %v, want %v", got, want)
			}
		})
	}

	t.Run("no statement", func(t *testing.T) {
		t.Parallel()
		got := lintSummary(lintBlock(t, strings.TrimPrefix(goodScenario, "\n"), nil))
		if want := []string{"LINT-REQ-STATEMENT@3"}; !reflect.DeepEqual(got, want) {
			t.Errorf("findings = %v, want %v", got, want)
		}
	})
}

func TestLintScenarios(t *testing.T) {
	t.Parallel()
	// The statement is line 5, the first scenario heading line 7.
	tests := []struct {
		name      string
		scenarios string
		want      []string
	}{
		{"none", "", []string{"LINT-REQ-SCENARIO@3"}},
		{"only prose", "Some notes.\n", []string{"LINT-REQ-SCENARIO@3"}},
		{"good", "#### Scenario: s\n- **WHEN** a\n- **THEN** b\n", nil},
		{"given and and", "#### Scenario: s\n- **GIVEN** a\n- **WHEN** b\n- **AND** c\n- **THEN** d\n- **AND** e\n", nil},
		{"nested bullets are sub-points", "#### Scenario: s\n- **WHEN** a\n  - detail\n- **THEN** b\n", nil},
		{"no steps", "#### Scenario: s\n", []string{"LINT-REQ-WHEN-THEN@7"}},
		{"no THEN", "#### Scenario: s\n- **WHEN** a\n", []string{"LINT-REQ-WHEN-THEN@7"}},
		{"no WHEN", "#### Scenario: s\n- **THEN** a\n", []string{"LINT-REQ-WHEN-THEN@7"}},
		{"THEN before WHEN", "#### Scenario: s\n- **THEN** a\n- **WHEN** b\n", []string{"LINT-REQ-WHEN-THEN@8"}},
		{"GIVEN after WHEN", "#### Scenario: s\n- **WHEN** a\n- **GIVEN** b\n- **THEN** c\n", []string{"LINT-REQ-WHEN-THEN@9"}},
		{"lower-case keyword", "#### Scenario: s\n- **when** a\n- **WHEN** a\n- **THEN** b\n", []string{"LINT-REQ-WHEN-THEN@8"}},
		{"unknown keyword", "#### Scenario: s\n- **WHEN** a\n- **SO** b\n- **THEN** c\n", []string{"LINT-REQ-WHEN-THEN@9"}},
		{"step without keyword", "#### Scenario: s\n- **WHEN** a\n- b\n- **THEN** c\n", []string{"LINT-REQ-WHEN-THEN@9"}},
		{"each scenario is checked", "#### Scenario: s\n- **WHEN** a\n- **THEN** b\n\n#### Scenario: t\n- **WHEN** a\n",
			[]string{"LINT-REQ-WHEN-THEN@11"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lintSummary(lintBlock(t, "The system SHALL act.\n\n"+tt.scenarios, nil))
			want := tt.want
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("findings = %v, want %v", got, want)
			}
		})
	}
}

func TestLintVague(t *testing.T) {
	t.Parallel()
	custom := &SpecLintConfig{VagueWords: []string{"Snappy", "in a  timely manner", "rápido"}}
	tests := []struct {
		name string
		text string
		cfg  *SpecLintConfig
		want []string
	}{
		{"default word in statement", "The system SHALL respond fast.", nil, []string{"LINT-REQ-VAGUE@5"}},
		{"case-insensitive", "The system SHALL be Robust.", nil, []string{"LINT-REQ-VAGUE@5"}},
		{"hyphenated word", "The UI SHALL be user-friendly.", nil, []string{"LINT-REQ-VAGUE@5"}},
		{"phrase", "The system SHALL log errors as appropriate.", nil, []string{"LINT-REQ-VAGUE@5"}},
		{"two words, two findings", "The system SHALL be fast and easy.", nil, []string{"LINT-REQ-VAGUE@5", "LINT-REQ-VAGUE@5"}},
		{"whole word only", "The system SHALL use a breakfast and fastest easyjet.", nil, nil},
		{"code span is skipped", "The system SHALL set `fast: true`.", nil, nil},
		{"custom list replaces the default", "The system SHALL be fast and snappy.", custom, []string{"LINT-REQ-VAGUE@5"}},
		{"custom phrase with spacing", "The system SHALL answer in a timely manner.", custom, []string{"LINT-REQ-VAGUE@5"}},
		{"non-ASCII word", "El sistema SHALL ser rápido.", custom, []string{"LINT-REQ-VAGUE@5"}},
		{"empty list disables the words", "The system SHALL be fast.", &SpecLintConfig{VagueWords: []string{}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lintSummary(lintBlock(t, tt.text+"\n"+goodScenario, tt.cfg))
			want := tt.want
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("findings = %v, want %v", got, want)
			}
		})
	}

	t.Run("scenario name and steps", func(t *testing.T) {
		t.Parallel()
		got := lintSummary(lintBlock(t, "The system SHALL act.\n\n#### Scenario: a quick, easy path\n- **WHEN** it runs quickly\n- **THEN** it is done etc.\n", nil))
		want := []string{"LINT-REQ-VAGUE@7", "LINT-REQ-VAGUE@8", "LINT-REQ-VAGUE@9"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("findings = %v, want %v", got, want)
		}
	})
}

func TestLintSeverityResolution(t *testing.T) {
	t.Parallel()
	// This block breaks STATEMENT (no SHALL), SCENARIO and VAGUE.
	const rest = "The system is fast.\n"
	tests := []struct {
		name string
		cfg  *SpecLintConfig
		want map[Code]Severity
	}{
		{"nil config is all warnings", nil, map[Code]Severity{
			LintReqStatement: SeverityWarning, LintReqScenario: SeverityWarning, LintReqVague: SeverityWarning}},
		{"global error", &SpecLintConfig{Severity: LintError}, map[Code]Severity{
			LintReqStatement: SeverityError, LintReqScenario: SeverityError, LintReqVague: SeverityError}},
		{"global off", &SpecLintConfig{Severity: LintOff}, map[Code]Severity{}},
		{"rule raises one", &SpecLintConfig{Rules: map[Code]LintLevel{LintReqVague: LintError}}, map[Code]Severity{
			LintReqStatement: SeverityWarning, LintReqScenario: SeverityWarning, LintReqVague: SeverityError}},
		{"rule silences one", &SpecLintConfig{Severity: LintError, Rules: map[Code]LintLevel{LintReqScenario: LintOff}}, map[Code]Severity{
			LintReqStatement: SeverityError, LintReqVague: SeverityError}},
		{"rule wins over global off", &SpecLintConfig{Severity: LintOff, Rules: map[Code]LintLevel{LintReqVague: LintWarning}}, map[Code]Severity{
			LintReqVague: SeverityWarning}},
		{"invalid values read as warning", &SpecLintConfig{Severity: "loud", Rules: map[Code]LintLevel{LintReqVague: "never"}}, map[Code]Severity{
			LintReqStatement: SeverityWarning, LintReqScenario: SeverityWarning, LintReqVague: SeverityWarning}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := map[Code]Severity{}
			for _, f := range lintBlock(t, rest, tt.cfg) {
				if f.Ref.String() != "ACME-SP-0001.R1" {
					t.Errorf("finding ref = %s", f.Ref)
				}
				got[f.Rule] = f.Severity
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("severities = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLintConfigDecode(t *testing.T) {
	t.Parallel()
	base := "schema: 2\nkey: ACME\nname: Acme\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n" +
		"    - {id: done, category: done}\n  initial: todo\n"
	tests := []struct {
		name      string
		specs     string
		wantLevel map[Code]LintLevel
		wantWords []string
		wantDiags []string
	}{
		{"absent", "", map[Code]LintLevel{LintReqVague: LintWarning}, DefaultVagueWords, nil},
		{"scalar shorthand", "specs:\n  lint: error\n", map[Code]LintLevel{LintReqVague: LintError, LintReqMulti: LintError}, DefaultVagueWords, nil},
		{"shorthand off", "specs:\n  lint: off\n", map[Code]LintLevel{LintReqStatement: LintOff}, DefaultVagueWords, nil},
		{"mapping", "specs:\n  lint:\n    severity: off\n    rules:\n      LINT-REQ-VAGUE: error\n    vague_words: [snappy]\n",
			map[Code]LintLevel{LintReqVague: LintError, LintReqScenario: LintOff}, []string{"snappy"}, nil},
		{"empty vague list", "specs:\n  lint:\n    vague_words: []\n", map[Code]LintLevel{LintReqVague: LintWarning}, []string{}, nil},
		{"bad shorthand", "specs:\n  lint: loud\n", map[Code]LintLevel{LintReqVague: LintWarning}, DefaultVagueWords,
			[]string{"E-PROJ-SPECS specs.lint"}},
		{"bad severity", "specs:\n  lint:\n    severity: fatal\n", nil, nil, []string{"E-PROJ-SPECS specs.lint.severity"}},
		{"unknown rule and bad value", "specs:\n  lint:\n    rules:\n      LINT-REQ-TYPO: error\n      LINT-REQ-MULTI: maybe\n", nil, nil,
			[]string{"E-PROJ-SPECS specs.lint.rules.LINT-REQ-MULTI", "E-PROJ-SPECS specs.lint.rules.LINT-REQ-TYPO"}},
		{"wrong shapes", "specs:\n  lint:\n    severity: [error]\n    rules: error\n    vague_words: fast\n", nil, nil,
			[]string{"E-PROJ-SPECS specs.lint.rules", "E-PROJ-SPECS specs.lint.severity", "E-PROJ-SPECS specs.lint.vague_words"}},
		{"specs not a mapping", "specs: off\n", nil, nil, []string{"E-PROJ-SPECS specs"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := LoadProjectConfig([]byte(base + tt.specs))
			if cfg == nil {
				t.Fatalf("LoadProjectConfig() = nil, %v", err)
			}
			var diags []string
			for _, d := range cfg.Validate() {
				if d.Code == CodeProjSpecs {
					diags = append(diags, string(d.Code)+" "+d.Field)
				}
			}
			if !reflect.DeepEqual(diags, tt.wantDiags) {
				t.Errorf("diagnostics = %v, want %v", diags, tt.wantDiags)
			}
			if (err != nil) != (len(tt.wantDiags) > 0) {
				t.Errorf("LoadProjectConfig() error = %v", err)
			}
			for rule, want := range tt.wantLevel {
				if got := cfg.SpecLint().Level(rule); got != want {
					t.Errorf("Level(%s) = %q, want %q", rule, got, want)
				}
			}
			if tt.wantWords != nil && !reflect.DeepEqual(cfg.SpecLint().Words(), tt.wantWords) {
				t.Errorf("Words() = %v, want %v", cfg.SpecLint().Words(), tt.wantWords)
			}
		})
	}
}

func TestLintConfigKeepsItsForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"shorthand", "specs:\n  lint: error\n", "specs:\n  lint: error\n"},
		{"mapping", "specs:\n  lint:\n    severity: warning\n    rules:\n      LINT-REQ-VAGUE: error\n    vague_words: [fast, as needed]\n",
			"specs:\n  lint:\n    severity: warning\n    rules:\n      LINT-REQ-VAGUE: error\n    vague_words: [fast, as needed]\n"},
		{"severity-only mapping", "specs:\n  lint:\n    severity: off\n", "specs:\n  lint:\n    severity: \"off\"\n"},
	}
	base := "schema: 2\nkey: ACME\nname: Acme\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n  initial: todo\n"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, _ := LoadProjectConfig([]byte(base + tt.in))
			if cfg == nil {
				t.Fatal("LoadProjectConfig() = nil")
			}
			data, err := marshalProjectConfig(*cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tt.want) {
				t.Errorf("marshaled project.yaml lacks %q:\n%s", tt.want, data)
			}
			again, _ := LoadProjectConfig(data)
			if again == nil || !reflect.DeepEqual(again.SpecLint(), cfg.SpecLint()) {
				t.Errorf("round trip changed specs.lint: %+v, want %+v", again.SpecLint(), cfg.SpecLint())
			}
		})
	}
}

func TestLintSeverityGatesValidation(t *testing.T) {
	t.Parallel()
	item := validSpec(t)
	item.Body = strings.Replace(item.Body, "The system SHALL do two.", "The system does two, fast.", 1)

	tests := []struct {
		name       string
		lint       *SpecLintConfig
		wantErrors bool
		wantCodes  []Code
	}{
		{"default warns", nil, false, []Code{LintReqStatement, LintReqVague}},
		{"error blocks", &SpecLintConfig{Severity: LintError}, true, []Code{LintReqStatement, LintReqVague}},
		{"off is silent", &SpecLintConfig{Severity: LintOff}, false, nil},
		{"one rule raised", &SpecLintConfig{Rules: map[Code]LintLevel{LintReqVague: LintError}}, true, []Code{LintReqVague, LintReqStatement}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := specConfig(t)
			if tt.lint != nil {
				cfg.Specs = &SpecsConfig{Lint: tt.lint}
			}
			diags := ValidateItem(item, cfg)
			if got := HasErrors(diags); got != tt.wantErrors {
				t.Errorf("HasErrors() = %v, want %v; diagnostics:\n%v", got, tt.wantErrors, diags)
			}
			var codes []Code
			for _, d := range diags {
				codes = append(codes, d.Code)
				if d.Field != "body.TEST-SP-0001.R2" || !strings.HasPrefix(d.Message, "line 13 of the body: ") {
					t.Errorf("diagnostic %v does not point at R2's statement", d)
				}
			}
			if !reflect.DeepEqual(codes, tt.wantCodes) {
				t.Errorf("codes = %v, want %v", codes, tt.wantCodes)
			}
		})
	}
}

func TestLintGolden(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "spec-lint-body.md"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &SpecLintConfig{Rules: map[Code]LintLevel{LintReqMulti: LintError}}
	var b strings.Builder
	b.WriteString("# LintSpec(ACME-SP-0004, spec-lint-body.md) with LINT-REQ-MULTI at error\n")
	for _, f := range LintSpec("ACME-SP-0004", string(data), cfg) {
		fmt.Fprintf(&b, "%s:%d %s %s: %s\n", f.Ref, f.Line, f.Severity, f.Rule, f.Message)
	}
	compareGolden(t, "spec-lint.txt", []byte(b.String()))
}
