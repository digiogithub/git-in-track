package core

import (
	"sort"
	"strings"
)

// This file holds the set algebra and the validation of the `external`
// front-matter field (ADR-031). The type itself lives in model.go, beside Link,
// because it is part of the item shape; everything that reasons about a list of
// them lives here.

// maxExternalIDBytes bounds an external identifier. It is generous: YouTrack
// issue ids are short, but a system addressed by URL fragment may not be.
const maxExternalIDBytes = 200

// NormalizeExternal returns a copy of e with its scalar fields trimmed and its
// system lower-cased, which is the form written to disk and compared on.
func NormalizeExternal(e External) External {
	e.System = strings.ToLower(strings.TrimSpace(e.System))
	e.ID = strings.TrimSpace(e.ID)
	e.URL = strings.TrimSpace(e.URL)
	e.Key = strings.TrimSpace(e.Key)
	return e
}

// addExternals merges entries into a list with set semantics keyed on
// (system, id): a pair already present is updated in place instead of appended,
// so two clients pushing different systems never clobber each other and pushing
// the same system twice never duplicates (docs/07 section 5.5).
func addExternals(list, add []External) []External {
	for _, raw := range add {
		e := NormalizeExternal(raw)
		if !e.Valid() {
			continue
		}
		replaced := false
		for i := range list {
			if list[i].Ref() != e.Ref() {
				continue
			}
			// Keep the fields the caller did not supply: an importer that only
			// refreshes synced_at must not drop a url somebody else recorded.
			merged := NormalizeExternal(list[i])
			merged.System = e.System
			merged.ID = e.ID
			if e.URL != "" {
				merged.URL = e.URL
			}
			if e.Key != "" {
				merged.Key = e.Key
			}
			if !e.SyncedAt.IsZero() {
				merged.SyncedAt = e.SyncedAt
			}
			list[i] = merged
			replaced = true
			break
		}
		if !replaced {
			list = append(list, e)
		}
	}
	return list
}

// removeExternals drops every entry whose (system, id) pair is named by remove.
// A remove entry with an empty id removes every entry of that system, which is
// how a surface unlinks an item from a tracker wholesale.
func removeExternals(list, remove []External) []External {
	if len(list) == 0 || len(remove) == 0 {
		return list
	}
	refs := make(map[ExternalRef]bool, len(remove))
	systems := make(map[string]bool)
	for _, raw := range remove {
		e := NormalizeExternal(raw)
		switch {
		case e.System == "":
			continue
		case e.ID == "":
			systems[e.System] = true
		default:
			refs[e.Ref()] = true
		}
	}
	out := list[:0]
	for _, e := range list {
		ref := e.Ref()
		if refs[ref] || systems[ref.System] {
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// dedupeExternals keeps the first entry of every (system, id) pair, in order.
// It is what a parsed file goes through, so a hand-edited duplicate does not
// reach the index.
func dedupeExternals(list []External) []External {
	if len(list) < 2 {
		return list
	}
	seen := make(map[ExternalRef]bool, len(list))
	out := make([]External, 0, len(list))
	for _, e := range list {
		ref := e.Ref()
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, e)
	}
	return out
}

// cloneExternals returns a deep-enough copy: External holds only scalars.
func cloneExternals(list []External) []External {
	if list == nil {
		return nil
	}
	return append([]External(nil), list...)
}

// ExternalSystems returns the distinct systems a list references, sorted.
func ExternalSystems(list []External) []string {
	if len(list) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, e := range list {
		s := e.Ref().System
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// FindExternal returns the entry of a list that belongs to a system, or false.
func FindExternal(list []External, system string) (External, bool) {
	want := NewExternalRef(system, "x").System
	for _, e := range list {
		if e.Ref().System == want {
			return e, true
		}
	}
	return External{}, false
}

// externalsFromAny decodes the `external:` front-matter value of a document whose
// front matter is a generic mapping — a knowledge-base page (docs/03 section 14),
// whose block is free-form and never validated. Malformed entries are dropped
// rather than reported: a broken block must not hide a page from the tree.
func externalsFromAny(v any) []External {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]External, 0, len(list))
	for _, raw := range list {
		m, isMap := raw.(map[string]any)
		if !isMap {
			continue
		}
		e := NormalizeExternal(External{
			System: stringOf(m["system"]),
			ID:     stringOf(m["id"]),
			URL:    stringOf(m["url"]),
			Key:    stringOf(m["key"]),
		})
		if ts, err := ParseTimestamp(stringOf(m["synced_at"])); err == nil {
			e.SyncedAt = ts
		}
		if !e.Valid() {
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil
	}
	return dedupeExternals(out)
}

// validateExternal applies the E-EXT-* rules to a list of references. It is
// shared by items and comments, which is why it takes the list and not the item.
//
// The rules are deliberately shallow: a system this version does not know is
// accepted and preserved, because the whole point of the field is that other
// tools write into it (R-EVO-5).
func validateExternal(d *diagSet, field string, list []External) {
	seen := make(map[ExternalRef]bool, len(list))
	for _, raw := range list {
		e := NormalizeExternal(raw)
		switch {
		case e.System == "" && e.ID == "":
			d.errorf(field, CodeExternalFields, "an external reference needs both system and id")
			continue
		case e.System == "":
			d.errorf(field, CodeExternalFields, "external reference %q has no system", e.ID)
			continue
		case e.ID == "":
			d.errorf(field, CodeExternalFields, "external reference to %q has no id", e.System)
			continue
		}
		if !validExternalSystem(e.System) {
			d.errorf(field, CodeExternalFields,
				"external system %q does not match [a-z0-9][a-z0-9_.-]{0,31}", e.System)
			continue
		}
		if len(e.ID) > maxExternalIDBytes {
			d.errorf(field, CodeExternalFields,
				"external id of %q is longer than %d characters", e.System, maxExternalIDBytes)
			continue
		}
		if e.URL != "" && !isHTTPURL(e.URL) {
			d.warnf(field, CodeWarnExternalURL,
				"external reference %s has a url that is not http(s): %q", e, e.URL)
		}
		ref := e.Ref()
		if seen[ref] {
			d.warnf(field, CodeWarnExternalDup, "external reference %s is listed more than once", e)
			continue
		}
		seen[ref] = true
	}
}

// isHTTPURL reports whether a url is an absolute http(s) address, which is the
// only shape a tracker link is useful in: it has to be openable in a browser.
func isHTTPURL(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// validExternalSystem reports whether a system identifier is a short lowercase
// token. The grammar exists so that a system name can be a path segment, a query
// parameter and a map key without escaping; it constrains the shape, never the
// set of values.
func validExternalSystem(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '-' || r == '_' || r == '.'):
		default:
			return false
		}
	}
	return true
}

// normalizeExternals returns a copy of a list with every entry normalised and
// the invalid ones — those missing a system or an id — dropped. It is what a
// caller-supplied list goes through before it becomes item state.
func normalizeExternals(list []External) []External {
	if len(list) == 0 {
		return nil
	}
	out := make([]External, 0, len(list))
	for _, raw := range list {
		e := NormalizeExternal(raw)
		if !e.Valid() {
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
