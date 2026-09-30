package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The cache policy of the "is there a newer gintrack" answer. The file is
// derived data: deleting it only costs one lookup (AGENTS.md, "Do not store
// state outside Markdown and YAML").
const (
	// CacheFileName is the file, under the gintrack cache directory.
	CacheFileName = "update-check.json"
	// SuccessTTL is how long a successful lookup is trusted.
	SuccessTTL = 6 * time.Hour
	// FailureTTL is how long a failed lookup suppresses the next attempt, so
	// an offline machine does not pay a timeout on every command.
	FailureTTL = 15 * time.Minute
	// LookupTimeout bounds one lookup.
	LookupTimeout = 5 * time.Second
)

// Status is the answer of a Checker.
type Status struct {
	// Current is the running version.
	Current string
	// Latest is the newest release version without the v prefix; empty when no
	// lookup has succeeded.
	Latest string
	// URL is the release page of Latest.
	URL string
	// UpdateAvailable is true when Latest is newer than Current.
	UpdateAvailable bool
	// CheckedAt is when the lookup that produced this answer ran; zero when
	// there is none.
	CheckedAt time.Time
	// NotApplicable is non-empty when update checks make no sense for this
	// install (a development build, a package-manager install); it says why.
	NotApplicable string
	// Err is the message of the last failed lookup, empty otherwise.
	Err string
}

// cacheEntry is the on-disk shape.
type cacheEntry struct {
	CheckedAt  time.Time `json:"checkedAt"`
	OK         bool      `json:"ok"`
	Latest     string    `json:"latest,omitempty"`
	URL        string    `json:"url,omitempty"`
	Prerelease bool      `json:"prerelease,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Checker answers "is a newer release published" from a cache file, and asks
// GitHub only when the cache is missing, corrupt or expired. The zero value
// is not usable: set Dir and Current.
type Checker struct {
	// Dir is the cache directory (config.CacheDir); empty disables the file
	// and every Check looks up.
	Dir string
	// Current is the running version (vX.Y.Z or X.Y.Z).
	Current string
	// Client performs the lookup; nil builds a default one.
	Client *Client
	// NotApplicable returns a non-empty reason when checks do not apply to
	// this install. It is the seam the install-channel detection plugs into;
	// nil means "always applicable" after the version itself parses.
	NotApplicable func() string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Timeout bounds a lookup; zero means LookupTimeout.
	Timeout time.Duration

	mu sync.Mutex
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) path() string {
	if c.Dir == "" {
		return ""
	}
	return filepath.Join(c.Dir, CacheFileName)
}

// skip reports why this install is not checked, or "".
func (c *Checker) skip() (string, Semver) {
	cur, err := ParseSemver(c.Current)
	if err != nil {
		return "development build", Semver{}
	}
	if c.NotApplicable != nil {
		if reason := c.NotApplicable(); reason != "" {
			return reason, cur
		}
	}
	return "", cur
}

// read loads the cache; a missing or corrupt file is simply "no entry".
func (c *Checker) read() (cacheEntry, bool) {
	p := c.path()
	if p == "" {
		return cacheEntry{}, false
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return cacheEntry{}, false
	}
	var e cacheEntry
	if json.Unmarshal(raw, &e) != nil || e.CheckedAt.IsZero() {
		return cacheEntry{}, false
	}
	return e, true
}

// write stores the cache atomically. Failures are ignored: the cache is an
// optimisation.
func (c *Checker) write(e cacheEntry) {
	p := c.path()
	if p == "" {
		return
	}
	raw, err := json.Marshal(e)
	if err != nil || os.MkdirAll(c.Dir, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.Dir, CacheFileName+".*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(name, p) != nil {
		_ = os.Remove(name)
	}
}

// status builds a Status from an entry.
func (c *Checker) status(cur Semver, e cacheEntry, have bool) Status {
	st := Status{Current: c.Current}
	if !have {
		return st
	}
	st.CheckedAt = e.CheckedAt
	if !e.OK {
		st.Err = e.Error
		return st
	}
	st.Latest, st.URL = e.Latest, e.URL
	if latest, err := ParseSemver(e.Latest); err == nil && latest.Compare(cur) > 0 {
		st.UpdateAvailable = true
	}
	return st
}

// fresh reports whether the entry can be used without a lookup.
func (c *Checker) fresh(cur Semver, e cacheEntry) bool {
	ttl := FailureTTL
	if e.OK {
		ttl = SuccessTTL
	}
	age := c.now().Sub(e.CheckedAt)
	// A future timestamp (clock change) is not trusted.
	return age >= 0 && age < ttl && e.Prerelease == cur.IsPrerelease()
}

// Cached answers from the cache only, without touching the network. fresh is
// false when the entry is missing or expired, so the caller knows a refresh is
// due; the returned Status still carries the stale answer, which is what a
// notice wants.
func (c *Checker) Cached() (st Status, fresh bool) {
	reason, cur := c.skip()
	if reason != "" {
		return Status{Current: c.Current, NotApplicable: reason}, true
	}
	e, have := c.read()
	return c.status(cur, e, have), have && c.fresh(cur, e)
}

// Check returns the cached answer when it is fresh and otherwise looks the
// latest release up (bounded by Timeout) and caches the outcome, success or
// failure. A failed lookup is reported in Status.Err, never as an error.
// Concurrent callers share one lookup.
func (c *Checker) Check(ctx context.Context) Status {
	reason, cur := c.skip()
	if reason != "" {
		return Status{Current: c.Current, NotApplicable: reason}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, have := c.read()
	if have && c.fresh(cur, e) {
		return c.status(cur, e, true)
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = LookupTimeout
	}
	lctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := c.Client
	if client == nil {
		client = New(Options{Version: c.Current})
	}
	rel, err := client.Latest(lctx, cur.IsPrerelease())
	entry := cacheEntry{CheckedAt: c.now().UTC(), Prerelease: cur.IsPrerelease()}
	switch {
	case err == nil:
		entry.OK, entry.Latest, entry.URL = true, rel.Version(), rel.HTMLURL
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		// The caller went away: that says nothing about GitHub, so neither
		// cache it nor overwrite a stale answer.
		return c.status(cur, e, have)
	default:
		entry.Error = err.Error()
		if have && e.OK {
			// Keep the last known release next to the failure.
			entry.Latest, entry.URL = e.Latest, e.URL
		}
	}
	c.write(entry)
	st := c.status(cur, entry, true)
	if !entry.OK {
		st.Latest, st.URL = "", ""
		st.UpdateAvailable = false
	}
	return st
}
