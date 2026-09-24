package trace

import (
	"fmt"
	"sync"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Resolution is what a resolver knows about a marker ref.
type Resolution uint8

// The resolutions of a ref.
const (
	Resolved     Resolution = iota // the spec and its requirement block exist
	MissingSpec                    // no spec with that ID
	MissingBlock                   // the spec exists, the block does not
)

// Resolver answers whether a marker ref names an existing requirement. The
// project is the marker's "<KEY>/" qualifier, empty when none was written.
type Resolver interface {
	Resolve(project core.ProjectKey, ref core.RequirementRef) Resolution
}

// ResolverFunc adapts a function to a Resolver.
type ResolverFunc func(project core.ProjectKey, ref core.RequirementRef) Resolution

// Resolve calls f.
func (f ResolverFunc) Resolve(project core.ProjectKey, ref core.RequirementRef) Resolution {
	return f(project, ref)
}

// IndexResolver resolves refs against a built index: the spec is looked up by
// ID (the ID carries its project key) and its body parsed once for its
// requirement blocks. A deleted spec, or an item of another type, is a
// missing spec.
func IndexResolver(ix *core.Index) Resolver {
	var mu sync.Mutex
	bodies := map[core.ItemID]*core.SpecBody{}
	return ResolverFunc(func(_ core.ProjectKey, ref core.RequirementRef) Resolution {
		mu.Lock()
		defer mu.Unlock()
		body, ok := bodies[ref.Spec]
		if !ok {
			if it, err := ix.Item(ref.Spec); err == nil && it.Type == core.TypeSpec && !it.Deleted {
				b := core.ParseSpecBody(it.ID, it.Body)
				body = &b
			}
			bodies[ref.Spec] = body
		}
		if body == nil {
			return MissingSpec
		}
		if _, ok := body.Block(ref.Number); !ok {
			return MissingBlock
		}
		return Resolved
	})
}

// Dangling returns a W-MARKER-DANGLING finding for every marker whose ref the
// resolver does not know (R-MARK-4), sorted by path and line.
func Dangling(markers []Marker, r Resolver) []Finding {
	var out []Finding
	for _, m := range markers {
		var why string
		switch r.Resolve(m.Project, m.Ref) {
		case MissingSpec:
			why = fmt.Sprintf("no spec %s", m.Ref.Spec)
		case MissingBlock:
			why = fmt.Sprintf("spec %s has no requirement %s", m.Ref.Spec, m.Ref.Key())
		default:
			continue
		}
		out = append(out, Finding{
			Code: CodeMarkerDangling, Path: m.Path, Line: m.Line,
			Message: fmt.Sprintf("%s marker names %s: %s", m.Kind, m.Target(), why),
		})
	}
	return out
}

// Dangling reports the dangling markers of the cache; see the function
// Dangling.
func (c *Cache) Dangling(r Resolver) []Finding {
	return Dangling(c.Markers(), r)
}
