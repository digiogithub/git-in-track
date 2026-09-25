package core

import "errors"

// The answer types of the requirement impact query (ADR-037, docs/03 section
// 21.11, GIT-US-0119): given a diff, the requirements it affects, each with
// the tier that reached it, the reasons, its coverage state and whether it is
// suspect. The query is resolved by a native host — it needs git, the marker
// scan and, for tiers 2 and 3, Pando — so only the shapes a host hands back
// through the vault seam live here, free of any filesystem access. Nothing
// here is ever written into a file: an impact report is derived data.

// ErrUnknownRevision is a base or head an impact query names that the
// repository's history does not hold.
var ErrUnknownRevision = errors.New("unknown revision")

// The three impact tiers, cheapest and most certain first.
const (
	// ImpactTierDirect: a changed file or symbol carries a marker or a
	// trace: entry of the requirement, or the story the diff is for names it
	// in its Spec Delta or its links.
	ImpactTierDirect = 1
	// ImpactTierTransitive: Pando's call graph reaches, from a changed
	// symbol, a caller that carries a marker of the requirement.
	ImpactTierTransitive = 2
	// ImpactTierSemantic: Pando's semantic search over requirement blocks
	// ranks the requirement near the changed symbol names and the story
	// title. A candidate, never a certainty.
	ImpactTierSemantic = 3
)

// ImpactTierStatus says how one tier of an impact query was answered.
type ImpactTierStatus string

// The tier states.
const (
	// ImpactTierOK: the tier ran; its hits are in the result.
	ImpactTierOK ImpactTierStatus = "ok"
	// ImpactTierUnavailable: the tier needs a backend this session has not
	// got, or that did not answer (no Pando, Pando unreachable or timing out).
	ImpactTierUnavailable ImpactTierStatus = "unavailable"
	// ImpactTierError: the backend answered with an error of its own, for
	// example a code project Pando has not indexed.
	ImpactTierError ImpactTierStatus = "error"
	// ImpactTierSkipped: the caller did not ask for the tier.
	ImpactTierSkipped ImpactTierStatus = "skipped"
)

// ImpactQuery is what an impact query resolves.
type ImpactQuery struct {
	// Base is the revision the diff starts from: a branch, a SHA, HEAD.
	Base string `json:"base"`
	// Head is the revision the diff ends at; empty means the working tree.
	Head string `json:"head,omitempty"`
	// Story is the story or task the diff is for. Its Spec Delta and its
	// implements / modifies links are direct hits, and its title feeds the
	// semantic tier.
	Story ItemID `json:"story,omitempty"`
	// Title is extra text for the semantic tier, next to the story title.
	Title string `json:"title,omitempty"`
	// Tiers limits the query to these tiers; empty runs all three.
	Tiers []int `json:"tiers,omitempty"`
	// Depth is the transitive caller depth of tier 2; 0 is the host default.
	Depth int `json:"depth,omitempty"`
	// Limit caps the callers per changed symbol of tier 2 and the candidates
	// of tier 3; 0 is the host default.
	Limit int `json:"limit,omitempty"`
}

// Wants reports whether the query asks for a tier.
func (q ImpactQuery) Wants(tier int) bool {
	if len(q.Tiers) == 0 {
		return true
	}
	for _, t := range q.Tiers {
		if t == tier {
			return true
		}
	}
	return false
}

// ImpactTier is how one tier was answered.
type ImpactTier struct {
	Tier   int              `json:"tier"`
	Status ImpactTierStatus `json:"status"`
	// Hits counts the result's hits whose strongest tier is this one.
	Hits int `json:"hits"`
	// Truncated reports that the tier's input or its backend's answer was cut
	// at a bound, so the tier may reach more than it reports.
	Truncated bool `json:"truncated,omitempty"`
	// Message says why a tier is unavailable or failed.
	Message string `json:"message,omitempty"`
}

// ImpactKind says what a certain (tier 1 or 2) hit's diff changed: the code
// behind the requirement, or only a test that verifies it (GIT-US-0157).
type ImpactKind string

// The hit kinds.
const (
	// ImpactKindBehaviour: at least one reason reaches the requirement
	// through its code — an Implements: marker or a trace.code entry, directly
	// or through a call — or the story names it in its links or Spec Delta.
	ImpactKindBehaviour ImpactKind = "behaviour"
	// ImpactKindTestOnly: every reason reaches the requirement through a test
	// that verifies it — a Verifies: marker or a trace.tests entry. The tests
	// changed; what the requirement states may not have.
	ImpactKindTestOnly ImpactKind = "test-only"
)

// ImpactHit is one requirement a diff affects. A requirement reached by
// several tiers is one hit, at its strongest (lowest) tier, listing the
// reasons of every tier that reached it with certainty.
type ImpactHit struct {
	Ref   RequirementRef `json:"ref"`
	Title string         `json:"title"`
	Tier  int            `json:"tier"`
	// Kind is behaviour or test-only on a tier-1 or tier-2 hit; empty on a
	// tier-3 candidate, which is not a trace.
	Kind ImpactKind `json:"kind,omitempty"`
	// Candidate marks a tier-3 hit: a semantic neighbor, not a trace.
	Candidate bool `json:"candidate,omitempty"`
	// Score is the semantic score of a candidate, rounded to three decimals.
	Score float64 `json:"score,omitempty"`
	// Status is the requirement's coverage state (docs/03 section 21.6);
	// empty when no coverage backend answered.
	Status CoverageStatus `json:"status,omitempty"`
	// Suspect is set when the coverage state is suspect, or when passing
	// evidence covers code the diff changes directly or transitively.
	Suspect bool `json:"suspect,omitempty"`
	// Reasons are short codes, "<kind>:<detail>" (docs/03 section 21.11),
	// sorted by tier and then by text, at most a few then "+<n>".
	Reasons []string `json:"reasons"`
	// Pending lists the open stories and tasks whose unapplied Spec Delta
	// modifies the requirement (R-DELTA-10).
	Pending []ItemID `json:"pending,omitempty"`
}

// ImpactResult is the answer of an impact query.
type ImpactResult struct {
	Base string `json:"base"`
	Head string `json:"head,omitempty"`
	// Files counts the changed files; Symbols the changed symbols they map to.
	Files   int          `json:"files"`
	Symbols int          `json:"symbols"`
	Tiers   []ImpactTier `json:"tiers"`
	Hits    []ImpactHit  `json:"hits"`
}
