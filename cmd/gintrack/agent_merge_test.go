package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// existingPandoConfig is a Pando configuration shaped like a real one: Pando
// dumps every key it knows with its current value, so the file has root-level
// keys before the first table, tables gintrack never touches, an array of
// tables, a section whose keys are lower-cased, and — crucially — settings of
// the user's inside the tables gintrack writes into. `[ToolDiscovery]` and
// `[MCPGateway]` are on here exactly as they are in this repository's own
// configuration, where they are what make `gintrack_*` calls work at all.
const existingPandoConfig = `WorkingDir = ''
Debug = false
ContextPaths = []
AutoCompact = true

[Data]
Directory = './.pando/data'

[MCPServers]

[Agents.coder]
Model = 'copilot.gpt-5.6-luna'
MaxTokens = 0

[Skills]
Enabled = true
Paths = ['./agents/skills']

[evaluator]
enabled = false
model = 'copilot.gpt-5-mini'
maxSkills = 100

[[evaluator.taskPatterns]]
pattern = 'fix|bug|error|crash'
taskType = 'debug'

[[evaluator.taskPatterns]]
pattern = 'test|spec|coverage'
taskType = 'test'

[MCPGateway]
Enabled = true
FavoriteThreshold = 3

[ToolDiscovery]
Enabled = true
Mode = 'auto'
MaxDirectTools = 64

[Remembrances]
Enabled = true
KBPath = '/www/example/docs'
KBAutoImport = true
ChunkSize = 800
MemoryPinnedScopes = []

[MCPServer]
Enabled = false
HttpPort = 0
StdioEnabled = false

[AGUI]
Enabled = false
Path = ''
Port = 0
AgentPoolTTL = ''
AutoApprove = false
AllowedOrigins = []

[PersonaAutoSelect]
Enabled = false
PersonaPath = ''

[Extensions.Memory]
Enabled = false
Paths = []
`

// writeExistingPandoConfig puts the fixture configuration in a repository, the
// way a user who already runs Pando there would have it.
func writeExistingPandoConfig(t *testing.T, repo string) string {
	t.Helper()
	path := filepath.Join(repo, agentPandoConfigName)
	if err := os.WriteFile(path, []byte(existingPandoConfig), 0o600); err != nil {
		t.Fatalf("write %s: %v", agentPandoConfigName, err)
	}
	return path
}

// findBackup returns the single backup copy the merge left next to the file.
func findBackup(t *testing.T, repo string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(repo, agentPandoConfigName+".*.bak"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly one backup, got %v", matches)
	}
	return matches[0]
}

// TestAgentInitMergesIntoAnExistingPandoConfig is the headline of GIT-T-0227: a
// repository that already has a Pando configuration keeps it, the run succeeds,
// and everything gintrack needs is in the file afterwards — comments included.
func TestAgentInitMergesIntoAnExistingPandoConfig(t *testing.T) {
	h := newHarness(t)
	h.register()
	t.Setenv("GINTRACK_TOKEN", "companion-secret")
	installFakePando(t, fakePandoScript)

	writeExistingPandoConfig(t, h.Repo)
	stdout := h.mustRun("agent", "init", "--repo", h.Repo)

	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	for _, want := range []string{
		// The tables the file did not have, inserted whole.
		"[AGUI.Profiles.backlog-assistant]",
		"Base = 'coder'",
		"[MCPServers.gintrack]",
		"Type = 'streamable-http'",
		"URL = 'http://127.0.0.1:7317/mcp'",
		"\n[MCPServers.gintrack.Auth]",
		"Token = 'age1:",
		// The keys the shared tables did not have, added with their comments.
		"MaxConcurrentRuns = 4",
		"KBWatch = true",
		"HttpEnabled = false",
		"Persona = 'backlog-assistant'",
		"'gintrack_*'",
		"Mesnada = false",
		// The comments that come with the blocks and keys it inserts. (The
		// "strips the browser" one rides on AllowedOrigins, which this fixture
		// already has at the value the template wants, so it is not inserted
		// here; TestAgentInitWritesThePandoConfiguration covers it.)
		"performs no file write",
		":9777",
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("the merged %s does not contain %q", agentPandoConfigName, want)
		}
	}

	backup := findBackup(t, h.Repo)
	if !strings.Contains(stdout, backup) {
		t.Errorf("the report does not print the backup path %q:\n%s", backup, stdout)
	}
	if got, err := os.ReadFile(backup); err != nil || string(got) != existingPandoConfig {
		t.Errorf("the backup is not the previous version (%v)", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(h.Repo, agentPandoConfigName))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("the merged file has mode %o, want 600", got)
		}
	}
}

// TestAgentInitMergeKeepsEverythingItDoesNotOwn is the preservation guarantee:
// every line the file had is still there, in the same order, byte for byte —
// apart from the [AGUI] zero values, which count as unset. A merge may only add
// and, in that one table, fill in.
func TestAgentInitMergeKeepsEverythingItDoesNotOwn(t *testing.T) {
	h := newHarness(t)
	h.register()
	writeExistingPandoConfig(t, h.Repo)

	h.mustRun("agent", "init", "--repo", h.Repo)

	// The one documented exception: inside [AGUI] a key left at its zero value
	// counts as unset, so these three are rewritten in place rather than kept.
	rewritten := map[string]bool{"Enabled = false": true, "Path = ''": true, "Port = 0": true}

	merged := strings.Split(readGenerated(t, h.Repo, agentPandoConfigName), "\n")
	at := 0
	for _, line := range strings.Split(existingPandoConfig, "\n") {
		if rewritten[line] {
			continue
		}
		found := false
		for ; at < len(merged); at++ {
			if merged[at] == line {
				at++
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("the merge dropped or reordered %q", line)
		}
	}
}

// TestAgentInitMergeReportsDivergingKeys is the conflict policy: a key gintrack
// would set that the file already has with another value is left alone and
// reported with the recommended value and the reason. Overwriting them would
// break a working setup — with the MCP gateway off, Pando v0.705.1 deadlocks.
func TestAgentInitMergeReportsDivergingKeys(t *testing.T) {
	h := newHarness(t)
	h.register()
	writeExistingPandoConfig(t, h.Repo)

	stdout := h.mustRun("agent", "init", "--repo", h.Repo)

	for _, want := range []string{
		"[ToolDiscovery] Enabled: kept Enabled = true, recommended Enabled = false",
		"[ToolDiscovery] Mode: kept Mode = 'auto', recommended Mode = 'off'",
		"[MCPGateway] Enabled: kept Enabled = true, recommended Enabled = false",
		"[Remembrances] KBPath: kept KBPath = '/www/example/docs'",
		"[MCPServer] StdioEnabled: kept StdioEnabled = false",
		"tool_search",
		// The KBPath reason of GIT-EP-0020: a path outside the repository
		// indexes a copy nobody edits, and a root would be indexed whole.
		"no exclusions at all",
		"would be indexed whole",
		"indexes a copy nobody edits",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the divergence report does not mention %q:\n%s", want, stdout)
		}
	}

	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	for _, want := range []string{
		"Mode = 'auto'",
		"KBPath = '/www/example/docs'",
		"StdioEnabled = false",
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("the merge overwrote a user value: %q is gone", want)
		}
	}
	// [Skills] already agreed with the template, so it is not a divergence.
	if strings.Contains(stdout, "[Skills]") {
		t.Errorf("a key that already matched was reported:\n%s", stdout)
	}
}

// TestAgentInitMergeAcceptsAWatcherAlreadyOn is the other half of the KBWatch
// reversal: `KBWatch = true` is now the generated value and Pando's own
// default, so a user who already has it must not be reported as diverging.
func TestAgentInitMergeAcceptsAWatcherAlreadyOn(t *testing.T) {
	h := newHarness(t)
	h.register()
	path := writeExistingPandoConfig(t, h.Repo)
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	updated := strings.Replace(string(current), "KBAutoImport = true", "KBAutoImport = true\nKBWatch = true", 1)
	if updated == string(current) {
		t.Fatal("the fixture no longer has KBAutoImport in [Remembrances]")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout := h.mustRun("agent", "init", "--repo", h.Repo)

	if strings.Contains(stdout, "KBWatch") {
		t.Errorf("a KBWatch already at the generated value was reported as a divergence:\n%s", stdout)
	}
	if !strings.Contains(readGenerated(t, h.Repo, agentPandoConfigName), "KBWatch = true") {
		t.Error("the merged configuration lost KBWatch = true")
	}
}

// TestAgentInitMergeSwapsTheAuthBranch covers the three mutually exclusive
// [MCPServers.gintrack] auth branches: a re-run that changes the token mode
// must leave exactly one of them behind, not two.
func TestAgentInitMergeSwapsTheAuthBranch(t *testing.T) {
	base := agentTemplateData{
		AGUIPath: agentDefaultAGUIPath, AGUIHost: "127.0.0.1", AGUIPort: agentDefaultAGUIPort,
		MaxConcurrentRuns: agentDefaultMaxRuns, Persona: agentPersonaID, Tools: agentTools,
		MCPURL: "http://127.0.0.1:7317/mcp", KBPath: "/www/example/repo/docs",
	}
	render := func(t *testing.T, mutate func(*agentTemplateData)) string {
		t.Helper()
		data := base
		mutate(&data)
		out, err := renderAgentTemplate("pando.toml.tmpl", data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		return string(out)
	}

	none := render(t, func(*agentTemplateData) {})
	encrypted := render(t, func(d *agentTemplateData) {
		d.Token, d.Encrypted, d.AuthToken = true, true, "age1:ciphertext"
	})
	plaintext := render(t, func(d *agentTemplateData) {
		d.Token, d.BearerHeader = true, "Bearer companion-secret"
	})

	for _, step := range []struct {
		name     string
		rendered string
		want     string
		gone     []string
	}{
		{"no token to encrypted", encrypted, "[MCPServers.gintrack.Auth]", []string{"\n[MCPServers.gintrack.Headers]", "# [MCPServers.gintrack.Auth]"}},
		{"encrypted to plaintext", plaintext, "[MCPServers.gintrack.Headers]", []string{"\n[MCPServers.gintrack.Auth]", "# [MCPServers.gintrack.Auth]"}},
		{"plaintext back to none", none, "# [MCPServers.gintrack.Auth]", []string{"\n[MCPServers.gintrack.Headers]"}},
	} {
		// Each step merges into the result of the previous one.
		merged, _, err := mergePandoTOML(existingPandoConfig, none, agentPersonaID)
		if err != nil {
			t.Fatalf("seed merge: %v", err)
		}
		for _, rendered := range []string{encrypted, plaintext, step.rendered} {
			merged, _, err = mergePandoTOML(merged, rendered, agentPersonaID)
			if err != nil {
				t.Fatalf("%s: %v", step.name, err)
			}
		}
		if !strings.Contains(merged, step.want) {
			t.Errorf("%s: the merged file has no %q", step.name, step.want)
		}
		for _, gone := range step.gone {
			if strings.Contains(merged, gone) {
				t.Errorf("%s: the merged file still has %q", step.name, gone)
			}
		}
		if n := strings.Count(merged, "\n[MCPServers.gintrack]"); n != 1 {
			t.Errorf("%s: %d [MCPServers.gintrack] tables, want 1", step.name, n)
		}
	}
}

// TestAgentInitMergeFailsOnAnUnparsableLine is the failure path: a line that
// opens a table and cannot be read is refused, nothing is written and the
// message names the line, because guessing would move the user's keys into the
// wrong table.
func TestAgentInitMergeFailsOnAnUnparsableLine(t *testing.T) {
	h := newHarness(t)
	h.register()

	broken := "[Data]\nDirectory = './x'\n\n[Remembrances\nKBWatch = true\n"
	target := filepath.Join(h.Repo, agentPandoConfigName)
	if err := os.WriteFile(target, []byte(broken), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, stderr, code := h.run("agent", "init", "--repo", h.Repo)
	if code != exitConflict {
		t.Fatalf("exit %d, want %d\n%s", code, exitConflict, stderr)
	}
	for _, want := range []string{"line 4", "[Remembrances", "nothing was written"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the failure does not mention %q:\n%s", want, stderr)
		}
	}
	if got := readGenerated(t, h.Repo, agentPandoConfigName); got != broken {
		t.Errorf("the failed merge changed the file:\n%s", got)
	}
	if matches, _ := filepath.Glob(filepath.Join(h.Repo, agentPandoConfigName+".*.bak")); len(matches) != 0 {
		t.Errorf("the failed merge left a backup: %v", matches)
	}
	if _, err := os.Stat(filepath.Join(h.Repo, filepath.FromSlash(agentPersonaName))); err == nil {
		t.Error("the failed merge still wrote the persona")
	}
}

// TestAgentInitMergeFillsInAGUIZeroValues covers the one exception to
// warn-do-not-overwrite. Pando writes `[AGUI]` out with every key at its zero
// value the moment anything touches the section, so `Enabled = false` /
// `Path = ”` / `Port = 0` there is the absence of a choice rather than a
// choice, and only warning about them would leave the adapter off and the panel
// broken. The rule stops at that table: a `false` elsewhere is a setting
// somebody relies on, so `[MCPServer] StdioEnabled = false` must still diverge.
func TestAgentInitMergeFillsInAGUIZeroValues(t *testing.T) {
	h := newHarness(t)
	h.register()
	writeExistingPandoConfig(t, h.Repo)

	stdout := h.mustRun("agent", "init", "--repo", h.Repo)

	toml := readGenerated(t, h.Repo, agentPandoConfigName)
	for _, want := range []string{"Enabled = true", "Path = '/api/v1/agui'", "Port = 8090"} {
		if !strings.Contains(toml, want) {
			t.Errorf("an [AGUI] zero value was not filled in: %q is missing", want)
		}
	}
	for _, gone := range []string{"\nPath = ''", "\nPort = 0"} {
		if strings.Contains(toml, gone) {
			t.Errorf("the [AGUI] zero value %q survived", gone)
		}
	}
	// A value the template wants anyway is neither a write nor a divergence.
	if n := strings.Count(toml, "AutoApprove = false"); n != 1 {
		t.Errorf("AutoApprove appears %d times, want 1", n)
	}
	if n := strings.Count(toml, "AllowedOrigins = []"); n != 1 {
		t.Errorf("AllowedOrigins appears %d times, want 1", n)
	}
	// Keys of the user's inside [AGUI] are still none of gintrack's business.
	if !strings.Contains(toml, "AgentPoolTTL = ''") {
		t.Error("the merge touched an [AGUI] key gintrack does not own")
	}
	for _, key := range []string{"Enabled", "Path", "Port", "AutoApprove", "AllowedOrigins"} {
		if strings.Contains(stdout, "[AGUI] "+key+":") {
			t.Errorf("[AGUI] %s was reported as a divergence:\n%s", key, stdout)
		}
	}

	// The rule does not generalise: outside [AGUI] a zero value is a decision.
	if !strings.Contains(stdout, "[MCPServer] StdioEnabled: kept StdioEnabled = false") {
		t.Errorf("[MCPServer] StdioEnabled = false stopped being a divergence:\n%s", stdout)
	}
	if !strings.Contains(toml, "StdioEnabled = false") {
		t.Error("[MCPServer] StdioEnabled was overwritten")
	}
	if !strings.Contains(stdout, "[PersonaAutoSelect] PersonaPath: kept PersonaPath = ''") {
		t.Errorf("an empty string outside [AGUI] stopped being a divergence:\n%s", stdout)
	}
}
