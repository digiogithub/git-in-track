package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// pingInterval is the keepalive period of the event stream (docs/07 §5.6).
const pingInterval = 30 * time.Second

// pingTimeout bounds how long a pong may take before the peer is considered
// gone. Two missed pings therefore close the connection within a minute.
const pingTimeout = 25 * time.Second

// writeTimeout bounds a single frame write, so that a stalled TCP connection
// cannot pin the goroutine forever.
const writeTimeout = 10 * time.Second

// maxClientFrame is the largest client frame the server reads. Client frames
// are tiny control messages; anything bigger is a protocol error.
const maxClientFrame = 32 << 10

// clientFrame is a message from a client: subscribe, unsubscribe, resume or
// ping (docs/07 section 5.6).
type clientFrame struct {
	Op     string   `json:"op"`
	Topics []string `json:"topics,omitempty"`
	Seq    uint64   `json:"seq,omitempty"`
}

// handleEvents upgrades the connection and streams events until the client
// leaves. Authentication already happened in the middleware chain, which reads
// the token from the Authorization header, the `token` query parameter or the
// bearer sub-protocol, because a browser cannot set headers on a WebSocket.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.originPatterns(),
		Subprotocols:   []string{subprotocol},
	})
	if err != nil {
		s.log.Debug("websocket upgrade rejected", "error", err)
		return
	}
	conn.SetReadLimit(maxClientFrame)
	defer func() { _ = conn.CloseNow() }()

	client := newHubClient()
	s.hub.register(client)
	defer s.hub.unregister(client)

	// The request context dies with the handler, which is exactly the lifetime
	// of the connection; a cancel of our own lets either pump stop the other.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go s.readFrames(ctx, cancel, conn, client)
	s.writeFrames(ctx, conn, client)
}

// readFrames handles the client half of the protocol.
func (s *Server) readFrames(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, client *hubClient) {
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var frame clientFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		switch frame.Op {
		case "subscribe":
			client.subscribe(frame.Topics)
		case "unsubscribe":
			client.unsubscribe(frame.Topics)
		case "resume":
			s.replay(ctx, client, frame.Seq)
		case "ping":
			s.queueControl(ctx, client, Event{Type: "pong", TS: s.now().UTC().Format(time.RFC3339Nano)})
		default:
			// Unknown ops are ignored: the protocol is additive.
		}
	}
}

// replay pushes the buffered events a resuming client missed, or the gap notice
// telling it to refetch.
func (s *Server) replay(ctx context.Context, client *hubClient, seq uint64) {
	missed, ok := s.hub.since(seq)
	if !ok {
		s.queueControl(ctx, client, Event{
			Type: eventResumeGap,
			TS:   s.now().UTC().Format(time.RFC3339Nano),
			Seq:  s.hub.lastSeq(),
		})
		return
	}
	for _, ev := range missed {
		s.queueControl(ctx, client, ev)
	}
}

// queueControl hands a frame to the write pump, dropping it when the client is
// already too far behind to matter.
func (s *Server) queueControl(ctx context.Context, client *hubClient, ev Event) {
	select {
	case client.control <- ev:
	case <-ctx.Done():
	case <-client.overflowed:
	}
}

// writeFrames is the only writer on the socket: events, replays and keepalives
// all go through it.
func (s *Server) writeFrames(ctx context.Context, conn *websocket.Conn, client *hubClient) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-client.overflowed:
			// The client stopped draining its queue. Tell it, then hang up: it
			// reconnects and refetches instead of reading a truncated stream.
			_ = s.writeFrame(ctx, conn, Event{
				Type: eventStreamOverflow,
				TS:   s.now().UTC().Format(time.RFC3339Nano),
				Seq:  s.hub.lastSeq(),
			})
			_ = conn.Close(websocket.StatusTryAgainLater, eventStreamOverflow)
			return
		case ev := <-client.control:
			if err := s.writeFrame(ctx, conn, ev); err != nil {
				return
			}
		case ev := <-client.events:
			if err := s.writeFrame(ctx, conn, ev); err != nil {
				return
			}
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// writeFrame marshals and sends one envelope.
func (s *Server) writeFrame(ctx context.Context, conn *websocket.Conn, ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err //nolint:wrapcheck // the caller only decides whether to stop
	}
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		return err //nolint:wrapcheck // idem: the connection is being torn down
	}
	return nil
}

// ------------------------------------------------------------- publishing --

// itemChangedData is the payload of an item.changed event.
type itemChangedData struct {
	Repo      string `json:"repo"`
	ID        string `json:"id"`
	Op        string `json:"op"`
	Rev       string `json:"rev,omitempty"`
	Origin    string `json:"origin"`
	RequestID string `json:"requestId,omitempty"`
}

// fileChangedData is the payload of a file.changed event.
type fileChangedData struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Op      string `json:"op"`
	Size    int64  `json:"size"`
	IsPmngr bool   `json:"isPmngr"`
	IsKb    bool   `json:"isKb"`
}

// indexCounts is what one index pass changed.
type indexCounts struct {
	Full    bool
	Added   int
	Updated int
	Removed int
}

// indexUpdatedData is the payload of an index.updated event: the counts of the
// pass plus the index statistics the shared core reports, so that a client can
// refresh its badges from the event alone.
type indexUpdatedData struct {
	Repo      string `json:"repo"`
	Full      bool   `json:"full"`
	Added     int    `json:"added"`
	Updated   int    `json:"updated"`
	Removed   int    `json:"removed"`
	Origin    string `json:"origin,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	vault.IndexStats
}

// requestIDOf returns the id the request logger and the problem documents use.
func requestIDOf(r *http.Request) string {
	if r == nil {
		return ""
	}
	return middleware.GetReqID(r.Context())
}

// publishIndexUpdated announces an index pass.
func (s *Server) publishIndexUpdated(m *mount, counts indexCounts, requestID string) {
	if m == nil || !m.ready() {
		return
	}
	s.hub.Publish(eventIndexUpdated, indexUpdatedData{
		Repo:       m.id,
		Full:       counts.Full,
		Added:      counts.Added,
		Updated:    counts.Updated,
		Removed:    counts.Removed,
		Origin:     originOf(requestID),
		RequestID:  requestID,
		IndexStats: m.vlt.Stats(),
	})
}

// originOf says where a change came from, so that the tab that made it can skip
// its own optimistic-update echo (docs/07 section 5.6).
func originOf(requestID string) string {
	if requestID == "" {
		return "watcher"
	}
	return "api"
}

// publishWrite announces one write made through the API: what changed and what
// the index now holds.
func (s *Server) publishWrite(r *http.Request, m *mount, result any, id, op string) {
	requestID := requestIDOf(r)
	if id != "" {
		s.hub.Publish(eventItemChanged, itemChangedData{
			Repo:      m.id,
			ID:        id,
			Op:        op,
			Rev:       revOf(field(result, "item")),
			Origin:    "api",
			RequestID: requestID,
		})
	}
	counts := indexCounts{Updated: 1}
	switch op {
	case "created":
		counts = indexCounts{Added: 1}
	case "deleted":
		counts = indexCounts{Removed: 1}
	}
	s.publishIndexUpdated(m, counts, requestID)
	m.touch(s.now())
}

// publishPageWrite announces a knowledge-base write. A page is not an item, so
// the file event is what tells the UI to refetch it.
func (s *Server) publishPageWrite(r *http.Request, m *mount, result any) {
	requestID := requestIDOf(r)
	writes, ok := writesOf(result)
	if ok {
		for _, file := range writes.Written {
			s.hub.Publish(eventFileChanged, fileChangedData{
				Repo:    m.id,
				Path:    file.Path,
				Op:      "write",
				Size:    int64(len(file.Text)),
				IsPmngr: isBacklogPath(file.Path),
				IsKb:    !isBacklogPath(file.Path),
			})
		}
	}
	s.publishIndexUpdated(m, indexCounts{Updated: 1}, requestID)
	m.touch(s.now())
}

// publishBoardMove announces a card move. It writes to two repositories, so it
// publishes one file event per repository and, when a status changed, the item
// event the backlog views listen to (docs/04 R-MOVE-1).
func (s *Server) publishBoardMove(r *http.Request, result any) {
	moved, ok := result.(vault.BoardMoveResult)
	if !ok {
		return
	}
	requestID := requestIDOf(r)
	for _, set := range moved.Writes {
		m, found := s.repos.lookup(set.VaultID)
		for _, file := range set.Written {
			repo := set.VaultID
			if found {
				repo = m.id
			}
			s.hub.Publish(eventFileChanged, fileChangedData{
				Repo:    repo,
				Path:    file.Path,
				Op:      "write",
				Size:    int64(len(file.Text)),
				IsPmngr: isBacklogPath(file.Path),
				IsKb:    !isBacklogPath(file.Path),
			})
		}
		if !found {
			continue
		}
		if moved.Item != nil && string(moved.Item.ID) != "" && set.VaultID != "" {
			s.hub.Publish(eventItemChanged, itemChangedData{
				Repo:      m.id,
				ID:        string(moved.Item.ID),
				Op:        "moved",
				Rev:       string(moved.Item.Rev),
				Origin:    "api",
				RequestID: requestID,
			})
		}
		s.publishIndexUpdated(m, indexCounts{Updated: 1}, requestID)
		m.touch(s.now())
	}
}

// publishDelta announces what an incremental index pass changed. It is the
// watcher's counterpart of publishWrite.
func (s *Server) publishDelta(m *mount, delta core.IndexDelta) {
	changed := func(ids []core.ItemID, op string) {
		for _, id := range ids {
			s.hub.Publish(eventItemChanged, itemChangedData{
				Repo:   m.id,
				ID:     string(id),
				Op:     op,
				Origin: "watcher",
			})
		}
	}
	changed(delta.Added, "created")
	changed(delta.Updated, "updated")
	changed(delta.Removed, "deleted")
	changed(delta.CommentsChanged, "commented")

	s.publishIndexUpdated(m, indexCounts{
		Added:   len(delta.Added) + len(delta.PagesAdded),
		Updated: len(delta.Updated) + len(delta.PagesUpdated),
		Removed: len(delta.Removed) + len(delta.PagesRemoved),
	}, "")
}

// isBacklogPath reports whether a vault-relative path lives in a `.pmngr`
// backlog folder rather than in the knowledge base.
func isBacklogPath(p string) bool {
	return p == ".pmngr" || strings.HasPrefix(p, ".pmngr/") || strings.Contains(p, "/.pmngr/")
}
