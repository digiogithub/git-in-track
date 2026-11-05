package server

import (
	"net/http"
	"testing"
)

// TestProjectKPIsRoute answers the core's figures for a mounted project and a
// not_found problem for any other key (GIT-US-0205).
func TestProjectKPIsRoute(t *testing.T) {
	t.Parallel()
	s, _ := specServer(t)

	var got struct {
		Project string `json:"project"`
		Stories struct {
			Total int `json:"total"`
		} `json:"stories"`
		Specs struct {
			Total int `json:"total"`
		} `json:"specs"`
		Progress struct {
			Ratio float64 `json:"ratio"`
		} `json:"progress"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/projects/DEMO/kpis"}), http.StatusOK, &got)
	if got.Project != "DEMO" || got.Stories.Total == 0 || got.Specs.Total != 1 {
		t.Errorf("kpis = %+v, want DEMO with its stories and one spec", got)
	}
	if got.Progress.Ratio < 0 || got.Progress.Ratio > 1 {
		t.Errorf("ratio = %v, want within [0, 1]", got.Progress.Ratio)
	}

	var doc problemBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/projects/NOPE/kpis"}),
		http.StatusNotFound, &doc)
	if doc.Code != "not_found" {
		t.Errorf("code = %q, want not_found", doc.Code)
	}
}
