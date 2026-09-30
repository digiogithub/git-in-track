package supervisor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/pando"
)

// File names inside an instance directory.
const (
	configFileName = ".pando.toml"
	tokenFileName  = "token"
	stateFileName  = "state.json"
	lockFileName   = "supervisor.lock"
	logFileName    = "pando.log"
	dataDirName    = "data"
)

// State is the supervisor's view of an instance.
type State string

// The states an instance moves through. failed is left only by Restart.
const (
	StateStopped    State = "stopped"
	StateStarting   State = "starting"
	StateReady      State = "ready"
	StateRestarting State = "restarting"
	StateFailed     State = "failed"
)

// Kind is what an instance runs.
type Kind string

// The kinds of instance. The zero value means KindMCP.
const (
	// KindMCP is `pando mcp-server`, the semantic-search and code-graph backend.
	KindMCP Kind = "mcp"
	// KindAGUI is `pando agui-serve`, the adapter the agent panel talks to.
	KindAGUI Kind = "agui"
)

// Status is the content of state.json and what Supervisor.Status returns. It
// never carries the token, only the path of the file that does.
type Status struct {
	State State `json:"state"`
	// Kind is what the instance runs; an older state.json has none and means
	// KindMCP.
	Kind Kind `json:"kind,omitempty"`
	// Key, Root and Project identify the instance: the directory name, the
	// repository root and the Pando code-project id.
	Key     string `json:"key"`
	Root    string `json:"root"`
	Project string `json:"project"`
	// PID is the child's pid, 0 when there is no child. Port and MCPURL describe
	// the endpoint the current or last child was told to listen on.
	PID    int    `json:"pid"`
	Port   int    `json:"port"`
	MCPURL string `json:"mcpUrl"`
	// AGUIURL is the base URL of a KindAGUI instance's listener, empty for
	// KindMCP.
	AGUIURL   string `json:"aguiUrl,omitempty"`
	TokenFile string `json:"tokenFile"`
	// Version is the first line of `pando --version`.
	Version string `json:"version"`
	Binary  string `json:"binary"`
	// Since is when State last changed.
	Since time.Time `json:"since"`
	// SupervisorPID is the pid of the gintrack process that holds the lock.
	SupervisorPID int `json:"supervisorPid"`
	// Crashes counts the crashes inside the crash window.
	Crashes int `json:"crashes"`
	// LastError is the reason of the last crash or refusal; LastStderr is the
	// tail of the child's output at that moment, redacted.
	LastError  string   `json:"lastError"`
	LastStderr []string `json:"lastStderr,omitempty"`
}

// InstanceKey names the instance directory for a repository root:
// pando.SanitizeProjectID of the absolute path, then "-" and the first four
// bytes of the path's SHA-256 in hex.
func InstanceKey(repoRoot string) string {
	abs := absPath(repoRoot)
	sum := sha256.Sum256([]byte(abs))
	return pando.SanitizeProjectID(abs) + "-" + hex.EncodeToString(sum[:4])
}

// AGUIKey names the instance directory of the AG-UI adapter of a repository:
// InstanceKey plus "-agui", so it sits next to the MCP instance and never
// shares its lock, state file, token or data directory.
func AGUIKey(repoRoot string) string { return InstanceKey(repoRoot) + "-agui" }

// InstanceDir is where an instance's files live: <cacheDir>/pando/<key>.
func InstanceDir(cacheDir, key string) string {
	return filepath.Join(cacheDir, "pando", key)
}

// ReadStatus reads state.json of an instance directory. It is what a process
// that only connects (gintrack mcp, gintrack spec) uses; the caller must still
// check that PID is alive and that Health passes, because the file outlives a
// crashed supervisor.
func ReadStatus(dir string) (Status, error) {
	b, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		return Status{}, fmt.Errorf("read %s: %w", stateFileName, err)
	}
	var st Status
	if err := json.Unmarshal(b, &st); err != nil {
		return Status{}, fmt.Errorf("parse %s: %w", stateFileName, err)
	}
	return st, nil
}

// ReadToken reads the bearer token of an instance directory.
func ReadToken(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, tokenFileName))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", tokenFileName, err)
	}
	return strings.TrimSpace(string(b)), nil
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate a token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// writeFileAtomic writes data to path with the given mode through a temporary
// file in the same directory, so a reader never sees a torn file and the mode
// is right from the first byte.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create a temporary file: %w", err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// generatedConfig renders the .pando.toml ADR-039 specifies. The token is in
// it, which is why the file is 0600.
func generatedConfig(o Options, dir string, port int, token string) []byte {
	var b strings.Builder
	q := tomlQuote
	off := func(section string) {
		b.WriteString("\n[" + section + "]\nEnabled = false\n")
	}
	b.WriteString("# Generated by gintrack for a managed Pando instance. Rewritten on every start.\n")
	b.WriteString("\n[Data]\nDirectory = " + q(filepath.Join(dir, dataDirName)) + "\n")
	b.WriteString("\n[Remembrances]\nEnabled = true\nKBPath = " + q(filepath.Join(o.RepoRoot, o.DocsFolder)) + "\nKBAutoImport = true\nKBWatch = true\n")
	b.WriteString("\n[TokenOptimization]\nBuildCodeGraph = true\n")
	b.WriteString("\n[MCPServer]\nHttpEnabled = true\nStdioEnabled = false\nHttpHost = \"127.0.0.1\"\n")
	b.WriteString("HttpPort = " + strconv.Itoa(port) + "\nHttpToken = " + q(token) + "\nHttpAllowedOrigins = []\n")
	for _, s := range []string{"FileTools", "SystemExecution", "GatewayExpose", "Design", "SelfImprovement"} {
		off("MCPServer." + s)
	}
	off("Mesnada")
	off("APIServer")
	off("Telemetry")
	return []byte(b.String())
}

// tomlQuote returns s as a TOML basic string.
func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// errNoVersion is returned when `pando --version` prints no semantic version.
var errNoVersion = errors.New("pando --version printed no version")

// parseVersion extracts the first x.y.z from the output of `pando --version`.
func parseVersion(out string) (v [3]int, line string, err error) {
	line = strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
	for _, f := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '(' || r == ')' || r == ',' }) {
		f = strings.TrimPrefix(f, "v")
		if i := strings.IndexAny(f, "-+"); i >= 0 {
			f = f[:i]
		}
		parts := strings.Split(f, ".")
		if len(parts) != 3 {
			continue
		}
		ok := true
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				ok = false
				break
			}
			v[i] = n
		}
		if ok {
			return v, line, nil
		}
	}
	return v, line, errNoVersion
}

func versionLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func marshalStatus(st Status) ([]byte, error) {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal the status: %w", err)
	}
	return append(b, '\n'), nil
}

// DataDir is where Pando keeps the index and the knowledge base of an
// instance directory; `gintrack pando reset` deletes it.
func DataDir(dir string) string { return filepath.Join(dir, dataDirName) }
