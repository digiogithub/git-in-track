package tunnel

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// teardownNoise lists the messages cloudflared logs at ERROR level while a
// tunnel is being cancelled. They are the normal consequence of tearing a
// healthy tunnel down, so they are demoted to debug while the manager is
// stopping; otherwise switching the tunnel off would look like a failure in
// gintrack's own log.
var teardownNoise = []string{
	"failed to run the datagram handler",
	"failed to serve tunnel connection",
	"Serve tunnel error",
}

// slogWriter is the smallest possible zerolog-to-slog bridge: zerolog writes
// one JSON object per event to its io.Writer, so the object is decoded and
// re-emitted through the slog.Logger the embedding program gave us. cloudflared
// only accepts a *zerolog.Logger, and gintrack only speaks slog.
type slogWriter struct {
	log *slog.Logger
	// quiet is shared with the Manager: while it is set, the ERROR lines listed
	// in teardownNoise are demoted to debug.
	quiet *atomic.Bool
}

// Write implements io.Writer for a single zerolog JSON event.
func (w slogWriter) Write(p []byte) (int, error) {
	var fields map[string]any
	if isJSON := json.Unmarshal(p, &fields) == nil; !isJSON {
		// Not a JSON event (zerolog never emits one, but a caller-supplied
		// hook could): pass the raw line through rather than dropping it.
		w.log.Info(strings.TrimSpace(string(p)))
		return len(p), nil
	}

	msg, _ := fields[zerolog.MessageFieldName].(string)
	levelName, _ := fields[zerolog.LevelFieldName].(string)
	delete(fields, zerolog.MessageFieldName)
	delete(fields, zerolog.LevelFieldName)
	delete(fields, zerolog.TimestampFieldName)

	level := slogLevel(levelName)
	if level >= slog.LevelError && w.quiet != nil && w.quiet.Load() && isTeardownNoise(msg) {
		level = slog.LevelDebug
	}

	attrs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}
	w.log.Log(nil, level, msg, attrs...) //nolint:staticcheck // slog accepts a nil context; this writer has none to offer
	return len(p), nil
}

// slogLevel maps a zerolog level name to the closest slog level.
func slogLevel(name string) slog.Level {
	switch name {
	case "trace", "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error", "fatal", "panic":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// isTeardownNoise reports whether msg is one of the errors cloudflared logs
// while a tunnel is being cancelled on purpose.
func isTeardownNoise(msg string) bool {
	for _, noise := range teardownNoise {
		if strings.Contains(msg, noise) {
			return true
		}
	}
	return false
}

// newZerolog returns a zerolog.Logger that funnels every event into log.
func newZerolog(log *slog.Logger, quiet *atomic.Bool) zerolog.Logger {
	return zerolog.New(slogWriter{log: log, quiet: quiet}).Level(zerolog.InfoLevel)
}
