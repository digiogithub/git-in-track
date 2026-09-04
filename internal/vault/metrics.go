package vault

import (
	"context"
	"sort"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the metrics half of the sprint surface: "sprint.metrics"
// (docs/04 section 12, story GIT-US-0028).
//
// The computation itself is in internal/core and is the same everywhere. What
// differs between hosts is where the history comes from, and that is the only
// thing this file arranges: a companion process injects a HistorySource backed
// by git (internal/gitops), and a host that has none — the browser — falls back
// to the approximation core.ApproximateHistories draws from the `updated`
// stamps, which is reported as an approximation and never as a measurement
// (ADR-017).

// RepoRevision is one version of one file of one repository, as a host read it
// out of that repository's history.
type RepoRevision struct {
	// Path is repository-relative and forward-slashed.
	Path string
	At   time.Time
	Data []byte
	// Deleted reports a revision that removed the file.
	Deleted bool
}

// RepoHistory is what a host's history reader produced for one repository.
type RepoHistory struct {
	Revisions []RepoRevision
	// Commits counts the commits that were read.
	Commits int
	// Truncated reports a walk that stopped before the beginning of the
	// history, so that the days it cannot cover are reported as unknown.
	Truncated bool
	// Oldest is the instant of the earliest revision.
	Oldest time.Time
}

// HistorySource reads the past of a repository's files. The companion process
// implements it over internal/gitops; the browser leaves it nil, because a
// WebAssembly build has no git to walk and inventing a series would be worse
// than saying so.
type HistorySource interface {
	// FileHistory returns every revision of paths inside the repository mounted
	// as vaultID. An unknown repository is not an error: it is an empty
	// history.
	FileHistory(ctx context.Context, vaultID string, paths []string) (RepoHistory, error)
}

// SetHistorySource installs the reader the metrics reconstruct their series
// from. Passing nil removes it, which is what the browser leaves in place.
func (w *Workspace) SetHistorySource(source HistorySource) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.history = source
}

// historySource returns the installed reader, nil when there is none.
func (w *Workspace) historySource() HistorySource {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.history
}

// SprintMetrics answers "sprint.metrics": the burndown, the cumulative flow
// diagram and the flow statistics of one sprint, with the provenance of the
// history they were reconstructed from.
//
// It never fails because history is missing. A host that cannot read git gets
// the `updated` approximation, and a reference no history covers is reported as
// unknown on every day rather than counted as work that was never done.
func (w *Workspace) SprintMetrics(ctx context.Context, id string) (core.SprintMetricsView, error) {
	c, err := w.sprintContext(ctx)
	if err != nil {
		return core.SprintMetricsView{}, err
	}
	sprint, err := c.team.Vault.Sprint(ctx, id)
	if err != nil {
		return core.SprintMetricsView{}, err
	}
	view := c.view(ctx, sprint)
	history, provenance := w.reconstruct(ctx, c, view.Cards)
	out := core.BuildSprintMetrics(sprint, core.MetricsInput{
		Cards: view.Cards, History: history, Provenance: provenance, Now: c.now,
	})
	return out, nil
}

// reconstruct builds one history per reference and says where it came from.
func (w *Workspace) reconstruct(
	ctx context.Context, c sprintContext, cards []core.BoardCard,
) ([]core.ItemHistory, core.MetricsProvenance) {
	source := w.historySource()
	if source == nil {
		return core.ApproximateHistories(cards), core.MetricsProvenance{Source: core.MetricsSourceUpdated}
	}

	// The history of an item file can only be read where that file is: in the
	// clone the card was resolved from. A card read from a committed snapshot,
	// or not resolved at all, has no local history and stays unknown.
	byVault := map[string][]string{}
	refOf := map[string]map[string]string{}
	for _, card := range cards {
		if card.VaultID == "" || card.Path == "" || card.Source != core.CardSourceLive {
			continue
		}
		byVault[card.VaultID] = append(byVault[card.VaultID], card.Path)
		if refOf[card.VaultID] == nil {
			refOf[card.VaultID] = map[string]string{}
		}
		refOf[card.VaultID][card.Path] = card.Ref
	}
	vaults := make([]string, 0, len(byVault))
	for id := range byVault {
		vaults = append(vaults, id)
	}
	sort.Strings(vaults)

	provenance := core.MetricsProvenance{Source: core.MetricsSourceGit}
	var revisions []core.ItemRevision
	for _, id := range vaults {
		read, err := source.FileHistory(ctx, id, byVault[id])
		if err != nil {
			// A repository whose history cannot be read is not a failure of the
			// whole metric: its references stay unknown and the result says the
			// history is partial.
			provenance.Truncated = true
			continue
		}
		provenance.Commits += read.Commits
		provenance.Truncated = provenance.Truncated || read.Truncated
		if !read.Oldest.IsZero() &&
			(provenance.From.IsZero() || read.Oldest.Before(provenance.From.Time)) {
			provenance.From = core.NewDate(read.Oldest)
		}
		for _, rev := range read.Revisions {
			ref, ok := refOf[id][rev.Path]
			if !ok {
				continue
			}
			revisions = append(revisions, core.ItemRevision{
				Ref: ref, At: core.NewTimestamp(rev.At), Data: rev.Data, Deleted: rev.Deleted,
			})
		}
	}
	if len(revisions) == 0 {
		// The source read nothing — an unversioned folder, or a repository with
		// no commit yet. The approximation is then strictly better than an
		// empty chart, and it says what it is.
		return core.ApproximateHistories(cards), core.MetricsProvenance{Source: core.MetricsSourceUpdated}
	}
	return core.HistoriesFromRevisions(revisions, !provenance.Truncated, c.categoryOf), provenance
}

// categoryOf maps a status onto its coarse bucket in the workflow of the
// project the item belongs to. A project nobody cloned has no workflow here, so
// its statuses stay uncategorised and its days stay unknown.
func (c sprintContext) categoryOf(key core.ProjectKey, status core.Status) core.StatusCategory {
	if cfg, ok := c.configs[key]; ok && cfg != nil {
		return cfg.CategoryOf(status)
	}
	return ""
}
