package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// streamTimeout bounds every wait in the stream tests.
const streamTimeout = 10 * time.Second

// frame is one server envelope as a test reads it.
type frame struct {
	Type string          `json:"type"`
	ID   string          `json:"id"`
	Seq  uint64          `json:"seq"`
	Data json.RawMessage `json:"data"`
}

// liveServer starts the server over a real listener so that the tests speak
// HTTP and WebSocket exactly the way the web app does.
func liveServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()

	s, root := newAPIServer(t)
	httpSrv := httptest.NewServer(s.Handler())
	t.Cleanup(httpSrv.Close)
	return s, httpSrv, root
}

// dialEvents opens the event stream and waits until the hub has registered it.
func dialEvents(ctx context.Context, t *testing.T, s *Server, httpSrv *httptest.Server, query string) *websocket.Conn {
	t.Helper()

	before := s.hub.clientCount()
	url := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/api/v1/events?token=test-token" + query
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: httpSrv.Client()})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial the event stream: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	deadline := time.Now().Add(streamTimeout)
	for s.hub.clientCount() <= before {
		if time.Now().After(deadline) {
			t.Fatal("the hub never registered the connection")
		}
		time.Sleep(2 * time.Millisecond)
	}
	return conn
}

// readFrame reads one envelope.
func readFrame(ctx context.Context, t *testing.T, conn *websocket.Conn) frame {
	t.Helper()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read a frame: %v", err)
	}
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return f
}

// awaitFrame reads until an envelope of the wanted type arrives.
func awaitFrame(ctx context.Context, t *testing.T, conn *websocket.Conn, want string) frame {
	t.Helper()

	for {
		f := readFrame(ctx, t, conn)
		if f.Type == want {
			return f
		}
	}
}

// apiPatch performs one authenticated write against the live server.
func apiPatch(ctx context.Context, t *testing.T, httpSrv *httptest.Server, path, rev string, body any) *http.Response {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, httpSrv.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", rev)
	resp, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("PATCH %s: %v", path, err)
	}
	return resp
}

func TestEventStreamPublishesAPIWrites(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	s, httpSrv, _ := liveServer(t)
	conn := dialEvents(ctx, t, s, httpSrv, "")
	if err := conn.Write(ctx, websocket.MessageText,
		[]byte(`{"op":"subscribe","topics":["item.changed","index.updated","file.changed"]}`)); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	rev, _ := getItem(t, s, "DEMO-T-0001")
	resp := apiPatch(ctx, t, httpSrv, "/api/v1/items/DEMO-T-0001", rev, map[string]any{"priority": "low"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d", resp.StatusCode)
	}

	changed := awaitFrame(ctx, t, conn, eventItemChanged)
	if changed.Seq == 0 || changed.ID == "" {
		t.Errorf("envelope = %+v, want a sequence number and an id", changed)
	}
	var payload struct {
		Repo   string `json:"repo"`
		ID     string `json:"id"`
		Op     string `json:"op"`
		Origin string `json:"origin"`
		Rev    string `json:"rev"`
	}
	if err := json.Unmarshal(changed.Data, &payload); err != nil {
		t.Fatalf("decode the payload: %v", err)
	}
	if payload.ID != "DEMO-T-0001" || payload.Op != "updated" {
		t.Errorf("payload = %+v", payload)
	}
	if payload.Repo != testRepoID || payload.Origin != "api" {
		t.Errorf("payload = %+v, want repo %q from the api", payload, testRepoID)
	}
	if payload.Rev == rev {
		t.Error("the event carries the revision the write replaced")
	}

	updated := awaitFrame(ctx, t, conn, eventIndexUpdated)
	var stats struct {
		Repo    string `json:"repo"`
		Updated int    `json:"updated"`
		Items   int    `json:"items"`
	}
	if err := json.Unmarshal(updated.Data, &stats); err != nil {
		t.Fatalf("decode the index payload: %v", err)
	}
	if stats.Repo != testRepoID || stats.Updated != 1 || stats.Items == 0 {
		t.Errorf("index payload = %+v", stats)
	}
	if updated.Seq <= changed.Seq {
		t.Errorf("sequence numbers are not monotonic: %d then %d", changed.Seq, updated.Seq)
	}
}

func TestEventStreamResumesFromASequence(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	s, httpSrv, _ := liveServer(t)

	// Two writes happen while nobody is listening.
	rev, _ := getItem(t, s, "DEMO-T-0001")
	first := apiPatch(ctx, t, httpSrv, "/api/v1/items/DEMO-T-0001", rev, map[string]any{"priority": "low"})
	_ = first.Body.Close()
	afterFirst := s.hub.lastSeq()

	rev, _ = getItem(t, s, "DEMO-US-0002")
	second := apiPatch(ctx, t, httpSrv, "/api/v1/items/DEMO-US-0002", rev, map[string]any{"priority": "critical"})
	_ = second.Body.Close()

	conn := dialEvents(ctx, t, s, httpSrv, "")
	frame := []byte(`{"op":"resume","seq":` + strconv.FormatUint(afterFirst, 10) + `}`)
	if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
		t.Fatalf("resume: %v", err)
	}

	replayed := awaitFrame(ctx, t, conn, eventItemChanged)
	if replayed.Seq <= afterFirst {
		t.Errorf("replayed seq = %d, want after %d", replayed.Seq, afterFirst)
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(replayed.Data, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.ID != "DEMO-US-0002" {
		t.Errorf("replayed %s, want the write that happened after the cursor", payload.ID)
	}
}

func TestEventStreamResumeGapWhenTheRingMovedOn(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	s, httpSrv, _ := liveServer(t)
	// The ring holds 1000 events; publishing more than that drops the position
	// a client resuming from 1 is asking for.
	for i := 0; i < ringCapacity+5; i++ {
		s.hub.Publish(eventFileChanged, fileChangedData{Repo: testRepoID, Path: "docs/index.md", Op: "write"})
	}

	conn := dialEvents(ctx, t, s, httpSrv, "")
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"op":"resume","seq":1}`)); err != nil {
		t.Fatalf("resume: %v", err)
	}
	gap := awaitFrame(ctx, t, conn, eventResumeGap)
	if gap.Seq == 0 {
		t.Errorf("the gap notice carries no position: %+v", gap)
	}
}

func TestEventStreamOverflowClosesTheConnection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	s, httpSrv, _ := liveServer(t)
	conn := dialEvents(ctx, t, s, httpSrv, "")

	// A client that stops draining its 256-event queue is disconnected rather
	// than served from an unbounded buffer.
	for _, c := range s.hub.snapshot() {
		c.markOverflow()
	}

	overflow := awaitFrame(ctx, t, conn, eventStreamOverflow)
	if overflow.Type != eventStreamOverflow {
		t.Fatalf("frame = %+v", overflow)
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Error("the connection stayed open after stream.overflow")
	}
}

func TestEventStreamRefusesAnUnauthenticatedUpgrade(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	_, httpSrv, _ := liveServer(t)
	url := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/api/v1/events"
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: httpSrv.Client()})
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("the upgrade succeeded without a token")
	}
	if resp == nil {
		t.Fatal("no response to inspect")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHubBuffersAndDropsSlowClients(t *testing.T) {
	t.Parallel()

	hub := newHub("test", func() time.Time { return time.Unix(0, 0).UTC() })
	client := newHubClient()
	hub.register(client)

	for i := 0; i < clientBuffer; i++ {
		hub.Publish(eventItemChanged, map[string]any{"n": i})
	}
	select {
	case <-client.overflowed:
		t.Fatal("a client that filled its queue exactly is not overflowed yet")
	default:
	}

	hub.Publish(eventItemChanged, map[string]any{"n": "one too many"})
	select {
	case <-client.overflowed:
	default:
		t.Fatal("the client was not marked as overflowed")
	}

	t.Run("the ring is bounded", func(t *testing.T) {
		for i := 0; i < ringCapacity; i++ {
			hub.Publish(eventFileChanged, nil)
		}
		if _, ok := hub.since(1); ok {
			t.Error("a position the ring dropped must report a gap")
		}
		last := hub.lastSeq()
		missed, ok := hub.since(last - 3)
		if !ok || len(missed) != 3 {
			t.Errorf("since(last-3) = %d events, ok=%v", len(missed), ok)
		}
	})

	t.Run("topics filter", func(t *testing.T) {
		listener := newHubClient()
		listener.subscribe([]string{eventIndexUpdated})
		hub.register(listener)
		hub.Publish(eventFileChanged, nil)
		hub.Publish(eventIndexUpdated, nil)
		select {
		case ev := <-listener.events:
			if ev.Type != eventIndexUpdated {
				t.Errorf("received %q, want only the subscribed topic", ev.Type)
			}
		default:
			t.Error("the subscribed event was not delivered")
		}
	})
}
