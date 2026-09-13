package server

import (
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
//   - The translation itself — which keys carry value maps, how a configured
//     map is overlaid on the shipped defaults, how keys are folded — belongs to
//     mapping.FieldMap.WithFields. This file only reshapes one type into the
//     other, so that the preview here and the importer in internal/vault read
//     one set of rules rather than two that can drift.
//   - The reshaped block, not a flat field-name map, is what reaches
//     internal/vault: a flat map carries the field names and silently loses
//     every value the project configured (GIT-T-0131).

// youtrackFieldSpecs reshapes a project's configured field map into the shape
// internal/youtrack/mapping and internal/vault take. Both halves of each entry
// cross over: the YouTrack field name and the value map.
func youtrackFieldSpecs(link *config.YouTrackLink) map[string]mapping.FieldSpec {
	if link == nil || len(link.FieldMap) == 0 {
		return nil
	}
	out := make(map[string]mapping.FieldSpec, len(link.FieldMap))
	for key, entry := range link.FieldMap {
		out[key] = mapping.FieldSpec{Field: entry.Field, Values: link.FieldMap.ValuesFor(key)}
	}
	return out
}

// youtrackFieldMapping renders a project's configured field map as the
// translation table the issue preview reads. The importer builds the same table
// from the same specs, one layer down.
func youtrackFieldMapping(link *config.YouTrackLink) mapping.FieldMap {
	return mapping.DefaultFieldMap().WithFields(youtrackFieldSpecs(link))
}
