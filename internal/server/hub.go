package server

import (
	"fmt"
	"sync"
	"time"
)

// The event types of docs/07 section 5.6.
const (
	eventFileChanged      = "file.changed"
	eventIndexUpdated     = "index.updated"
	eventItemChanged      = "item.changed"
	eventGitCommit        = "git.commit"
	eventSyncProgress     = "sync.progress"
	eventConflictDetected = "conflict.detected"
	eventConflictResolved = "conflict.resolved"
	eventResumeGap        = "resume.gap"
	eventStreamOverflow   = "stream.overflow"
)

// ringCapacity is how many events the hub keeps for `resume` (docs/07 §6.2).
const ringCapacity = 1000

// clientBuffer is the per-client queue. A client that lets it fill up is too
// slow to follow the stream: it is told so and disconnected, rather than
// letting the server accumulate unbounded memory on its behalf.
const clientBuffer = 256

// Event is one frame of the WebSocket stream. Everything but Data is the
// envelope every event shares.
type Event struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	TS        string `json:"ts"`
	Workspace string `json:"workspace,omitempty"`
	Seq       uint64 `json:"seq"`
	Data      any    `json:"data,omitempty"`
}

// hubClient is one connected subscriber.
type hubClient struct {
	// events is the queue the connection drains.
	events chan Event
	// overflowed is closed when the queue filled up, which tells the connection
	// to send stream.overflow and hang up.
	overflowed chan struct{}
	// control carries frames the read side produced (a resume replay, a gap
	// notice) so that only the write side ever touches the socket.
	control chan Event

	mu       sync.Mutex
	topics   map[string]bool
	overflow bool
}

// newHubClient builds a client that receives every topic.
func newHubClient() *hubClient {
	return &hubClient{
		events:     make(chan Event, clientBuffer),
		overflowed: make(chan struct{}),
		control:    make(chan Event, 16),
	}
}

// subscribe narrows the client to a set of topics. An empty set means all of
// them, which is what a client that never sends a subscribe frame gets.
func (c *hubClient) subscribe(topics []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(topics) == 0 {
		c.topics = nil
		return
	}
	c.topics = make(map[string]bool, len(topics))
	for _, t := range topics {
		c.topics[t] = true
	}
}

// unsubscribe drops topics from the client's selection.
func (c *hubClient) unsubscribe(topics []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.topics == nil {
		return
	}
	for _, t := range topics {
		delete(c.topics, t)
	}
}

// wants reports whether the client asked for this event type.
func (c *hubClient) wants(topic string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.topics == nil {
		return true
	}
	return c.topics[topic]
}

// deliver queues one event, or marks the client as overflowed.
func (c *hubClient) deliver(ev Event) {
	if !c.wants(ev.Type) {
		return
	}
	c.mu.Lock()
	if c.overflow {
		c.mu.Unlock()
		return
	}
	select {
	case c.events <- ev:
		c.mu.Unlock()
	default:
		c.mu.Unlock()
		c.markOverflow()
	}
}

// markOverflow declares the client too slow to follow the stream. The write
// pump answers it with stream.overflow and hangs up, which is the documented
// back-pressure policy (docs/07 section 6.2).
func (c *hubClient) markOverflow() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.overflow {
		return
	}
	c.overflow = true
	close(c.overflowed)
}

// Hub fans events out to the connected clients and keeps the last
// ringCapacity of them so that a reconnecting client can resume.
type Hub struct {
	workspace string
	now       func() time.Time

	mu      sync.RWMutex
	clients map[*hubClient]struct{}
	ring    []Event
	seq     uint64
}

// newHub builds an empty hub.
func newHub(workspace string, now func() time.Time) *Hub {
	if now == nil {
		now = time.Now
	}
	return &Hub{
		workspace: workspace,
		now:       now,
		clients:   make(map[*hubClient]struct{}),
		ring:      make([]Event, 0, ringCapacity),
	}
}

// Publish stamps an event with the next sequence number, buffers it and hands
// it to every connected client. It never blocks: a client that cannot keep up
// is marked for disconnection instead.
func (h *Hub) Publish(topic string, data any) Event {
	h.mu.Lock()
	h.seq++
	ev := Event{
		Type:      topic,
		ID:        fmt.Sprintf("evt_%06d", h.seq),
		TS:        h.now().UTC().Format(time.RFC3339Nano),
		Workspace: h.workspace,
		Seq:       h.seq,
		Data:      data,
	}
	if len(h.ring) == ringCapacity {
		copy(h.ring, h.ring[1:])
		h.ring[len(h.ring)-1] = ev
	} else {
		h.ring = append(h.ring, ev)
	}
	h.mu.Unlock()

	for _, c := range h.snapshot() {
		c.deliver(ev)
	}
	return ev
}

// snapshot lists the connected clients, so that the fan-out never holds the
// hub lock while it hands an event to a client.
func (h *Hub) snapshot() []*hubClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*hubClient, 0, len(h.clients))
	for c := range h.clients {
		out = append(out, c)
	}
	return out
}

// register adds a client to the fan-out.
func (h *Hub) register(c *hubClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

// unregister removes a client.
func (h *Hub) unregister(c *hubClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

// clients reports how many connections are attached; the tests use it.
func (h *Hub) clientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// lastSeq reports the sequence number of the newest event.
func (h *Hub) lastSeq() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.seq
}

// since returns the buffered events after seq. It reports false when the ring
// no longer holds that position, which is what resume.gap tells the client.
func (h *Hub) since(seq uint64) ([]Event, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if seq >= h.seq {
		// Nothing was missed; an ahead-of-us cursor is a stale reconnect after a
		// server restart and is treated the same way.
		return nil, seq == h.seq || h.seq == 0
	}
	if len(h.ring) == 0 || h.ring[0].Seq > seq+1 {
		return nil, false
	}
	out := make([]Event, 0, len(h.ring))
	for _, ev := range h.ring {
		if ev.Seq > seq {
			out = append(out, ev)
		}
	}
	return out, true
}
