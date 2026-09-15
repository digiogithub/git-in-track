package core

import "testing"

// TestIndexSatisfiesSearcher pins the contract of GIT-T-0159: the index is a
// Searcher with no behavior of its own added, and every hit it produces says
// it came from the core backend.
func TestIndexSatisfiesSearcher(t *testing.T) {
	t.Parallel()

	ix, _ := buildFixtureIndex(t)
	var searcher Searcher = ix

	hits := searcher.Search("checkout", 10)
	if len(hits) == 0 {
		t.Fatal("the fixture index found nothing")
	}
	for _, hit := range hits {
		if hit.Source != SearchSourceCore {
			t.Errorf("hit %s reports source %q, want %q", hit.Path, hit.Source, SearchSourceCore)
		}
	}
	// The interface and the method are the same code, so the two must agree.
	direct := ix.Search("checkout", 10)
	if len(direct) != len(hits) {
		t.Errorf("the interface returned %d hits and the method %d", len(hits), len(direct))
	}
	if got := searcher.Search("", 10); got != nil {
		t.Errorf("an empty query returned %d hits", len(got))
	}
}
