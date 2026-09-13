package youtrack

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// DefaultSort is appended to a query that has no explicit ordering. Without a
// stable sort, a $skip walk over a live instance silently skips and duplicates
// rows, because YouTrack is free to re-order between requests.
const DefaultSort = "order by: created asc"

// maxPages bounds a walk so a misbehaving server cannot spin forever.
const maxPages = 10000

// Page is one window of a $top/$skip walk.
type Page struct {
	// Skip is the number of rows to skip. Never send a non-zero Skip with a
	// query that has no "order by:" clause; EnsureOrderBy exists for that.
	Skip int
	// Top is the page size. Zero selects DefaultTop; values above MaxTop are
	// clamped.
	Top int
}

// normalize applies the defaults and the clamps.
func (p Page) normalize() Page {
	if p.Top <= 0 {
		p.Top = DefaultTop
	}
	if p.Top > MaxTop {
		p.Top = MaxTop
	}
	if p.Skip < 0 {
		p.Skip = 0
	}
	return p
}

// Next returns the window that follows a full page.
func (p Page) Next() Page {
	p = p.normalize()
	return Page{Skip: p.Skip + p.Top, Top: p.Top}
}

// values renders the page as the $top and $skip parameters.
func (p Page) values() url.Values {
	p = p.normalize()
	q := url.Values{}
	q.Set("$top", strconv.Itoa(p.Top))
	q.Set("$skip", strconv.Itoa(p.Skip))
	return q
}

// EnsureOrderBy appends DefaultSort to a YouTrack search query that does not
// already order its results. It applies to the issue and article query
// language only; the admin endpoints take a substring search instead and must
// not be passed through here.
func EnsureOrderBy(query string) string {
	trimmed := strings.TrimSpace(query)
	if strings.Contains(strings.ToLower(trimmed), "order by:") {
		return trimmed
	}
	if trimmed == "" {
		return DefaultSort
	}
	return trimmed + " " + DefaultSort
}

// ProjectQuery builds the "project: {KEY}" clause, optionally with extra
// conditions, and guarantees a stable ordering. The braces are YouTrack's
// quoting for values that may contain spaces; applying them unconditionally is
// the safe choice.
func ProjectQuery(shortName, extra string) string {
	parts := []string{"project: {" + shortName + "}"}
	if trimmed := strings.TrimSpace(extra); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return EnsureOrderBy(strings.Join(parts, " "))
}

// walkAll repeatedly calls fetch until a short page proves the walk is
// exhausted. The caller's fetch must apply the same ordered query to every
// page.
func walkAll[T any](ctx context.Context, start Page, fetch func(context.Context, Page) ([]T, error)) ([]T, error) {
	page := start.normalize()
	var all []T
	for i := 0; i < maxPages; i++ {
		rows, err := fetch(ctx, page)
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(rows) < page.Top {
			return all, nil
		}
		page = page.Next()
	}
	return all, nil
}
