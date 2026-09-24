package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The token-budgeted impact report (docs/03 section 21.11, R-IMP-8 to
// R-IMP-10, GIT-US-0120). An agent asking "what does this diff affect?" wants
// the answer in a few hundred tokens, not the whole ImpactResult of a large
// change. The report ranks the hits so the ones that need attention come
// first, cuts the list at a token budget and hands back a cursor for the rest.
// It is pure: the MCP tool, the CLI and the HTTP API all render through it
// (the vault method "impact.report"), so the three cannot drift apart.

// ImpactReport is one page of the token-budgeted impact report.
type ImpactReport struct {
	Base    string `json:"base"`
	Head    string `json:"head,omitempty"`
	Files   int    `json:"files"`
	Symbols int    `json:"symbols"`
	// Tiers is the per-tier status of the JSON form; the text form carries it
	// in its second line instead.
	Tiers []ImpactTier `json:"tiers,omitempty"`
	// Hits is the page of the JSON form, ranked (RankImpactHits).
	Hits []ImpactHit `json:"hits,omitempty"`
	// Text is the text form of the page.
	Text string `json:"text,omitempty"`
	// Total counts every hit of the result; Offset is where this page starts.
	Total  int `json:"total"`
	Offset int `json:"offset,omitempty"`
	// Truncated counts the hits after this page; NextCursor fetches them.
	Truncated  int    `json:"truncated,omitempty"`
	NextCursor string `json:"nextCursor,omitempty"`
	// Budget is the budget the page was cut at; Tokens is the page's own
	// estimate, never above Budget unless a single hit alone exceeds it.
	Budget int `json:"budget"`
	Tokens int `json:"tokens"`
}

// impactPriority is the rank class of a hit: what needs attention first.
func impactPriority(h ImpactHit) int {
	switch {
	case h.Status == CoverageFailing:
		return 0
	case h.Suspect || h.Status == CoverageSuspect:
		return 1
	default:
		return 2
	}
}

// RankImpactHits returns the hits in report order: failing first, then
// suspect, then by tier (so tier-3 candidates come last), then — among
// candidates — by score, highest first, and finally by requirement ref (spec,
// then number). The order is total, so the same hits always rank the same way
// and a cursor offset means the same thing on every call. The input is not
// modified.
func RankImpactHits(hits []ImpactHit) []ImpactHit {
	out := append([]ImpactHit(nil), hits...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if pa, pb := impactPriority(a), impactPriority(b); pa != pb {
			return pa < pb
		}
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		if a.Candidate != b.Candidate {
			return !a.Candidate
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Ref.Spec != b.Ref.Spec {
			return a.Ref.Spec < b.Ref.Spec
		}
		return a.Ref.Number < b.Ref.Number
	})
	return out
}

// impactFingerprint hashes what a cursor offset depends on: the range and the
// ranked refs. A result whose hits changed under a walk fails it.
func impactFingerprint(res ImpactResult, ranked []ImpactHit) string {
	var b strings.Builder
	b.WriteString(res.Base + "\x00" + res.Head + "\x00")
	for _, hit := range ranked {
		b.WriteString(hit.Ref.String() + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:8]
}

func encodeImpactCursor(offset int, filter string) string { return encodeReportCursor(offset, filter) }

// RenderImpactReport renders one page of the report of an impact result: the
// ranked hits from the cursor on, as many as fit in the budget in the asked
// form, the per-tier status lines kept whatever the budget. A page always
// carries at least one hit when any remain, so a walk always advances; that
// hit alone may exceed a very small budget. The only error is a cursor that
// does not belong to this result (ErrInvalidCursor).
func RenderImpactReport(res ImpactResult, opt ImpactReportOptions) (ImpactReport, error) {
	budget, format, err := opt.resolve()
	if err != nil {
		return ImpactReport{}, err
	}
	ranked := RankImpactHits(res.Hits)
	filter := impactFingerprint(res, ranked)
	offset, err := decodeReportCursor(opt.Cursor, filter, len(ranked), "impact report")
	if err != nil {
		return ImpactReport{}, err
	}
	rest := ranked[offset:]

	page := func(n int) (ImpactReport, int) {
		r := ImpactReport{
			Base: res.Base, Head: res.Head, Files: res.Files, Symbols: res.Symbols,
			Total: len(ranked), Offset: offset, Budget: budget,
		}
		if left := len(rest) - n; left > 0 {
			r.Truncated = left
			r.NextCursor = encodeImpactCursor(offset+n, filter)
		}
		if format == ImpactReportText {
			// The envelope around the text counts too: the whole is measured.
			r.Text = impactText(r, res.Tiers, rest[:n])
		} else {
			r.Tiers = res.Tiers
			r.Hits = rest[:n]
		}
		cost := settleTokens(&r, &r.Tokens)
		return r, cost
	}
	return fitBudget(len(rest), budget, page), nil
}

// Clip widths of the text form.
const (
	impactTitleWidth   = 40
	impactReasonWidth  = 60
	impactMessageWidth = 48
)

// impactText renders the text form of a page:
//
//	impact <base>..<head|worktree>: <files> files, <symbols> symbols, <total> hits[, showing a-b]
//	tiers: 1 ok 7; 2 ok 3; 3 unavailable (<message>)
//	<ref> t<tier>[~score] <status|-> [suspect] "<title>" <reason>[ +n]
//	...
//	truncated: <n>, cursor: <token>
func impactText(r ImpactReport, tiers []ImpactTier, hits []ImpactHit) string {
	var b strings.Builder
	head := r.Head
	if head == "" {
		head = "worktree"
	}
	fmt.Fprintf(&b, "impact %s..%s: %d files, %d symbols, %d hits", r.Base, head, r.Files, r.Symbols, r.Total)
	if r.Offset > 0 || r.Truncated > 0 {
		fmt.Fprintf(&b, ", showing %d-%d", r.Offset+1, r.Offset+len(hits))
	}
	b.WriteByte('\n')
	if len(tiers) > 0 {
		parts := make([]string, 0, len(tiers))
		for _, t := range tiers {
			part := fmt.Sprintf("%d %s", t.Tier, t.Status)
			if t.Status == ImpactTierOK {
				part += " " + strconv.Itoa(t.Hits)
				if t.Truncated {
					part += " (partial)"
				}
			}
			if t.Message != "" && (t.Status == ImpactTierUnavailable || t.Status == ImpactTierError) {
				part += " (" + clipText(t.Message, impactMessageWidth) + ")"
			}
			parts = append(parts, part)
		}
		b.WriteString("tiers: " + strings.Join(parts, "; ") + "\n")
	}
	for _, h := range hits {
		b.WriteString(impactLine(h))
		b.WriteByte('\n')
	}
	if r.Truncated > 0 {
		fmt.Fprintf(&b, "truncated: %d, cursor: %s\n", r.Truncated, r.NextCursor)
	}
	return b.String()
}

// impactLine is one hit of the text form.
func impactLine(h ImpactHit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s t%d", h.Ref, h.Tier)
	if h.Candidate {
		b.WriteString("~" + strconv.FormatFloat(h.Score, 'f', -1, 64))
	}
	status := string(h.Status)
	if status == "" {
		status = "-"
	}
	b.WriteString(" " + status)
	if h.Suspect && h.Status != CoverageSuspect {
		b.WriteString(" suspect")
	}
	fmt.Fprintf(&b, " %q", clipText(h.Title, impactTitleWidth))
	if len(h.Reasons) > 0 {
		b.WriteString(" " + clipText(h.Reasons[0], impactReasonWidth))
		more := len(h.Reasons) - 1
		// A trailing "+<n>" entry (R-IMP-5) stands for n reasons already cut.
		if last := h.Reasons[len(h.Reasons)-1]; more > 0 && strings.HasPrefix(last, "+") {
			if n, err := strconv.Atoi(last[1:]); err == nil {
				more += n - 1
			}
		}
		if more > 0 {
			fmt.Fprintf(&b, " +%d", more)
		}
	}
	return b.String()
}
