package vault

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// projectCreateParams is the input of "project.create": where the backlog goes
// and what identity it starts with.
type projectCreateParams struct {
	// VaultID routes the call; it is read by the workspace router, not here.
	VaultID string `json:"vaultId,omitempty"`
	// DocsFolder is the documentation folder, vault-relative. An empty value
	// and "." both mean the repository root.
	DocsFolder string `json:"docsFolder,omitempty"`
	// Key is the ID prefix, matching [A-Z][A-Z0-9]{1,9}.
	Key string `json:"key"`
	// Name is the human name; it defaults to the key.
	Name string `json:"name,omitempty"`
	// Description is one optional paragraph.
	Description string `json:"description,omitempty"`
	// Timezone is an IANA name; it defaults to UTC.
	Timezone string `json:"timezone,omitempty"`
}

// projectCreated is what "project.create" answers with: the project as
// "project.list" reports it, and the files the host must persist.
type projectCreated struct {
	Project projectSummary `json:"project"`
	Writes  WriteSet       `json:"writes"`
}

// projectCreate scaffolds a backlog in this repository and re-indexes it.
//
// The whole decision lives in internal/core: this method only decodes the
// request, declares the folder so that discovery keeps finding it however deep
// it is, and reports what was written. The caller holds the vault lock.
func (v *Vault) projectCreate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[projectCreateParams](raw)
	if err != nil {
		return nil, err
	}
	docs := path.Clean(strings.TrimSpace(p.DocsFolder))
	if docs == "" {
		docs = "."
	}
	key := core.ProjectKey(strings.TrimSpace(p.Key))

	v.fs.begin()
	ref, err := core.CreateProject(v.fs, docs, core.NewProject{
		Key:         key,
		Name:        p.Name,
		Description: p.Description,
		Timezone:    p.Timezone,
	})
	if err != nil {
		return nil, classifyCreateProject(err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}

	// A folder the bounded rule cannot reach on its own is declared here, so
	// that this vault keeps finding the project it has just created. The host
	// records the same folder in its own registration (ADR-018).
	v.docsFolders = appendFolder(v.docsFolders, ref.DocsPath)
	if _, err := v.reload(ctx); err != nil {
		return nil, err
	}

	for _, summary := range v.projectList() {
		if summary.Key == string(ref.Key) {
			return projectCreated{Project: summary, Writes: writes}, nil
		}
	}
	return nil, failf("internal", "project %s was written to %s but is not indexed", ref.Key, ref.ConfigPath)
}

// classifyCreateProject maps a scaffolding failure onto the stable code catalog
// the hosts switch on.
func classifyCreateProject(err error) error {
	switch {
	case errors.Is(err, core.ErrProjectKey):
		return &Error{Code: "validation_failed", Message: err.Error()}
	case errors.Is(err, core.ErrProjectExists):
		return &Error{Code: "project_exists", Message: err.Error()}
	case errors.Is(err, core.ErrReadOnly):
		return &Error{Code: "read_only", Message: err.Error()}
	default:
		return fmt.Errorf("create project: %w", err)
	}
}

// appendFolder adds a documentation folder to a declared list, without
// duplicating one that is already there.
func appendFolder(folders []string, folder string) []string {
	for _, existing := range folders {
		if existing == folder {
			return folders
		}
	}
	return append(folders, folder)
}
