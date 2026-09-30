// Command fakepando stands in for `pando` in the supervisor tests. It reads the
// generated .pando.toml from its working directory the way Pando does, and
// serves a minimal streamable-HTTP MCP endpoint behind the bearer token found
// there. FAKE_PANDO_* environment variables select a misbehaviour:
//
//	FAKE_PANDO_MODE       ok (default) | shift | hang | crash | crashafter
//	FAKE_PANDO_CRASH_RUNS crash on the first N starts, then behave (needs FAKE_PANDO_COUNTER)
//	FAKE_PANDO_COUNTER    a file that counts starts across restarts
//	FAKE_PANDO_AFTER      duration before a crashafter exit
//	FAKE_PANDO_IGNORE_TERM=1  ignore SIGTERM
//	FAKE_PANDO_VERSION    what --version prints (default "pando v1.1.1")
//	FAKE_PANDO_RECORD     a file that receives one JSON line per start
//
// `agui-serve --port N --token-file F` (GIT-US-0185) serves the AG-UI routes
// GET /api/v1/agui/healthz (open) and /info (bearer) instead; the same modes
// and variables apply. FAKE_PANDO_AGUI_NO_PORT=1 makes it ignore --port and
// listen elsewhere, the way a port that was taken looks from outside.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		v := os.Getenv("FAKE_PANDO_VERSION")
		if v == "" {
			v = "pando v1.1.1"
		}
		fmt.Println(v)
		return
	}
	cwd, _ := os.Getwd()
	cfg, _ := os.ReadFile(".pando.toml")
	port, _ := strconv.Atoi(find(cfg, `HttpPort\s*=\s*(\d+)`))
	token := find(cfg, `HttpToken\s*=\s*"([^"]*)"`)
	agui := len(args) > 0 && args[0] == "agui-serve"
	if agui {
		port, _ = strconv.Atoi(flagValue(args, "--port"))
		b, _ := os.ReadFile(flagValue(args, "--token-file"))
		token = strings.TrimSpace(string(b))
	}

	record(map[string]any{
		"args": args, "cwd": cwd, "pid": os.Getpid(), "config": string(cfg),
		"parentSearch": os.Getenv("PANDO_CONFIG_PARENT_SEARCH"), "port": port,
	})
	// A well-behaved Pando never prints its token; this one does, so the tests
	// can check the redaction of the log and of state.json.
	fmt.Fprintln(os.Stderr, "starting; Authorization: Bearer", token, "token="+token)

	if os.Getenv("FAKE_PANDO_IGNORE_TERM") == "1" {
		signal.Ignore(syscall.SIGTERM)
	}
	mode := os.Getenv("FAKE_PANDO_MODE")
	if n, _ := strconv.Atoi(os.Getenv("FAKE_PANDO_CRASH_RUNS")); n > 0 && count() <= n {
		mode = "crash"
	}
	switch mode {
	case "crash":
		fmt.Fprintln(os.Stderr, "fatal: simulated crash")
		os.Exit(3)
	case "hang":
		select {}
	case "shift":
		// Pando's chooseAvailablePort: the requested port is "busy", so it
		// listens somewhere else and only logs a warning.
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		fmt.Fprintln(os.Stderr, "warning: port busy, using", l.Addr())
		serve(l, token, agui)
		return
	}
	if agui && os.Getenv("FAKE_PANDO_AGUI_NO_PORT") == "1" {
		port = 0
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	if mode == "crashafter" {
		d, _ := time.ParseDuration(os.Getenv("FAKE_PANDO_AFTER"))
		go func() { time.Sleep(d); fmt.Fprintln(os.Stderr, "fatal: crash after uptime"); os.Exit(1) }()
	}
	serve(l, token, agui)
}

// flagValue returns the value after name in args, "" when absent.
func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func serveAGUI(l net.Listener, token string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agui/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/v1/agui/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"agents":[]}`))
	})
	_ = http.Serve(l, mux)
}

func serve(l net.Listener, token string, agui bool) {
	if agui {
		serveAGUI(l, token)
		return
	}
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fakepando", Version: "test"}, nil)
	srv.AddTool(&mcpsdk.Tool{Name: "code_list_projects", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "[]"}}}, nil
		})
	mcp := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcp.ServeHTTP(w, r)
	})
	_ = http.Serve(l, mux)
}

func find(b []byte, re string) string {
	m := regexp.MustCompile(re).FindSubmatch(b)
	if m == nil {
		return ""
	}
	return string(m[1])
}

func record(v map[string]any) {
	path := os.Getenv("FAKE_PANDO_RECORD")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(v)
}

// count increments the start counter and returns the new value.
func count() int {
	path := os.Getenv("FAKE_PANDO_COUNTER")
	if path == "" {
		return 0
	}
	b, _ := os.ReadFile(path)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	n++
	_ = os.WriteFile(path, []byte(strconv.Itoa(n)), 0o600)
	return n
}
