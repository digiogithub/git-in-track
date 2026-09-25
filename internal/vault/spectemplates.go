package vault

import (
	"github.com/digiogithub/git-in-track/internal/core"
)

// specTemplates answers "spec.templates" (ADR-038, GIT-US-0162): the templates
// a new spec and a new requirement of one project start from — each override
// file of <docs>/.pmngr/templates/ that is valid, the embedded copy otherwise —
// with where each came from and the findings about the override files. It is a
// read: the web editor, the Add-requirement dialog and the MCP create tools
// call it, so the companion and browser-only mode resolve the same text
// through the vault's file system.
func (v *Vault) specTemplates(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Project string `json:"project,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	key, cfg, err := v.projectConfig(core.ProjectKey(p.Project))
	if err != nil {
		return nil, err
	}
	for _, ref := range v.projects {
		if !ref.Team && ref.Key == key {
			return core.LoadSpecTemplates(v.fs, ref.BacklogPath, cfg), nil
		}
	}
	return nil, failf("not_found", "project %q is not open in this repository", key)
}
