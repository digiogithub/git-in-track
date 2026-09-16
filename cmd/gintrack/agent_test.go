package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
)

// fakePandoScript is a stand-in for `pando secret <value>`: it prints the
// `age1:` prefix Pando's loader looks for followed by base64 of the value, so a
// test can assert the ciphertext is really the token and never the token
// itself. It records its arguments so the --age-keys forwarding can be checked.
const fakePandoScript = `#!/bin/sh
printf '%s\n' "$*" > "$FAKE_PANDO_ARGS"
if [ "$1" != secret ]; then
  echo "unexpected command: $*" >&2
  exit 2
fi
printf 'age1:'
printf '%s' "$2" | base64
`

// writeFakePando writes a script named `pando` into a fresh directory without
// touching PATH, and returns the script and the file its arguments land in.
func writeFakePando(t *testing.T, script string) (binary, args string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake pando is a shell script")
	}
	dir := t.TempDir()
	binary = filepath.Join(dir, "pando")
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatalf("write the fake pando: %v", err)
	}
	args = filepath.Join(dir, "args")
	t.Setenv("FAKE_PANDO_ARGS", args)
	return binary, args
}

// installFakePando additionally puts the fake first on PATH, so that the
// command finds it the way it would find a real Pando installation.
func installFakePando(t *testing.T, script string) string {
	t.Helper()
	binary, args := writeFakePando(t, script)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return args
}

// emptyPATH removes every directory from PATH so that no pando can be found.
// The fake pando is never reached through this PATH, so nothing needs the
// shell utilities it hides.
func emptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

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
	installFakePando(t, fakePandoScript)

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
		"[ToolDiscovery]",
		"Mode = 'off'",
		"[MCPGateway]",
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
		"\n[MCPServers.gintrack.Auth]",
		"Type = 'bearer'",
		"Token = 'age1:",
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
		"list_items", "get_item", "search_items", "search_semantic",
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

// TestAgentInitKeepsTheSecretOutOfTheOtherFiles is the "no secret in the
// generated file" criterion of GIT-T-0100: no generated file carries the clear
// token, .pando.toml holds only the age ciphertext, and it is mode 0600.
func TestAgentInitKeepsTheSecretOutOfTheOtherFiles(t *testing.T) {
	h := newHarness(t)
	h.register()
	t.Setenv("GINTRACK_TOKEN", "companion-secret")
	installFakePando(t, fakePandoScript)

	h.mustRun("agent", "init", "--repo", h.Repo)

	for _, name := range []string{agentPersonaName, agentSkillName, agentPandoConfigName} {
		if strings.Contains(readGenerated(t, h.Repo, name), "companion-secret") {
			t.Errorf("%s carries the companion token in clear", name)
		}
	}
	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	if strings.Contains(toml, "\n[MCPServers.gintrack.Headers]") {
		t.Errorf("the plaintext Headers block was written:\n%s", toml)
	}
	if strings.Contains(toml, "\nAuthorization = ") {
		t.Error("a literal Authorization header was written")
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
	if !strings.Contains(toml, "# [MCPServers.gintrack.Auth]") {
		t.Errorf("the commented-out Auth block is missing:\n%s", toml)
	}
	if !strings.Contains(toml, "pando secret") {
		t.Error("the comment does not say how to produce the ciphertext by hand")
	}
	if !strings.Contains(stdout, "No companion token is configured") {
		t.Errorf("the missing token was not reported:\n%s", stdout)
	}
}

// TestAgentInitEncryptsTheCompanionToken checks the whole encryption path: the
// ciphertext in the file is what `pando secret` returned, --age-keys reaches
// the subprocess, and --json reports the token as encrypted.
func TestAgentInitEncryptsTheCompanionToken(t *testing.T) {
	h := newHarness(t)
	h.register()
	t.Setenv("GINTRACK_TOKEN", "companion-secret")
	argsFile := installFakePando(t, fakePandoScript)

	stdout := h.mustRun("agent", "init", "--repo", h.Repo, "--age-keys", "work", "--json")

	want := "age1:" + base64.StdEncoding.EncodeToString([]byte("companion-secret"))
	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	if !strings.Contains(toml, "Token = '"+want+"'") {
		t.Errorf("the ciphertext is not %q:\n%s", want, toml)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read the recorded arguments: %v", err)
	}
	if !strings.Contains(string(args), "--age-keys work") {
		t.Errorf("--age-keys was not forwarded: %q", args)
	}
	if !strings.HasPrefix(string(args), "secret ") {
		t.Errorf("pando was not called with `secret`: %q", args)
	}

	payload := decode[agentInitPayload](t, stdout)
	if !payload.Encrypted {
		t.Error("--json does not report tokenEncrypted: true")
	}
	if !payload.HasToken {
		t.Error("--json does not report the companion token")
	}
	if strings.Contains(stdout, "companion-secret") || strings.Contains(stdout, want) {
		t.Errorf("stdout carries the token or the ciphertext:\n%s", stdout)
	}
}

// TestAgentInitPandoFlag points the command at a binary that is not on PATH.
func TestAgentInitPandoFlag(t *testing.T) {
	h := newHarness(t)
	h.register()
	t.Setenv("GINTRACK_TOKEN", "companion-secret")
	// The fake is deliberately NOT on PATH: only --pando can find it.
	binary, _ := writeFakePando(t, fakePandoScript)

	h.mustRun("agent", "init", "--repo", h.Repo, "--pando", binary)

	if !strings.Contains(readGenerated(t, h.Repo, agentPandoConfigName), "Token = 'age1:") {
		t.Error("--pando did not encrypt the token")
	}
}

// TestAgentInitRefusesWithoutPando is the refusal half of GIT-T-0100: with no
// pando to encrypt with, nothing is written at all.
func TestAgentInitRefusesWithoutPando(t *testing.T) {
	h := newHarness(t)
	h.register()
	t.Setenv("GINTRACK_TOKEN", "companion-secret")
	emptyPATH(t)

	_, stderr, code := h.run("agent", "init", "--repo", h.Repo)

	if code != exitConflict {
		t.Errorf("exit %d, want %d", code, exitConflict)
	}
	if !strings.Contains(stderr, "--plaintext-token") {
		t.Errorf("the refusal does not offer the escape hatch:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(h.Repo, agentPandoConfigName)); err == nil {
		t.Error("the refused run still wrote the configuration")
	}
}

// TestAgentInitRefusesABadSecretCommand covers the two ways `pando secret` can
// let us down: a non-zero exit, and output that is not an age ciphertext.
func TestAgentInitRefusesABadSecretCommand(t *testing.T) {
	cases := map[string]string{
		"failure":       "#!/bin/sh\necho 'no age key' >&2\nexit 1\n",
		"clear echo":    "#!/bin/sh\nprintf '%s\\n' \"$2\"\n",
		"empty output":  "#!/bin/sh\nexit 0\n",
		"not a warning": "#!/bin/sh\necho 'age1 without the colon'\n",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.register()
			t.Setenv("GINTRACK_TOKEN", "companion-secret")
			installFakePando(t, script)

			_, stderr, code := h.run("agent", "init", "--repo", h.Repo)

			if code != exitConflict {
				t.Errorf("exit %d, want %d", code, exitConflict)
			}
			if strings.Contains(stderr, "companion-secret") {
				t.Errorf("the refusal leaked the token:\n%s", stderr)
			}
			if _, err := os.Stat(filepath.Join(h.Repo, agentPandoConfigName)); err == nil {
				t.Error("the refused run still wrote the configuration")
			}
		})
	}
}

// TestAgentInitPlaintextToken is the escape hatch: the old Headers form, still
// 0600, still with the banner and a warning on stdout.
func TestAgentInitPlaintextToken(t *testing.T) {
	h := newHarness(t)
	h.register()
	t.Setenv("GINTRACK_TOKEN", "companion-secret")
	emptyPATH(t)

	stdout := h.mustRun("agent", "init", "--repo", h.Repo, "--plaintext-token", "--json")

	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	for _, want := range []string{
		"[MCPServers.gintrack.Headers]",
		"Authorization = 'Bearer companion-secret'",
		"SECURITY",
		"--plaintext-token",
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("%s does not contain %q", agentPandoConfigName, want)
		}
	}
	if strings.Contains(toml, "\n[MCPServers.gintrack.Auth]") {
		t.Error("--plaintext-token still wrote an Auth block")
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
	payload := decode[agentInitPayload](t, stdout)
	if payload.Encrypted {
		t.Error("--json reports tokenEncrypted: true for a plaintext token")
	}
	if !payload.HasToken {
		t.Error("--json does not report the companion token")
	}

	if _, warning, _ := h.run("agent", "init", "--repo", h.Repo, "--plaintext-token", "--force"); strings.Contains(warning, "companion-secret") {
		t.Error("the token reached stderr")
	}
	text := h.mustRun("agent", "init", "--repo", h.Repo, "--plaintext-token", "--force")
	if !strings.Contains(text, "WARNING") {
		t.Errorf("the plaintext run printed no warning:\n%s", text)
	}
	if strings.Contains(text, "companion-secret") {
		t.Errorf("the token was printed to stdout:\n%s", text)
	}
}

// TestAgentInitSkipsTheFilesItWouldOverwrite is the re-run criterion: a re-run
// is the normal way to pick up a template or a token change, so nothing in the
// way is an error any more. The configuration is merged into, the persona and
// the skill — gintrack's own files, which the user may have edited — are left
// exactly as they are and named on stdout together with the way to replace
// them, and the run exits 0.
func TestAgentInitSkipsTheFilesItWouldOverwrite(t *testing.T) {
	h := newHarness(t)
	h.register()
	h.mustRun("agent", "init", "--repo", h.Repo)

	marker := "# edited by hand\n"
	for _, name := range []string{agentPersonaName, agentSkillName} {
		if err := os.WriteFile(filepath.Join(h.Repo, filepath.FromSlash(name)), []byte(marker), 0o644); err != nil {
			t.Fatalf("edit %s: %v", name, err)
		}
	}

	stdout, stderr, code := h.run("agent", "init", "--repo", h.Repo, "--companion-url", "http://127.0.0.1:9999")
	if code != exitOK {
		t.Fatalf("exit %d, want %d\n%s", code, exitOK, stderr)
	}
	for _, name := range []string{agentPersonaName, agentSkillName} {
		if !strings.Contains(stdout, name) {
			t.Errorf("the report does not name the skipped %s:\n%s", name, stdout)
		}
		if got := readGenerated(t, h.Repo, name); got != marker {
			t.Errorf("the re-run rewrote %s:\n%s", name, got)
		}
	}
	if !strings.Contains(stdout, "--force") {
		t.Errorf("the report does not say how to overwrite them:\n%s", stdout)
	}
	// The whole point of letting the run continue: the configuration picks the
	// change up.
	if !strings.Contains(readGenerated(t, h.Repo, agentPandoConfigName), "URL = 'http://127.0.0.1:9999/mcp'") {
		t.Error("the re-run did not merge the new companion URL into the configuration")
	}
}

// TestAgentInitForceOverwrites completes the previous test: --force is the only
// way to replace the files wholesale, and it restores the mode as well.
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
