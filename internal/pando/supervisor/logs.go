package supervisor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Log rotation and the stderr tail kept in state.json.
const (
	maxLogBytes = 10 << 20
	keepLogs    = 2
	tailLines   = 20
	maxLineLen  = 4096
)

var bearerRE = regexp.MustCompile(`(?i)(bearer\s+)\S+`)

// logSink receives the child's stdout and stderr. It redacts the instance
// token and any bearer credential line by line, appends to pando.log (rotated
// at 10 MB, 2 rotated files kept) and remembers the last 20 lines.
type logSink struct {
	mu     sync.Mutex
	path   string
	token  string
	file   *os.File
	size   int64
	max    int64
	part   []byte
	tail   []string
	closed bool
}

func newLogSink(dir, token string) (*logSink, error) {
	s := &logSink{path: filepath.Join(dir, logFileName), token: token, max: maxLogBytes}
	if err := s.open(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *logSink) open() error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open the log file: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat the log file: %w", err)
	}
	s.file, s.size = f, st.Size()
	return nil
}

func (s *logSink) redact(line string) string {
	if s.token != "" {
		line = strings.ReplaceAll(line, s.token, "[redacted]")
	}
	return bearerRE.ReplaceAllString(line, "${1}[redacted]")
}

// Write implements io.Writer. It never fails: a log that cannot be written must
// not stop the child, so errors are dropped.
func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return len(p), nil
	}
	s.part = append(s.part, p...)
	for {
		i := bytes.IndexByte(s.part, '\n')
		if i < 0 {
			break
		}
		s.line(string(s.part[:i]))
		s.part = s.part[i+1:]
	}
	if len(s.part) > maxLineLen { // a line that never ends
		s.line(string(s.part))
		s.part = nil
	}
	return len(p), nil
}

func (s *logSink) line(l string) {
	l = s.redact(strings.TrimRight(l, "\r"))
	s.tail = append(s.tail, l)
	if len(s.tail) > tailLines {
		s.tail = s.tail[len(s.tail)-tailLines:]
	}
	if s.file == nil {
		return
	}
	if s.size+int64(len(l))+1 > s.max {
		s.rotate()
	}
	n, _ := fmt.Fprintln(s.file, l)
	s.size += int64(n)
}

func (s *logSink) rotate() {
	_ = s.file.Close()
	s.file = nil
	for i := keepLogs; i >= 1; i-- {
		from := s.path
		if i > 1 {
			from = fmt.Sprintf("%s.%d", s.path, i-1)
		}
		_ = os.Rename(from, fmt.Sprintf("%s.%d", s.path, i))
	}
	_ = s.open()
}

// Tail returns the last lines seen, already redacted.
func (s *logSink) Tail() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tail...)
}

// Close flushes a trailing partial line and closes the file.
func (s *logSink) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if len(s.part) > 0 {
		s.line(string(s.part))
		s.part = nil
	}
	s.closed = true
	if s.file != nil {
		_ = s.file.Close()
	}
}
