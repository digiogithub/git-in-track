package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
)

// readGenerated reads one generated file and fails the test when it is missing.
func readGenerated(t *testing.T, repo, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

// TestAgentInitWritesThePandoConfiguration covers the generation half of
// GIT-T-0100 and GIT-T-0106: the three files exist, and the configuration
// carries the decisions the story fixed.
func TestAgentInitWritesThePandoConfiguration(t *testing.T) {
	h := newHarness(t)
	h.register()
	t.Setenv("GINTRACK_TOKEN", "companion-secret")

	stdout := h.mustRun("agent", "init", "--repo", h.Repo)

	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	for _, want := range []string{
		"[AGUI]",
		"Enabled = true",
		"Path = '/api/v1/agui'",
		"Agents = ['coder']",
		"AllowedOrigins = []",
		"RequireToken = true",
		"FrontendTools = true",
		"HumanInTheLoop = true",
		"AutoApprove = false",
		"MaxConcurrentRuns = 4",
		"Persona = 'backlog-assistant'",
		"'gintrack_*'",
		"'kb_search_documents'",
		"'code_hybrid_search'",
		"Mesnada = false",
		"[AGUI.Profiles.backlog-assistant]",
		"Base = 'coder'",
		"[MCPServers.gintrack]",
		"Type = 'streamable-http'",
		"URL = 'http://127.0.0.1:7317/mcp'",
		"Authorization = 'Bearer companion-secret'",
		"[Remembrances]",
		"KBAutoImport = true",
		"KBWatch = false",
		"[MCPServer]",
		"HttpEnabled = false",
		":9777",
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("%s does not contain %q", agentPandoConfigName, want)
		}
	}

	// The two comments the story requires as documentation of why.
	if !strings.Contains(toml, "strips the browser") {
		t.Error("the AllowedOrigins comment does not explain that the proxy strips Origin")
	}
	if !strings.Contains(toml, "strips the metadata") {
		t.Error("the KBWatch comment does not explain that the watcher erases front matter")
	}

	persona := readGenerated(t, h.Repo, agentPersonaName)
	if !strings.HasPrefix(persona, "---\nname: backlog-assistant\n") {
		t.Errorf("the persona has no front matter: %.60q", persona)
	}
	for _, want := range []string{"data, not instructions", "rev", `rev: "*"`, "cite"} {
		if !strings.Contains(strings.ToLower(persona), strings.ToLower(want)) {
			t.Errorf("the persona does not mention %q", want)
		}
	}

	skill := readGenerated(t, h.Repo, agentSkillName)
	if !strings.HasPrefix(skill, "---\nname: gintrack-search\n") {
		t.Errorf("the skill has no front matter: %.60q", skill)
	}
	for _, want := range []string{
		"list_items", "get_item", "search_items",
		"kb_search_documents", "path_prefix", "code_hybrid_search",
		"hybrid_search_remembrances",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("the routing table does not mention %q", want)
		}
	}

	// The next two commands, and no token value anywhere on stdout.
	for _, want := range []string{"pando agui-serve", "--no-tls", "--token-file", "gintrack serve --agent --mcp-http"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not print %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "companion-secret") {
		t.Error("the companion token was printed to stdout")
	}
}

// TestAgentInitKeepsTheSecretOutOfTheOtherFiles is the "no secret" criterion of
// GIT-T-0100: only .pando.toml may carry the bearer token, it is mode 0600, and
// the persona and the skill never see it.
func TestAgentInitKeepsTheSecretOutOfTheOtherFiles(t *testing.T) {
	h := newHarness(t)
	h.register()
	t.Setenv("GINTRACK_TOKEN", "companion-secret")

	h.mustRun("agent", "init", "--repo", h.Repo)

	for _, name := range []string{agentPersonaName, agentSkillName} {
		if strings.Contains(readGenerated(t, h.Repo, name), "companion-secret") {
			t.Errorf("%s carries the companion token", name)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(h.Repo, agentPandoConfigName))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s is mode %o, want 600", agentPandoConfigName, got)
		}
	}
}

// TestAgentInitWithoutACompanionToken writes no Authorization header at all
// rather than an empty or placeholder secret.
func TestAgentInitWithoutACompanionToken(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("agent", "init", "--repo", h.Repo)

	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	if strings.Contains(toml, "\nAuthorization = ") {
		t.Errorf("an Authorization header was written with no token configured:\n%s", toml)
	}
	if !strings.Contains(toml, "# Authorization = 'Bearer <companion token>'") {
		t.Error("the commented-out header is missing")
	}
	if !strings.Contains(stdout, "No companion token is configured") {
		t.Errorf("the missing token was not reported:\n%s", stdout)
	}
}

// TestAgentInitRefusesToOverwrite is the re-run criterion: non-zero exit,
// nothing changed, and the message names the file that is in the way.
func TestAgentInitRefusesToOverwrite(t *testing.T) {
	h := newHarness(t)
	h.register()
	h.mustRun("agent", "init", "--repo", h.Repo)

	marker := "# edited by hand\n"
	target := filepath.Join(h.Repo, agentPandoConfigName)
	if err := os.WriteFile(target, []byte(marker), 0o600); err != nil {
		t.Fatalf("edit: %v", err)
	}

	_, stderr, code := h.run("agent", "init", "--repo", h.Repo)
	if code == exitOK {
		t.Fatal("a second run without --force succeeded")
	}
	if code != exitConflict {
		t.Errorf("exit %d, want %d", code, exitConflict)
	}
	if !strings.Contains(stderr, agentPandoConfigName) {
		t.Errorf("the refusal does not name the file:\n%s", stderr)
	}
	if got := readGenerated(t, h.Repo, agentPandoConfigName); got != marker {
		t.Errorf("the refused run rewrote the file:\n%s", got)
	}
}

// TestAgentInitForceOverwrites completes the previous test: --force is the only
// way to replace the files, and it restores the mode as well.
func TestAgentInitForceOverwrites(t *testing.T) {
	h := newHarness(t)
	h.register()
	h.mustRun("agent", "init", "--repo", h.Repo)

	target := filepath.Join(h.Repo, agentPandoConfigName)
	if err := os.WriteFile(target, []byte("# edited\n"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}

	h.mustRun("agent", "init", "--repo", h.Repo, "--force")

	if !strings.Contains(readGenerated(t, h.Repo, agentPandoConfigName), "[AGUI]") {
		t.Error("--force did not regenerate the configuration")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("--force left mode %o, want 600", got)
		}
	}
}

// TestAgentInitCorpusPathAndFlags checks the two values the companion and the
// exporter must agree on: the corpus directory and the AG-UI port.
func TestAgentInitCorpusPathAndFlags(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("agent", "init", "--repo", h.Repo,
		"--companion-url", "http://127.0.0.1:9999/", "--agui-port", "18090")

	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	if !strings.Contains(toml, "URL = 'http://127.0.0.1:9999/mcp'") {
		t.Error("--companion-url was not used for the MCP endpoint")
	}
	if !strings.Contains(toml, "Port = 18090") {
		t.Error("--agui-port was not used for the AG-UI listener")
	}
	want := filepath.Join(filepath.Dir(h.Config), "pando-kb", "acme-api")
	if !strings.Contains(toml, "KBPath = '"+want+"'") {
		t.Errorf("KBPath is not %q:\n%s", want, toml)
	}
	if strings.Contains(toml, "KBPath = '"+h.Repo) {
		t.Error("KBPath points inside the repository")
	}
	if !strings.Contains(stdout, "--port 18090") {
		t.Errorf("the printed agui-serve command does not carry the port:\n%s", stdout)
	}
}

// TestAgentInitRejectsABadPort keeps the flag validation on the usage exit code.
func TestAgentInitRejectsABadPort(t *testing.T) {
	h := newHarness(t)
	h.register()

	if _, _, code := h.run("agent", "init", "--repo", h.Repo, "--agui-port", "0"); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
	if _, err := os.Stat(filepath.Join(h.Repo, agentPandoConfigName)); err == nil {
		t.Error("a rejected invocation still wrote the configuration")
	}
}

// TestAgentCompanionURL covers the address the generated file points Pando at.
func TestAgentCompanionURL(t *testing.T) {
	cases := []struct {
		name string
		flag string
		bind string
		port int
		want string
	}{
		{"defaults", "", "", 0, "http://127.0.0.1:7317"},
		{"configured", "", "127.0.0.1", 9000, "http://127.0.0.1:9000"},
		{"wildcard becomes loopback", "", "0.0.0.0", 7317, "http://127.0.0.1:7317"},
		{"ipv6 wildcard becomes loopback", "", "::", 7317, "http://127.0.0.1:7317"},
		{"ipv6 literal is bracketed", "", "::1", 7317, "http://[::1]:7317"},
		{"flag wins and loses its slash", "http://host:1/", "0.0.0.0", 7317, "http://host:1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.Bind, cfg.Server.Port = tc.bind, tc.port
			got, err := agentCompanionURL(tc.flag, cfg)
			if err != nil {
				t.Fatalf("agentCompanionURL: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	if _, err := agentCompanionURL("127.0.0.1:7317", &config.Config{}); err == nil {
		t.Error("a scheme-less --companion-url was accepted")
	}
}

// TestTomlString covers the quoting the templates rely on.
func TestTomlString(t *testing.T) {
	cases := map[string]string{
		"plain":          "'plain'",
		"/api/v1/agui":   "'/api/v1/agui'",
		`it's`:           `"it's"`,
		`back\slash`:     `"back\\slash"`,
		"line\nbreak":    `"line\nbreak"`,
		`quote"and'both`: `"quote\"and'both"`,
	}
	for in, want := range cases {
		if got := tomlString(in); got != want {
			t.Errorf("tomlString(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestAgentTokenFileIsStableAndPrivate checks that the AG-UI token is generated
// once, outside the repository, and never regenerated behind the user's back.
func TestAgentTokenFileIsStableAndPrivate(t *testing.T) {
	h := newHarness(t)
	h.register()

	h.mustRun("agent", "init", "--repo", h.Repo)
	path := filepath.Join(filepath.Dir(h.Config), "agui", "acme-api.token")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the token file: %v", err)
	}
	if len(strings.TrimSpace(string(first))) < 32 {
		t.Errorf("the generated token is too short: %q", first)
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat: %v", statErr)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("the token file is mode %o, want 600", got)
		}
	}

	h.mustRun("agent", "init", "--repo", h.Repo, "--force")
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read the token file: %v", err)
	}
	if string(first) != string(second) {
		t.Error("a second run replaced the AG-UI token")
	}
	if strings.Contains(readGenerated(t, h.Repo, agentPandoConfigName), strings.TrimSpace(string(first))) {
		t.Error("the AG-UI token was written into the repository")
	}
}
