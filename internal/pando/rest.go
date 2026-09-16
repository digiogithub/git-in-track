package pando

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxErrorBody caps how much of a failing response body is kept in an error,
// so a stray HTML error page does not end up in a log line.
const maxErrorBody = 512

type reindexStatsWire struct {
	Scanned      int `json:"scanned"`
	Added        int `json:"added"`
	Updated      int `json:"updated"`
	Unchanged    int `json:"unchanged"`
	Deleted      int `json:"deleted"`
	LinksIndexed int `json:"links_indexed"`
}

// ReindexKB asks Pando to re-sync its knowledge-base filesystem mirror into the
// database. Pando's KBPath is the repository's own documentation directory
// (GIT-EP-0020) and its watcher follows it, so this is the catch-up pass — for
// a companion that was started after a batch of commits landed, or a Pando
// whose watcher was off — rather than the only way its index ever changes.
//
// It is the one call that does not go over MCP. Pando exposes the reindex on
// its REST surface only (POST /api/v1/remembrances/kb/reindex, authenticated
// with X-Pando-Token), and that surface needs `pando serve` rather than
// `pando mcp-server`, so it is configured separately and may well be absent:
// with no REST URL configured the call returns ErrNotConfigured rather than
// pretending to have run.
//
// Pando serializes reindex runs and answers a second concurrent caller with
// 409, which arrives here as ErrReindexRunning: retry later, do not treat it
// as a failure.
func (c *Client) ReindexKB(ctx context.Context) (ReindexStats, error) {
	var zero ReindexStats
	if c.opts.RESTURL == "" {
		return zero, fmt.Errorf("%w: no Pando REST URL", ErrNotConfigured)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	const path = "/api/v1/remembrances/kb/reindex"
	endpoint := strings.TrimRight(c.opts.RESTURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return zero, fmt.Errorf("%w: %w", ErrInvalidOptions, err)
	}
	req.Header.Set("Accept", "application/json")
	if c.opts.RESTToken != "" {
		req.Header.Set("X-Pando-Token", c.opts.RESTToken)
	}

	resp, err := c.rest.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return zero, fmt.Errorf("%w: %w", ErrTimeout, err)
		}
		return zero, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return zero, &httpError{
			Status: resp.StatusCode,
			Method: http.MethodPost,
			Path:   path,
			Body:   strings.TrimSpace(string(body)),
		}
	}

	var wire reindexStatsWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return zero, fmt.Errorf("%w: the reindex response could not be read: %w", ErrUnreachable, err)
	}
	return ReindexStats(wire), nil
}
