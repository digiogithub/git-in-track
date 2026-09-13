package server

import (
	"strings"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// The adapter between the saved `integrations.youtrack.field_map` and the
// translation table internal/youtrack/mapping works with, task GIT-T-0131.
//
// It lives here rather than in either of those packages on purpose: internal/config
// holds paths and credentials and must not learn the mapping rules, and
// internal/youtrack/mapping is a pure translation library that must not learn
// where a file is. The server is the one component that holds both.
//
// Two decisions worth knowing:
//
//   - A configured value map is **overlaid on** the shipped defaults rather
//     than replacing them. mapping.FieldMap's own rule is the opposite — a
//     caller that supplies a value map means exactly that map — which is right
//     for a caller assembling a table in code, and wrong for a person who used
//     a settings screen to say what one unusual state means and did not intend
//     to forget what "Fixed" means.
//   - Keys are folded to lower case here, before the overlay. mapping folds
//     them too, but folding after a merge would let "In Progress" and
//     "in progress" collide in map-iteration order, which is a non-deterministic
//     result nobody could debug.

// youtrackFieldMapping renders a project's configured field map as the
// translation table the importer and the issue preview read.
func youtrackFieldMapping(link *config.YouTrackLink) mapping.FieldMap {
	out := mapping.DefaultFieldMap()
	if link == nil || len(link.FieldMap) == 0 {
		return out
	}
	out = out.WithFieldNames(link.FieldMap.Names())
	out.Statuses = overlayValues(out.Statuses, link.FieldMap.ValuesFor("status"))
	out.Priorities = overlayValues(out.Priorities, link.FieldMap.ValuesFor("priority"))
	out.Types = overlayValues(out.Types, link.FieldMap.ValuesFor("type"))
	return out
}

// overlayValues merges one configured value map onto the defaults, folding the
// keys the way mapping looks them up.
func overlayValues[T ~string](defaults map[string]T, configured map[string]string) map[string]T {
	if len(configured) == 0 {
		return defaults
	}
	out := make(map[string]T, len(defaults)+len(configured))
	for from, to := range defaults {
		out[strings.ToLower(strings.TrimSpace(from))] = to
	}
	for from, to := range configured {
		key := strings.ToLower(strings.TrimSpace(from))
		if key == "" || strings.TrimSpace(to) == "" {
			continue
		}
		out[key] = T(strings.TrimSpace(to))
	}
	return out
}
