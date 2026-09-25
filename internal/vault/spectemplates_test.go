package vault

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// TestSpecTemplates answers the effective templates through the contract, as
// browser-only mode calls it: the embedded pair by default, an override that
// is part of the loaded vault, and the embedded copy in place of an invalid
// one (GIT-US-0162, ADR-038).
func TestSpecTemplates(t *testing.T) {
	t.Parallel()
	const custom = "The <system> SHALL <response>.\n\n#### Scenario: <name>\n- **WHEN** <action>\n- **THEN** <result>\n"
	tests := []struct {
		name      string
		overrides map[string]string
		wantReq   string
		wantSrc   string
		wantCodes []core.Code
	}{
		{name: "embedded", wantReq: core.RequirementTemplate(), wantSrc: core.TemplateSourceEmbedded},
		{
			name:      "override",
			overrides: map[string]string{"docs/.pmngr/templates/requirement.md": custom},
			wantReq:   custom, wantSrc: "docs/.pmngr/templates/requirement.md",
		},
		{
			name:      "invalid override",
			overrides: map[string]string{"docs/.pmngr/templates/requirement.md": "### Heading\n"},
			wantReq:   core.RequirementTemplate(), wantSrc: core.TemplateSourceEmbedded,
			wantCodes: []core.Code{core.CodeWarnTemplateInvalid},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := fixtureFiles(t)
			for p, text := range tt.overrides {
				files = append(files, map[string]string{"path": p, "text": text})
			}
			v := NewInMemory()
			call(t, v, "vault.load", map[string]any{"files": files})
			got := decode[core.SpecTemplates](t, call(t, v, "spec.templates", map[string]any{"project": "DEMO"}))
			if got.Requirement != tt.wantReq || got.RequirementSource != tt.wantSrc {
				t.Errorf("requirement = %q from %q, want %q from %q", got.Requirement, got.RequirementSource, tt.wantReq, tt.wantSrc)
			}
			if got.Spec != core.SpecTemplate() || got.SpecSource != core.TemplateSourceEmbedded {
				t.Errorf("spec = %q from %q", got.Spec, got.SpecSource)
			}
			if len(got.Diagnostics) != len(tt.wantCodes) {
				t.Fatalf("diagnostics = %+v, want %v", got.Diagnostics, tt.wantCodes)
			}
			for i, code := range tt.wantCodes {
				if got.Diagnostics[i].Code != code {
					t.Errorf("diagnostic %d = %s, want %s", i, got.Diagnostics[i].Code, code)
				}
			}
		})
	}

	t.Run("unknown project", func(t *testing.T) {
		t.Parallel()
		v, _ := loadedVault(t)
		if env := rawCall(t, v, "spec.templates", map[string]any{"project": "NOPE"}); env.OK || env.Error.Code != "not_found" {
			t.Errorf("envelope = %+v", env)
		}
	})
}
