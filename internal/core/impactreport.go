package core

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The token-budgeted impact report (docs/03 section 21.11, R-IMP-8 to
// R-IMP-10, GIT-US-0120). An agent asking "what does this diff affect?" wants
// the answer in a few hundred tokens, not the whole ImpactResult of a large
// change. The report ranks the hits so the ones that need attention come
// first, cuts the list at a token budget and hands back a cursor for the rest.
// It is pure: the MCP tool, the CLI and the HTTP API all render through it
// (the vault method "impact.report"), so the three cannot drift apart.

// DefaultImpactBudget is the token budget of a report that names none: the
// milestone's success criterion for the impact report of a typical PR.
const DefaultImpactBudget = 1500

// MaxImpactBudget bounds a budget a caller may ask for. A caller that wants
// everything walks the cursor instead.
const MaxImpactBudget = 20000

// ErrInvalidCursor reports a report cursor that this package did not issue,
// or that was issued for a different impact result: the diff, the base or the
// ranking changed under the walk, so resuming would skip or repeat hits.
var ErrInvalidCursor = errors.New("invalid cursor")

// ImpactReportFormat is the form a report is rendered and budgeted in.
type ImpactReportFormat string

// The two forms.
const (
	// ImpactReportJSON: the report's hits as structured data.
	ImpactReportJSON ImpactReportFormat = "json"
	// ImpactReportText: one terse line per requirement.
	ImpactReportText ImpactReportFormat = "text"
)

// EstimateTokens is the tokenizer approximation every impact budget is
// measured with: one token per three bytes of UTF-8, rounded up. Real BPE
// tokenizers average about four bytes per token over English prose and three
// to three and a half over JSON, identifiers and paths, which is what a report
// is made of, so the rule errs on the side of overestimating: a report within
// its estimated budget is within it for the model too.
func EstimateTokens(s string) int { return (len(s) + 2) / 3 }

// ImpactReportOptions shapes one page of a report.
type ImpactReportOptions struct {
	// Budget is the token budget of the page (EstimateTokens over the
	// rendered form); 0 is DefaultImpactBudget.
	Budget int
	// Cursor resumes a walk where the previous page stopped; empty starts it.
	Cursor string
	// Format is the form the page is rendered and measured in; empty is JSON.
	Format ImpactReportFormat
}

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

// impactCursor is the decoded report cursor: the same {offset, filter
// fingerprint} shape the MCP server's own cursors carry (docs/08 section 3,
// principle 4). Its fingerprint binds it to the ranked result, not to the
// budget or the form, so a walk may change either between pages.
type impactCursor struct {
	Offset int    `json:"o"`
	Filter string `json:"f"`
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

func encodeImpactCursor(offset int, filter string) string {
	raw, err := json.Marshal(impactCursor{Offset: offset, Filter: filter})
	if err != nil { // unreachable: an int and a string
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeImpactCursor(token, filter string, total int) (int, error) {
	if token == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not an impact report cursor", ErrInvalidCursor, token)
	}
	var c impactCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return 0, fmt.Errorf("%w: %q is not an impact report cursor", ErrInvalidCursor, token)
	}
	if c.Filter != filter {
		return 0, fmt.Errorf("%w: the cursor was issued for a different impact result; "+
			"restart without a cursor after the diff or the query changed", ErrInvalidCursor)
	}
	if c.Offset < 0 || c.Offset > total {
		return 0, fmt.Errorf("%w: the cursor's offset %d is out of range", ErrInvalidCursor, c.Offset)
	}
	return c.Offset, nil
}

// RenderImpactReport renders one page of the report of an impact result: the
// ranked hits from the cursor on, as many as fit in the budget in the asked
// form, the per-tier status lines kept whatever the budget. A page always
// carries at least one hit when any remain, so a walk always advances; that
// hit alone may exceed a very small budget. The only error is a cursor that
// does not belong to this result (ErrInvalidCursor).
func RenderImpactReport(res ImpactResult, opt ImpactReportOptions) (ImpactReport, error) {
	budget := opt.Budget
	if budget <= 0 {
		budget = DefaultImpactBudget
	}
	format := opt.Format
	if format == "" {
		format = ImpactReportJSON
	}
	if format != ImpactReportJSON && format != ImpactReportText {
		return ImpactReport{}, fmt.Errorf("unknown impact report format %q: use json or text", format)
	}
	ranked := RankImpactHits(res.Hits)
	filter := impactFingerprint(res, ranked)
	offset, err := decodeImpactCursor(opt.Cursor, filter, len(ranked))
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
		// Tokens is part of what it measures: raise it until it covers its
		// own digits. It only grows, so it settles within a round or two and
		// never reports less than the page costs.
		for {
			est := EstimateTokens(mustMarshal(r))
			if est <= r.Tokens {
				break
			}
			r.Tokens = est
		}
		return r, r.Tokens
	}

	// The cost grows with every hit added (each is tens of bytes, the
	// truncation fields change by a byte or two), so a binary search finds the
	// largest page that fits; every candidate is measured in full.
	if len(rest) == 0 {
		r, _ := page(0)
		return r, nil
	}
	if r, cost := page(len(rest)); cost <= budget {
		return r, nil
	}
	best, _ := page(1)
	lo, hi := 2, len(rest)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if r, cost := page(mid); cost <= budget {
			best, lo = r, mid+1
		} else {
			hi = mid - 1
		}
	}
	return best, nil
}

func mustMarshal(v any) string {
	raw, err := json.Marshal(v)
	if err != nil { // unreachable: plain data
		return ""
	}
	return string(raw)
}

// Clip widths of the text form.
const (
	impactTitleWidth   = 40
	impactReasonWidth  = 60
	impactMessageWidth = 48
)

// clipText shortens s to at most width runes, marking the cut with "...".
func clipText(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:width-3])) + "..."
}

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
