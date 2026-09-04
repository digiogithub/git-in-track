package server

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Repo is one repository the server mounts: a directory on disk holding one or
// more project backlogs. The companion never copies a repository; it indexes it
// where the user keeps it.
type Repo struct {
	// ID is the stable handle the API addresses the repository by. It defaults
	// to the base name of Path.
	ID string
	// Path is the absolute path of the working tree.
	Path string
	// Role is "project" or "team"; it is reported, not enforced.
	Role string
	// DocsFolder is the documentation folder relative to the repository root.
	// It is reported so that the UI can show where the backlog lives; project
	// discovery walks the tree itself.
	DocsFolder string
}

// roleProject is the default role of a mounted repository.
const roleProject = "project"

// mount is a repository the server serves, together with the vault built over
// it. A mount whose vault could not be opened keeps its error and answers every
// request about it with a problem, so that one broken repository never takes
// the whole companion down.
type mount struct {
	id    string
	path  string
	role  string
	docs  string
	label string
	vlt   *vault.Vault
	err   error

	// mu guards lastIndexed, which every reindex and every watcher pass writes.
	mu          sync.Mutex
	lastIndexed time.Time
}

// openMount builds the vault of one repository.
func openMount(repo Repo, now func() time.Time) *mount {
	id := repo.ID
	if id == "" {
		id = filepath.Base(filepath.Clean(repo.Path))
	}
	role := repo.Role
	if role == "" {
		role = roleProject
	}
	m := &mount{
		id:          id,
		path:        repo.Path,
		role:        role,
		docs:        repo.DocsFolder,
		label:       filepath.Base(filepath.Clean(repo.Path)),
		lastIndexed: now(),
	}
	fsys, err := osfs.New(repo.Path)
	if err != nil {
		m.err = fmt.Errorf("mount %s: %w", repo.Path, err)
		return m
	}
	v, err := vault.Open(fsys, m.label)
	if err != nil {
		m.err = fmt.Errorf("index %s: %w", repo.Path, err)
		return m
	}
	m.vlt = v
	return m
}

// ready reports whether the mount has a usable vault.
func (m *mount) ready() bool { return m.vlt != nil }

// projectKeys returns the project keys this mount exposes.
func (m *mount) projectKeys() []string {
	if !m.ready() {
		return nil
	}
	refs := m.vlt.Projects()
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, string(ref.Key))
	}
	sort.Strings(out)
	return out
}

// teamKey reports the key of the team repository this mount is, empty when its
// root holds no team.yaml.
func (m *mount) teamKey() string {
	if !m.ready() {
		return ""
	}
	if team := m.vlt.Team(); team != nil {
		return string(team.Key)
	}
	return ""
}

// touch records that the index was rebuilt or updated just now.
func (m *mount) touch(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastIndexed = at
}

// indexedAt reports when the index was last refreshed.
func (m *mount) indexedAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastIndexed
}

// info renders the repository the way GET /api/v1/repos documents it.
func (m *mount) info() map[string]any {
	out := map[string]any{
		"key":      m.id,
		"id":       m.id,
		"role":     m.role,
		"team":     m.teamKey(),
		"name":     m.label,
		"path":     m.path,
		"docs":     m.docs,
		"projects": m.projectKeys(),
	}
	if m.err != nil {
		out["error"] = m.err.Error()
		return out
	}
	stats := m.vlt.Stats()
	out["counts"] = map[string]int{
		"items":    stats.Items,
		"pages":    stats.Pages,
		"comments": stats.Comments,
	}
	out["lastIndexed"] = m.indexedAt().UTC().Format(time.RFC3339)
	return out
}

// registry holds every mounted repository. It is built once by New and is
// immutable afterwards, which is what makes it safe to read without a lock:
// registering a repository at runtime is a configuration change (`gintrack
// add`), not an API call.
type registry struct {
	mounts []*mount
	byID   map[string]*mount
	// space is the shared multi-repository view of the mounts: the same
	// vault.Workspace the browser worker drives, so that cross-repository
	// reference resolution, unified search and the team surface have exactly
	// one implementation (docs/04 section 1).
	space *vault.Workspace
}

// newRegistry mounts every configured repository. It never fails: a repository
// that cannot be opened is kept as a broken mount and reported as such.
func newRegistry(repos []Repo, now func() time.Time) *registry {
	reg := &registry{byID: make(map[string]*mount, len(repos)), space: vault.NewWorkspace()}
	for _, repo := range repos {
		m := openMount(repo, now)
		if _, clash := reg.byID[m.id]; clash {
			// Two repositories with the same base name: keep both reachable by
			// disambiguating the second one, exactly as the configuration does.
			m.id = fmt.Sprintf("%s-%d", m.id, len(reg.mounts)+1)
		}
		reg.byID[m.id] = m
		reg.mounts = append(reg.mounts, m)
		if m.ready() {
			if _, err := reg.space.Attach(m.id, m.role, m.vlt); err != nil {
				m.err = fmt.Errorf("attach %s: %w", m.id, err)
			}
		}
	}
	return reg
}

// workspace returns the multi-repository view of the mounted repositories.
func (reg *registry) workspace() *vault.Workspace { return reg.space }

// all returns every mount in registration order.
func (reg *registry) all() []*mount { return reg.mounts }

// ready returns the mounts that have a usable vault.
func (reg *registry) ready() []*mount {
	out := make([]*mount, 0, len(reg.mounts))
	for _, m := range reg.mounts {
		if m.ready() {
			out = append(out, m)
		}
	}
	return out
}

// lookup returns the mount registered under id.
func (reg *registry) lookup(id string) (*mount, bool) {
	m, ok := reg.byID[id]
	return m, ok
}

// forProject returns the mount exposing a project key.
func (reg *registry) forProject(key string) (*mount, bool) {
	if key == "" {
		return nil, false
	}
	for _, m := range reg.ready() {
		for _, k := range m.projectKeys() {
			if k == key {
				return m, true
			}
		}
	}
	return nil, false
}

// forItem returns the mount owning an item id. The project key is the id's own
// prefix, which is what makes a single-repository request routable without a
// query parameter; a vault that answers for the id anyway (an id the grammar
// does not cover) is the fallback.
func (reg *registry) forItem(id string) (*mount, bool) {
	if key, _, _, err := core.ParseItemID(id); err == nil {
		if m, ok := reg.forProject(string(key)); ok {
			return m, true
		}
	}
	ready := reg.ready()
	if len(ready) == 1 {
		return ready[0], true
	}
	return nil, false
}

// reindex rebuilds the whole index of a mount.
func (m *mount) reindex(ctx context.Context, now func() time.Time) (vault.IndexStats, error) {
	if !m.ready() {
		return vault.IndexStats{}, m.err
	}
	stats, err := m.vlt.Reload(ctx)
	if err != nil {
		return vault.IndexStats{}, fmt.Errorf("reindex %s: %w", m.id, err)
	}
	m.touch(now())
	return stats, nil
}
