package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// InboxEnabledCode is the problem code of "project.inbox.enable" on a project
// whose workflow already declares a triage status: it has an inbox.
const InboxEnabledCode = "inbox_already_enabled"

// TriageIDTakenCode is the problem code of "project.inbox.enable" on a project
// that already has an ordinary status called `triage`. The file is left alone:
// renaming that status is a decision for a person, not for this button.
const TriageIDTakenCode = "triage_status_id_taken"

// ProjectInboxEnableParams is the input of "project.inbox.enable".
type ProjectInboxEnableParams struct {
	// VaultID routes the call; it is read by the workspace router, not here.
	VaultID string `json:"vaultId,omitempty"`
	// Project is the key of the project whose project.yaml is edited.
	Project string `json:"project"`
	// Rev is the revision of project.yaml the caller read, as project.list
	// reports it in configRev. Empty means unconditional.
	Rev string `json:"rev,omitempty"`
}

// ProjectInboxEnabled is what "project.inbox.enable" answers with: the project
// as "project.list" now reports it, and the files the host must persist.
type ProjectInboxEnabled struct {
	Project projectSummary `json:"project"`
	Writes  WriteSet       `json:"writes"`
}

// projectInboxEnable adds the triage status to one project's workflow (story
// GIT-US-0100, ADR-033), so that a project created before the inbox existed
// gets one without anybody editing project.yaml by hand.
//
// The edit itself lives in internal/core; this method checks the project is
// writable and the revision is current, writes the file through the tracked
// file system so the host persists and commits it like any other write, and
// rediscovers the projects so the new workflow is what every later call sees.
// The caller holds the vault lock.
func (v *Vault) projectInboxEnable(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[ProjectInboxEnableParams](raw)
	if err != nil {
		return nil, err
	}
	key := core.ProjectKey(strings.TrimSpace(p.Project))
	store, err := v.storeFor(key)
	if err != nil {
		return nil, err
	}
	var ref core.ProjectRef
	for _, candidate := range v.projects {
		if !candidate.Team && v.stores[candidate.Key] == store {
			ref = candidate
			break
		}
	}
	if ref.ConfigPath == "" {
		return nil, failf("not_found", "project %q is not open for writing", key)
	}

	data, err := v.fs.ReadFile(ref.ConfigPath)
	if err != nil {
		return nil, &Error{Code: "not_found", Message: err.Error(), Path: ref.ConfigPath}
	}
	if current := core.ComputeRev(data); p.Rev != "" && core.Rev(p.Rev) != current {
		return nil, &Error{
			Code:    core.StaleRevisionCode,
			Message: fmt.Sprintf("%s changed since it was read", ref.ConfigPath),
			Path:    ref.ConfigPath,
			Current: string(current),
		}
	}
	updated, err := core.EnableInbox(data)
	if err != nil {
		return nil, classifyEnableInbox(err, ref)
	}

	v.fs.begin()
	if err := v.fs.WriteFile(ref.ConfigPath, updated); err != nil {
		if errors.Is(err, core.ErrReadOnly) {
			return nil, &Error{Code: "read_only", Message: err.Error(), Path: ref.ConfigPath}
		}
		return nil, fmt.Errorf("enable inbox: %w", err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := v.reload(ctx); err != nil {
		return nil, err
	}

	for _, summary := range v.projectList() {
		if summary.Key == string(ref.Key) {
			return ProjectInboxEnabled{Project: summary, Writes: writes}, nil
		}
	}
	return nil, failf("internal", "project %s was written to %s but is not indexed", ref.Key, ref.ConfigPath)
}

// classifyEnableInbox maps a refusal onto the stable code catalog the hosts
// switch on.
func classifyEnableInbox(err error, ref core.ProjectRef) error {
	switch {
	case errors.Is(err, core.ErrInboxEnabled):
		return &Error{Code: InboxEnabledCode, Path: ref.ConfigPath,
			Message: fmt.Sprintf("project %s already has an inbox", ref.Key)}
	case errors.Is(err, core.ErrTriageIDTaken):
		return &Error{Code: TriageIDTakenCode, Path: ref.ConfigPath,
			Message: fmt.Sprintf("project %s already has a status called triage that is not a triage status; "+
				"rename it in %s before enabling the inbox", ref.Key, ref.ConfigPath)}
	default:
		return &Error{Code: "validation_failed", Message: err.Error(), Path: ref.ConfigPath}
	}
}
