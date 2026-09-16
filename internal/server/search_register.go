package server

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/digiogithub/git-in-track/internal/pando"
)

// Registering the repository with Pando as a code project (GIT-US-0098).
//
// This happens when the server starts, not in `gintrack agent init`: `agent
// init` writes configuration once, by one person, while a fresh clone becomes
// searchable by someone starting the server. So whoever clones the repository
// and runs `gintrack serve` gets an indexed project without running anything
// else, and nothing about it is allowed to hold the listener up — a Pando that
// is down or slow leaves the companion fully functional with no code search and
// a reason in the settings card.

// The states a repository's code index can be in, as the settings card reports
// them.
const (
	// codeIndexStatusOff means no Pando endpoint is configured: there is nothing to
	// register with.
	codeIndexStatusOff = "off"
	// codeIndexStatusRegistered means Pando already knows the project and was not
	// asked to index it again.
	codeIndexStatusRegistered = "registered"
	// codeIndexStatusIndexing means this start handed Pando an indexing job.
	codeIndexStatusIndexing = "indexing"
	// codeIndexStatusUnavailable means Pando refused or did not answer. The
	// companion keeps working; there is no code search.
	codeIndexStatusUnavailable = "unavailable"
)

// codeProject is one mounted repository as Pando's code index sees it.
type codeProject struct {
	// repo is the mount id, which is the repository id the project name is
	// derived from.
	repo string
	// root is the working tree, the path Pando is told to index.
	root string
	// id is Pando's project identifier, and the project name it is registered
	// under: Pando derives the one from the other (sanitizeProjectID,
	// internal/llm/tools/remembrances_code.go), so the name *is* the id.
	id string
}

// codeIndexView is what one repository's code registration looks like on the
// wire. It is the "stated reason" half of the non-blocking promise: whatever
// went wrong is a sentence in the settings card rather than a line in a log the
// user never sees.
type codeIndexView struct {
	// Project is the Pando project id the repository was registered under.
	Project string `json:"project"`
	// Status is one of the codeIndex* constants.
	Status string `json:"status"`
	// Job is the Pando indexing job this start began, when it began one.
	Job string `json:"job,omitempty"`
	// Note says in words what happened, including why there is no code search.
	Note string `json:"note,omitempty"`
}

// codeProjects lists the code projects of this workspace, one per ready mount.
//
// A configured `search.pando.projectId` names the project of the first ready
// mount — it is the "point this companion at that project" knob and there is
// one Pando per repository — and every other mount derives its own, so a
// workspace holding several clones never collapses them into one project.
func (s *searchState) codeProjects() []codeProject {
	s.mu.RLock()
	configured := strings.TrimSpace(s.settings.ProjectID)
	s.mu.RUnlock()

	var out []codeProject
	for _, m := range s.repos.ready() {
		id := codeProjectID(m)
		if len(out) == 0 && configured != "" {
			id = pando.SanitizeProjectID(configured)
		}
		if id == "" {
			continue
		}
		out = append(out, codeProject{repo: m.id, root: m.path, id: id})
	}
	return out
}

// codeProjectID is the Pando project one mounted repository is registered
// under.
//
// It is derived from the repository's absolute path with Pando's own rule, not
// from the mount id alone: a mount id is unique within one workspace, while
// Pando's project table is shared by every workspace on the machine, so two
// repositories both mounted as "docs" would otherwise become one project and
// answer each other's searches. The path is what identifies a repository to
// Pando, and GIT-T-0142 already pinned this derivation for the search side.
func codeProjectID(m *mount) string {
	return pando.SanitizeProjectID(m.path)
}

// startRegistration registers every mounted repository in the background. It
// returns at once: the caller is `Start`, between the listener and the first
// request, and a Pando that never answers must cost nothing but the code half
// of the search.
//
// It runs once per process. A settings change rebuilds the client, and the next
// start — or an explicit reindex — is what re-registers; re-registering on
// every PATCH would turn a typo in a URL into a burst of indexing jobs.
func (s *searchState) startRegistration(ctx context.Context) {
	if s == nil || !s.registering.CompareAndSwap(false, true) {
		return
	}
	go s.registerCodeProjects(ctx)
}

// registerCodeProjects is the registration itself, and is what a test drives
// directly.
//
// Idempotence is explicit rather than inherited: Pando is asked what it already
// has, and a project whose id and root already match is left alone, so a second
// start neither duplicates the project nor starts a fresh index of it. (Pando's
// own indexer skips a file whose content hash has not changed, so even the
// reindex a caller asks for on purpose is incremental — but the promise here is
// that nothing is asked for at all.)
func (s *searchState) registerCodeProjects(ctx context.Context) {
	projects := s.codeProjects()
	client := s.pando()
	if client == nil {
		for _, p := range projects {
			s.noteCodeIndex(p.repo, codeIndexView{
				Project: p.id, Status: codeIndexStatusOff,
				Note: "No Pando endpoint is configured, so the repository is not indexed for code search.",
			})
		}
		return
	}

	known := make(map[string]pando.Project)
	if existing, err := client.ListProjects(ctx); err != nil {
		// Not fatal: IndexProject below is the authority, and it will report
		// the same outage in the row itself if Pando is really down.
		s.log.Debug("could not list the Pando code projects", "error", err)
	} else {
		for _, p := range existing {
			known[p.ProjectID] = p
		}
	}

	for _, p := range projects {
		if prev, ok := known[p.id]; ok && sameRoot(prev.RootPath, p.root) {
			s.noteCodeIndex(p.repo, codeIndexView{
				Project: p.id, Status: codeIndexStatusRegistered,
				Note: "Already registered with Pando; it was not reindexed. " +
					"Use Reindex to ask for a fresh pass.",
			})
			continue
		}
		job, err := client.IndexProject(ctx, p.root, p.id)
		if err != nil {
			s.log.Warn("the repository could not be registered with Pando",
				"repo", p.repo, "project", p.id, "error", err)
			s.noteCodeIndex(p.repo, codeIndexView{
				Project: p.id, Status: codeIndexStatusUnavailable,
				Note: "Pando did not accept the code project, so there is no code search: " + err.Error(),
			})
			continue
		}
		s.log.Info("registered the repository with Pando as a code project",
			"repo", p.repo, "project", p.id, "root", p.root, "job", job)
		s.noteCodeIndex(p.repo, codeIndexView{
			Project: p.id, Status: codeIndexStatusIndexing, Job: job,
			Note: "Indexing the repository root; code search answers as the index fills.",
		})
	}
}

// sameRoot reports whether Pando's record of a project points at the tree this
// companion would register. A project id that has been pointed at another tree
// is re-registered rather than silently searched.
func sameRoot(known, want string) bool {
	if known == "" {
		return false
	}
	return filepath.Clean(known) == filepath.Clean(want)
}

// noteCodeIndex records what became of one repository's registration.
func (s *searchState) noteCodeIndex(repo string, view codeIndexView) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codeIndex == nil {
		s.codeIndex = make(map[string]codeIndexView)
	}
	s.codeIndex[repo] = view
}

// codeIndexOf returns the registration of one repository, if there is one yet.
func (s *searchState) codeIndexOf(repo string) *codeIndexView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	view, ok := s.codeIndex[repo]
	if !ok {
		return nil
	}
	return &view
}
