package server

import (
	"context"
	"net/http"
	"time"

	"github.com/digiogithub/git-in-track/internal/selfupdate"
)

// UpdateChecker answers whether a newer release exists. It is satisfied by
// *selfupdate.Checker, which caches the answer and bounds the lookup, so the
// handler never waits longer than one lookup timeout.
type UpdateChecker interface {
	Check(ctx context.Context) selfupdate.Status
}

// versionResponse is the body of GET /api/v1/version. It carries no secret:
// the release URL is the public GitHub page.
type versionResponse struct {
	Current         string     `json:"current"`
	Latest          string     `json:"latest"`
	UpdateAvailable bool       `json:"updateAvailable"`
	CheckedAt       *time.Time `json:"checkedAt"`
	URL             string     `json:"url"`
}

// handleVersion reports the running version and the latest published release.
// A failed lookup, a development build and a missing checker all answer 200
// with updateAvailable false: "unknown" is never an error for a notice.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	out := versionResponse{Current: s.opts.Version}
	if s.opts.Update != nil {
		st := s.opts.Update.Check(r.Context())
		out.Latest, out.URL, out.UpdateAvailable = st.Latest, st.URL, st.UpdateAvailable
		if !st.CheckedAt.IsZero() {
			t := st.CheckedAt.UTC()
			out.CheckedAt = &t
		}
	}
	writeJSON(w, r, http.StatusOK, out)
}
