package syncengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JournalName is the file the queue is persisted to, inside the cache
// directory the caller supplies.
const JournalName = "jobs.json"

// journalVersion is bumped whenever the on-disk shape changes. A file carrying
// an unknown version is treated exactly like a corrupt one: moved aside, never
// fatal.
const journalVersion = 1

// DefaultJournalFlush is how long a burst of state transitions is allowed to
// accumulate before the journal is rewritten. It is what keeps a thousand jobs
// finishing in a second from producing a thousand writes.
const DefaultJournalFlush = 500 * time.Millisecond

// DefaultRetention is how long a done job is kept before it is pruned.
const DefaultRetention = 7 * 24 * time.Hour

// journalDoc is the on-disk shape. It holds bookkeeping only — ids, kinds,
// coalescing keys, states, attempt counts, redacted errors — and never the
// content of an item or a page: the Markdown files remain the source of truth
// and this file is a cache that can always be thrown away.
type journalDoc struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
	Jobs      []Job     `json:"jobs"`
	// DeadLetter lists, oldest first, the ids in Jobs that gave up.
	DeadLetter []string `json:"deadLetter,omitempty"`
}

// journal writes the queue to disk, atomically and rarely.
//
// Rarely, because writes are coalesced behind a timer on the engine's clock: a
// worker marks the journal dirty and moves on, never blocking on disk I/O.
// Atomically, because the file is written next to its target and renamed over
// it, the same discipline internal/core uses for items (writeFileAtomic), so a
// crash can lose the last few transitions but can never leave an unreadable
// journal behind.
type journal struct {
	path  string
	clock Clock
	log   *slog.Logger
	// flush is the coalescing window.
	flush time.Duration
	// snapshot produces the document to write. It is supplied by the engine and
	// is called without the journal lock held.
	snapshot func() journalDoc

	mu     sync.Mutex
	timer  Timer
	armed  bool
	closed bool
	// writes counts completed writes, which is what the coalescing test
	// asserts on.
	writes int
	// lastErr is the last write failure, kept so Close can report it.
	lastErr error
}

// newJournal builds a journal. A path of "" disables persistence entirely and
// every method becomes a no-op, which is what an engine built for a unit test
// of the scheduler wants.
func newJournal(path string, clock Clock, log *slog.Logger, flush time.Duration) *journal {
	if flush <= 0 {
		flush = DefaultJournalFlush
	}
	return &journal{path: path, clock: clock, log: log, flush: flush}
}

// enabled reports whether anything is written at all.
func (j *journal) enabled() bool { return j != nil && j.path != "" }

// markDirty asks for a write soon. Repeated calls inside one window are one
// write.
func (j *journal) markDirty() {
	if !j.enabled() {
		return
	}
	j.mu.Lock()
	if j.closed || j.armed {
		j.mu.Unlock()
		return
	}
	j.armed = true
	j.timer = j.clock.AfterFunc(j.flush, func() {
		j.mu.Lock()
		j.armed = false
		j.mu.Unlock()
		j.write()
	})
	j.mu.Unlock()
}

// write persists the current snapshot now. It is safe to call concurrently
// with markDirty; writes are serialized on the journal's own lock so two of
// them cannot interleave over the same temporary file.
func (j *journal) write() {
	if !j.enabled() || j.snapshot == nil {
		return
	}
	doc := j.snapshot()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	if err := j.writeDoc(doc); err != nil {
		j.lastErr = err
		j.log.Warn("sync engine journal write failed", "path", j.path, "error", err)
		return
	}
	j.writes++
}

// writeDoc renders and stores the document, redacting on the way out: whatever
// a handler put in a payload, and whatever a remote put in an error string, a
// credential must not reach the disk.
func (j *journal) writeDoc(doc journalDoc) error {
	doc.Version = journalVersion
	doc.UpdatedAt = j.clock.Now()
	for i := range doc.Jobs {
		doc.Jobs[i] = redactJob(doc.Jobs[i])
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode journal: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(j.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, j.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", j.path, err)
	}
	return nil
}

// close stops the timer and writes one last time, synchronously, so a clean
// shutdown never loses the transitions of the last window.
func (j *journal) close() error {
	if !j.enabled() {
		return nil
	}
	j.mu.Lock()
	if j.closed {
		err := j.lastErr
		j.mu.Unlock()
		return err
	}
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
	j.armed = false
	j.mu.Unlock()

	j.write()

	j.mu.Lock()
	defer j.mu.Unlock()
	j.closed = true
	return j.lastErr
}

// writeCount reports how many writes have completed.
func (j *journal) writeCount() int {
	if !j.enabled() {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.writes
}

// load reads the journal back. A missing file is an empty queue, not an error.
// A corrupt, truncated or unreadable one is moved aside with a timestamped
// suffix and reported as empty: the journal is derived state, and refusing to
// start because a cache file is damaged would be the worse failure.
func (j *journal) load() journalDoc {
	if !j.enabled() {
		return journalDoc{}
	}
	data, err := os.ReadFile(j.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return journalDoc{}
	case err != nil:
		j.log.Warn("sync engine journal unreadable, starting empty", "path", j.path, "error", err)
		j.moveAside("unreadable")
		return journalDoc{}
	}
	var doc journalDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		j.log.Warn("sync engine journal corrupt, starting empty", "path", j.path, "error", err)
		j.moveAside("corrupt")
		return journalDoc{}
	}
	if doc.Version != journalVersion {
		j.log.Warn("sync engine journal version not understood, starting empty",
			"path", j.path, "version", doc.Version, "expected", journalVersion)
		j.moveAside("version")
		return journalDoc{}
	}
	return doc
}

// moveAside renames a journal the engine could not use, so the next start is
// not blocked by it and a human can still look at what was there.
func (j *journal) moveAside(reason string) {
	stamp := j.clock.Now().UTC().Format("20060102T150405Z")
	target := fmt.Sprintf("%s.%s-%s", j.path, reason, stamp)
	if err := os.Rename(j.path, target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		j.log.Warn("could not move the damaged sync engine journal aside",
			"path", j.path, "target", target, "error", err)
		return
	}
	j.log.Warn("damaged sync engine journal moved aside", "path", j.path, "target", target)
}

// redactJob strips the credentials a job might be carrying, in its payload and
// in its recorded error, before it is written.
func redactJob(job Job) Job {
	out := job.clone()
	out.Payload = redactPayload(out.Payload)
	if out.LastError != nil {
		out.LastError.Message = redactString(out.LastError.Message)
	}
	return out
}

// redactPayload rewrites a JSON payload, replacing the value of every field
// whose name looks like a credential, at any depth. A payload that is not
// valid JSON is redacted as free text instead.
func redactPayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage(redactString(string(raw)))
	}
	v = redactValue(v, false)
	out, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(redactString(string(raw)))
	}
	return out
}

// redactValue walks a decoded JSON value. sensitive marks a value reached
// through a key that looked like a credential.
func redactValue(v any, sensitive bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = redactValue(val, sensitive || isSensitiveKey(k))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactValue(val, sensitive)
		}
		return out
	case string:
		if sensitive {
			return redacted
		}
		return redactString(t)
	default:
		if sensitive {
			return redacted
		}
		return v
	}
}

// isSensitiveKey reports whether a JSON field name names a credential.
func isSensitiveKey(key string) bool {
	k := normalizeKey(key)
	for _, s := range sensitiveKeys {
		if k == normalizeKey(s) {
			return true
		}
	}
	return false
}

// normalizeKey lowercases a field name and drops the separators that make
// api_key, apiKey and api-key the same field.
func normalizeKey(key string) string {
	out := make([]byte, 0, len(key))
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c == '_' || c == '-' || c == ' ':
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
