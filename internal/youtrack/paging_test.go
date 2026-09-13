package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestEnsureOrderBy(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"empty", "", DefaultSort},
		{"blank", "   ", DefaultSort},
		{"plain", "project: {ACME}", "project: {ACME} " + DefaultSort},
		{"already ordered", "project: {ACME} order by: updated desc", "project: {ACME} order by: updated desc"},
		{"ordered in another case", "project: {ACME} ORDER BY: updated desc", "project: {ACME} ORDER BY: updated desc"},
		{"trimmed", "  #Unresolved  ", "#Unresolved " + DefaultSort},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EnsureOrderBy(tc.query); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProjectQuery(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		extra string
		want  string
	}{
		{"key only", "ACME", "", "project: {ACME} " + DefaultSort},
		{"with extra", "ACME", "  #Unresolved ", "project: {ACME} #Unresolved " + DefaultSort},
		{"caller ordered", "ACME", "order by: updated desc", "project: {ACME} order by: updated desc"},
		{"key with spaces", "My Project", "", "project: {My Project} " + DefaultSort},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProjectQuery(tc.key, tc.extra); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPageNormalizesAndAdvances(t *testing.T) {
	tests := []struct {
		name         string
		page         Page
		wantTop      int
		wantSkip     int
		wantNextSkip int
	}{
		{"zero value", Page{}, DefaultTop, 0, DefaultTop},
		{"custom top", Page{Top: 25}, 25, 0, 25},
		{"clamped top", Page{Top: 5000}, MaxTop, 0, MaxTop},
		{"negative skip", Page{Skip: -10, Top: 50}, 50, 0, 50},
		{"mid walk", Page{Skip: 200, Top: 100}, 100, 200, 300},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.page.normalize()
			if got.Top != tc.wantTop || got.Skip != tc.wantSkip {
				t.Fatalf("normalize = %+v, want top %d skip %d", got, tc.wantTop, tc.wantSkip)
			}
			if next := tc.page.Next(); next.Skip != tc.wantNextSkip {
				t.Fatalf("Next().Skip = %d, want %d", next.Skip, tc.wantNextSkip)
			}
		})
	}
}

// pagingServer serves total synthetic issues, in a stable order, and records
// what each request asked for.
type pagingServer struct {
	mu      sync.Mutex
	total   int
	queries []string
	tops    []int
	skips   []int
}

func (p *pagingServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		top, err := strconv.Atoi(query.Get("$top"))
		if err != nil {
			t.Errorf("missing or bad $top: %q", query.Get("$top"))
			http.Error(w, "bad $top", http.StatusBadRequest)
			return
		}
		skip, err := strconv.Atoi(query.Get("$skip"))
		if err != nil {
			t.Errorf("missing or bad $skip: %q", query.Get("$skip"))
			http.Error(w, "bad $skip", http.StatusBadRequest)
			return
		}

		p.mu.Lock()
		p.queries = append(p.queries, query.Get("query"))
		p.tops = append(p.tops, top)
		p.skips = append(p.skips, skip)
		total := p.total
		p.mu.Unlock()

		rows := make([]string, 0, top)
		for i := skip; i < skip+top && i < total; i++ {
			rows = append(rows, fmt.Sprintf(`{"id":"2-%d","idReadable":"ACME-%d","summary":"row %d"}`, i, i, i))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "[%s]", strings.Join(rows, ","))
	}
}

// TestSearchAllIssuesWalksEveryRowOnce is the paging contract: the walk visits
// every row exactly once, stops on a short page, and every request carries an
// explicit ordering, without which $skip silently skips and duplicates rows.
func TestSearchAllIssuesWalksEveryRowOnce(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		top       int
		wantPages int
	}{
		{"two full pages and a short one", 250, 100, 3},
		{"exact multiple needs a trailing empty page", 200, 100, 3},
		{"single short page", 7, 100, 1},
		{"empty result", 0, 100, 1},
		{"small pages", 25, 10, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := &pagingServer{total: tc.total}
			srv, _ := newTestServer(t, backend.handler(t))
			client := newTestClient(t, srv.URL, newFakeClock(), nil)

			issues, err := client.SearchAllIssues(context.Background(), "project: {ACME}", Page{Top: tc.top})
			if err != nil {
				t.Fatalf("SearchAllIssues: %v", err)
			}
			if len(issues) != tc.total {
				t.Fatalf("got %d issues, want %d", len(issues), tc.total)
			}

			seen := make(map[string]int, len(issues))
			for _, issue := range issues {
				seen[issue.IDReadable]++
			}
			for i := 0; i < tc.total; i++ {
				id := fmt.Sprintf("ACME-%d", i)
				switch seen[id] {
				case 1:
				case 0:
					t.Fatalf("%s was never visited", id)
				default:
					t.Fatalf("%s was visited %d times", id, seen[id])
				}
			}

			backend.mu.Lock()
			defer backend.mu.Unlock()
			if len(backend.queries) != tc.wantPages {
				t.Fatalf("%d requests, want %d", len(backend.queries), tc.wantPages)
			}
			for i, query := range backend.queries {
				if !strings.Contains(strings.ToLower(query), "order by:") {
					t.Fatalf("request %d paged without an ordering: %q", i, query)
				}
				if backend.tops[i] != tc.top {
					t.Fatalf("request %d $top = %d, want %d", i, backend.tops[i], tc.top)
				}
				if want := i * tc.top; backend.skips[i] != want {
					t.Fatalf("request %d $skip = %d, want %d", i, backend.skips[i], want)
				}
			}
		})
	}
}

// TestSearchRespectsACallerSuppliedOrdering proves the guard does not fight a
// caller that already ordered its query.
func TestSearchRespectsACallerSuppliedOrdering(t *testing.T) {
	backend := &pagingServer{total: 5}
	srv, _ := newTestServer(t, backend.handler(t))
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	const query = "project: {ACME} order by: updated desc"
	if _, err := client.SearchIssues(context.Background(), query, Page{}); err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.queries[0] != query {
		t.Fatalf("query = %q, want %q unchanged", backend.queries[0], query)
	}
	if strings.Count(strings.ToLower(backend.queries[0]), "order by:") != 1 {
		t.Fatalf("the ordering was duplicated: %q", backend.queries[0])
	}
}

// TestCommentsWalkPagesToo covers the sub-resource walk, which pages on $top
// and $skip without a query.
func TestCommentsWalkPagesToo(t *testing.T) {
	var requests int
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		top, _ := strconv.Atoi(r.URL.Query().Get("$top"))
		skip, _ := strconv.Atoi(r.URL.Query().Get("$skip"))
		const total = 3
		rows := make([]string, 0, top)
		for i := skip; i < skip+top && i < total; i++ {
			rows = append(rows, fmt.Sprintf(`{"id":"4-%d","text":"c%d"}`, i, i))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "[%s]", strings.Join(rows, ","))
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	comments, err := client.AllComments(context.Background(), "ACME-42")
	if err != nil {
		t.Fatalf("AllComments: %v", err)
	}
	if len(comments) != 3 {
		t.Fatalf("got %d comments, want 3", len(comments))
	}
	if requests != 1 {
		t.Fatalf("%d requests, want 1 short page", requests)
	}
}
