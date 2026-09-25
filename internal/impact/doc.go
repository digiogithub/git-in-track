// Package impact resolves the requirement impact of a diff (ADR-037, docs/03
// section 21.11, GIT-US-0119): given base..head, or base..working tree, the
// requirements the change affects, each with the tier that reached it, the
// reasons, its coverage state and whether it is suspect. It is the core
// differentiator of Phase 11 and a native package: it needs git, the marker
// scan of internal/trace and, for its upper tiers, Pando.
//
// Three tiers, cheapest and most certain first:
//
//  1. Direct. gitops.ChangedFiles lists the changed files and line ranges;
//     the trace graph's reverse query (trace.Engine.TraceTouching) maps them
//     to the markers and trace: entries they touch. The Spec Delta and the
//     implements / modifies links of the story the diff is for are direct
//     hits too.
//  2. Transitive. The changed lines are mapped to changed symbols with the
//     scanner's own spans; Pando's code_impact_analysis over each of them
//     returns its callers, and a caller whose file and line fall inside a
//     traced symbol reaches that symbol's requirements.
//  3. Semantic. Pando's semantic search over requirement blocks, queried
//     with the changed symbol names and the story title. Its hits are
//     candidates with a score, never merged into the certainty of 1 and 2.
//
// Tiers 1 and 2 are deterministic: every list is sorted and no timestamp or
// map order reaches the answer. Without Pando, tiers 2 and 3 report
// `unavailable` and tier 1 still answers. The Resolver is what a native host
// installs into a vault as its impact backend (vault.RequirementImpact).
package impact
