package core

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The token-budget conventions every agent-facing report shares (docs/03
// section 21.11, R-IMP-8 to R-IMP-10): one tokenizer approximation, one
// cursor shape, one way to fit a page into a budget. The impact report
// (GIT-US-0120) and the spec context (GIT-US-0123) both render through these
// helpers, so a budget and a cursor mean the same thing in either.

// DefaultImpactBudget is the token budget of a report that names none: the
// milestone's success criterion for the impact report of a typical PR. The
// spec context uses the same default.
const DefaultImpactBudget = 1500

// MaxImpactBudget bounds a budget a caller may ask for. A caller that wants
// everything walks the cursor instead.
const MaxImpactBudget = 20000

// ErrInvalidCursor reports a report cursor that this package did not issue,
// or that was issued for a different result: the diff, the story or the
// ranking changed under the walk, so resuming would skip or repeat entries.
var ErrInvalidCursor = errors.New("invalid cursor")

// ImpactReportFormat is the form a budgeted report is rendered and measured
// in. The name predates the spec context, which takes the same two forms.
type ImpactReportFormat string

// The two forms.
const (
	// ImpactReportJSON: the report's entries as structured data.
	ImpactReportJSON ImpactReportFormat = "json"
	// ImpactReportText: one terse line per requirement.
	ImpactReportText ImpactReportFormat = "text"
)

// ImpactReportOptions shapes one page of a budgeted report.
type ImpactReportOptions struct {
	// Budget is the token budget of the page (EstimateTokens over the
	// rendered form); 0 is DefaultImpactBudget.
	Budget int
	// Cursor resumes a walk where the previous page stopped; empty starts it.
	Cursor string
	// Format is the form the page is rendered and measured in; empty is JSON.
	Format ImpactReportFormat
}

// resolve fills the defaults of the options and checks the format.
func (o ImpactReportOptions) resolve() (int, ImpactReportFormat, error) {
	budget := o.Budget
	if budget <= 0 {
		budget = DefaultImpactBudget
	}
	format := o.Format
	if format == "" {
		format = ImpactReportJSON
	}
	if format != ImpactReportJSON && format != ImpactReportText {
		return 0, "", fmt.Errorf("unknown report format %q: use json or text", format)
	}
	return budget, format, nil
}

// EstimateTokens is the tokenizer approximation every report budget is
// measured with: one token per three bytes of UTF-8, rounded up. Real BPE
// tokenizers average about four bytes per token over English prose and three
// to three and a half over JSON, identifiers and paths, which is what a report
// is made of, so the rule errs on the side of overestimating: a report within
// its estimated budget is within it for the model too.
func EstimateTokens(s string) int { return (len(s) + 2) / 3 }

// reportCursor is the decoded report cursor: the same {offset, filter
// fingerprint} shape the MCP server's own cursors carry (docs/08 section 3,
// principle 4). Its fingerprint binds it to the ranked entries, not to the
// budget or the form, so a walk may change either between pages.
type reportCursor struct {
	Offset int    `json:"o"`
	Filter string `json:"f"`
}

func encodeReportCursor(offset int, filter string) string {
	raw, err := json.Marshal(reportCursor{Offset: offset, Filter: filter})
	if err != nil { // unreachable: an int and a string
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeReportCursor reads a cursor of the report named what, checks it
// against the fingerprint of the current entries and returns its offset.
func decodeReportCursor(token, filter string, total int, what string) (int, error) {
	if token == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a%s %s cursor", ErrInvalidCursor, token, article(what), what)
	}
	var c reportCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return 0, fmt.Errorf("%w: %q is not a%s %s cursor", ErrInvalidCursor, token, article(what), what)
	}
	if c.Filter != filter {
		return 0, fmt.Errorf("%w: the cursor was issued for a different %s; "+
			"restart without a cursor after the query or the files changed", ErrInvalidCursor, what)
	}
	if c.Offset < 0 || c.Offset > total {
		return 0, fmt.Errorf("%w: the cursor's offset %d is out of range", ErrInvalidCursor, c.Offset)
	}
	return c.Offset, nil
}

// article is the "n" of "an" before a word starting with a vowel.
func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "n"
	}
	return ""
}

// settleTokens sets *tokens to the estimate of v, which holds it: the field is
// part of what it measures, so it is raised until it covers its own digits. It
// only grows, so it settles within a round or two and never reports less than
// the page costs. v is a pointer to the page.
func settleTokens(v any, tokens *int) int {
	for {
		est := EstimateTokens(mustMarshal(v))
		if est <= *tokens {
			return *tokens
		}
		*tokens = est
	}
}

// fitBudget returns the largest page of the n remaining entries whose cost
// fits the budget. page(k) renders the page of the first k entries and
// returns its cost; the cost grows with every entry added (each is tens of
// bytes, the truncation fields change by a byte or two), so a binary search
// finds the largest page that fits, every candidate measured in full. A page
// always carries at least one entry when any remain, so a walk always
// advances; that entry alone may exceed a very small budget.
func fitBudget[R any](n, budget int, page func(k int) (R, int)) R {
	if n == 0 {
		r, _ := page(0)
		return r
	}
	if r, cost := page(n); cost <= budget {
		return r
	}
	best, _ := page(1)
	lo, hi := 2, n-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if r, cost := page(mid); cost <= budget {
			best, lo = r, mid+1
		} else {
			hi = mid - 1
		}
	}
	return best
}

func mustMarshal(v any) string {
	raw, err := json.Marshal(v)
	if err != nil { // unreachable: plain data
		return ""
	}
	return string(raw)
}

// clipText shortens s to at most width runes, marking the cut with "...". It
// folds every run of whitespace, newlines included, into one space.
func clipText(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:width-3])) + "..."
}
