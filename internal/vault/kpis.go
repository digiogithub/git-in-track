package vault

import (
	"github.com/digiogithub/git-in-track/internal/core"
)

// projectKPIs answers "project.kpis" (GIT-US-0205): the workspace-home figures
// of one project of this repository — knowledge-base pages, milestones, epics,
// stories, specs and the estimated progress — computed by the core over the
// index, so the companion and browser-only mode report the same numbers.
func (v *Vault) projectKPIs(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Project string `json:"project,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	key, _, err := v.projectConfig(core.ProjectKey(p.Project))
	if err != nil {
		return nil, err
	}
	kpis, ok := v.index.ProjectKPIs(key)
	if !ok {
		return nil, failf("not_found", "project %q is not open in this repository", key)
	}
	return kpis, nil
}
