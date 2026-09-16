package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
)

// agentTemplates holds the files `gintrack agent init` writes. They are
// embedded rather than generated in code so that the shape of the Pando
// configuration is reviewable as one file, next to the documentation that
// explains it (docs/20-agent-interface.md).
//
//go:embed templates/agent/*.tmpl
var agentTemplates embed.FS

// The generated file names, relative to the repository root.
const (
	agentPandoConfigName = ".pando.toml"
	agentPersonaName     = "agents/personas/backlog-assistant.md"
	agentSkillName       = "agents/skills/gintrack-search/SKILL.md"
)

// agentPersonaID is the persona and profile name the generated configuration
// points at. It is also the route the browser posts a run to:
// POST {AGUI.Path}/backlog-assistant.
const agentPersonaID = "backlog-assistant"

// agentDefaultAGUIPath is Pando's own default AG-UI route prefix. The generated
// file states it explicitly so the companion and Pando cannot drift apart when
// Pando changes its default.
const agentDefaultAGUIPath = "/api/v1/agui"

// agentDefaultAGUIPort is the loopback port the generated file gives the AG-UI
// listener. It is a dedicated listener: nothing else of Pando's API is on it.
const agentDefaultAGUIPort = 8090

// agentDefaultMaxRuns is Pando's own backstop on concurrent AG-UI runs. The
// companion imposes its own per-user and global caps on top of it.
const agentDefaultMaxRuns = 4

// agentTools is the glob allow-list written into `[AGUI] Tools`. Pando matches
// a glob (path.Match semantics) against each tool's Info().Name and keeps only
// the tools that match, so this is what turns the coder agent into something
// closer to a backlog assistant. MCP gateway tools are named
// `<server>_<tool>`, which is why the gintrack entry is `gintrack_*` and not
// `gintrack__*`.
var agentTools = []string{
	"gintrack_*",
	"kb_search_documents",
	"kb_get_document",
	"code_hybrid_search",
	"code_find_symbol",
}

// agentSecretPrefix is the marker Pando puts in front of an age ciphertext.
// `pando secret <value>` prints it and Pando's configuration loader decrypts
// any value carrying it (internal/config/agecrypto.go).
const agentSecretPrefix = "age1:"

// agentInitFlags are the flags of `gintrack agent init`.
type agentInitFlags struct {
	repo           string
	companionURL   string
	aguiPort       int
	force          bool
	asJSON         bool
	pando          string
	ageKeys        string
	plaintextToken bool
}

// agentTemplateData is what the embedded templates are rendered against.
type agentTemplateData struct {
	RepoID            string
	RepoPath          string
	AGUIPath          string
	AGUIHost          string
	AGUIPort          int
	MaxConcurrentRuns int
	Persona           string
	Tools             []string
	MCPURL            string
	Token             bool
	Encrypted         bool
	AuthToken         string
	BearerHeader      string
	KBPath            string
}

// agentInitPayload is what `gintrack agent init --json` prints. It carries no
// token, only whether one was found.
type agentInitPayload struct {
	Repo      string   `json:"repo"`
	RepoID    string   `json:"repoId"`
	Written   []string `json:"written"`
	Skipped   []string `json:"skipped,omitempty"`
	KBPath    string   `json:"kbPath"`
	MCPURL    string   `json:"mcpUrl"`
	TokenFile string   `json:"tokenFile"`
	AGUIPort  int      `json:"aguiPort"`
	HasToken  bool     `json:"hasCompanionToken"`
	Encrypted bool     `json:"tokenEncrypted"`
	// Merged is set when an existing .pando.toml was merged into rather than
	// written from scratch; Backup is the copy taken before the first edit and
	// Divergences are the keys the merge refused to overwrite.
	Merged      bool              `json:"pandoConfigMerged"`
	Backup      string            `json:"pandoConfigBackup,omitempty"`
	Divergences []agentDivergence `json:"divergences,omitempty"`
}

// newAgentCommand builds the `agent` command tree.
func newAgentCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Set up the Pando agent interface for a repository",
		Long: `Commands for the agent panel: a Pando instance answering questions about one
repository's backlog, reached from the browser through the companion.

See docs/20-agent-interface.md for the architecture and the security model.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAgentInitCommand(flags))
	return cmd
}

// newAgentInitCommand writes the Pando-side configuration for one repository.
func newAgentInitCommand(flags *globalFlags) *cobra.Command {
	local := &agentInitFlags{}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write the Pando configuration, persona and skill for a repository",
		Long: `Write everything Pando needs to serve the agent panel for one repository:

  .pando.toml                                 the AG-UI adapter, the gintrack MCP
                                              server and the corpus index
  agents/personas/backlog-assistant.md        the persona every run is given
  agents/skills/gintrack-search/SKILL.md      how the assistant picks a search tool

The deployment is one Pando process per repository, so there is exactly one
.pando.toml per repository. Re-running is the normal way to pick up a change --
a new companion URL, a new token, a newer template -- and nothing in the way is
an error:

  .pando.toml   is MERGED into. A Pando configuration is a Pando-wide file, so
                every table, key and comment it already had survives, the tables
                and keys gintrack owns are written, and a key gintrack would set
                that already has another value is left alone and reported. A copy
                of the previous version is kept next to it. Inside [AGUI] only, a
                key left at its zero value ('', 0, false, []) counts as unset and
                is written.
  the persona   are gintrack's own files and may have been edited by hand, so an
  the skill     existing one is left alone and reported.

--force replaces all three wholesale instead.

The generated file carries the companion's bearer token, because Pando does not
expand environment variables inside an MCP server's configuration. The token is
encrypted first with "pando secret", which wraps it in an age ciphertext Pando
decrypts on load, and the result goes into [MCPServers.gintrack.Auth]. Without a
usable pando binary the command refuses rather than writing a clear secret; pass
--plaintext-token to write the token as a literal Authorization header instead.
Either way the file is mode 0600 and must not be committed: add .pando.toml to
.gitignore.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAgentInit(cmd, flags, local)
		},
	}

	cmd.Flags().StringVar(&local.repo, "repo", ".", "repository to configure")
	cmd.Flags().StringVar(&local.companionURL, "companion-url", "",
		"base URL of the running companion (default: the configured bind address and port)")
	cmd.Flags().IntVar(&local.aguiPort, "agui-port", agentDefaultAGUIPort, "loopback port the AG-UI adapter listens on")
	cmd.Flags().BoolVar(&local.force, "force", false,
		"replace all three files wholesale instead of merging and skipping")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	cmd.Flags().StringVar(&local.pando, "pando", "",
		"path to the pando binary used to encrypt the companion token (default: the one on PATH)")
	cmd.Flags().StringVar(&local.ageKeys, "age-keys", "",
		"name of the Pando age key set to encrypt the companion token with")
	cmd.Flags().BoolVar(&local.plaintextToken, "plaintext-token", false,
		"write the companion token as a literal Authorization header instead of encrypting it")
	return cmd
}

// runAgentInit renders the embedded templates into the repository.
func runAgentInit(cmd *cobra.Command, flags *globalFlags, local *agentInitFlags) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	if local.aguiPort < 1 || local.aguiPort > 65535 {
		return usagef("--agui-port must be between 1 and 65535, got %d", local.aguiPort)
	}

	repoPath, err := config.Expand(local.repo, flags.reader())
	if err != nil {
		return usagef("resolve --repo %s: %v", local.repo, err)
	}
	info, err := os.Stat(repoPath)
	if err != nil || !info.IsDir() {
		return notFoundf("%s is not a directory", repoPath)
	}
	repoID := agentRepoID(res.Config, repoPath)

	companion, err := agentCompanionURL(local.companionURL, res.Config)
	if err != nil {
		return err
	}

	data := agentTemplateData{
		RepoID:            repoID,
		RepoPath:          repoPath,
		AGUIPath:          agentDefaultAGUIPath,
		AGUIHost:          "127.0.0.1",
		AGUIPort:          local.aguiPort,
		MaxConcurrentRuns: agentDefaultMaxRuns,
		Persona:           agentPersonaID,
		Tools:             agentTools,
		MCPURL:            companion + "/mcp",
		KBPath:            filepath.Join(res.Config.CacheDir(res.Path), "pando-kb", repoID),
	}

	targets := []struct {
		name     string
		template string
		mode     os.FileMode
	}{
		{agentPandoConfigName, "pando.toml.tmpl", 0o600},
		{agentPersonaName, "persona.md.tmpl", 0o644},
		{agentSkillName, "skill.md.tmpl", 0o644},
	}

	// A re-run is the normal way to pick up a change: to the companion URL, to
	// the token, to the template itself. So nothing in the way is a refusal any
	// more (GIT-T-0227).
	//
	// `.pando.toml` is a Pando-wide configuration file, not a gintrack artifact,
	// so an existing one is merged into. The persona and the skill are gintrack's
	// own files and may have been edited by hand, so an existing one is skipped
	// and said so — replacing them stays an explicit --force.
	mergePando := false
	skipped := make([]string, 0, len(targets))
	if !local.force {
		for _, t := range targets {
			if _, statErr := os.Stat(filepath.Join(repoPath, t.name)); statErr != nil {
				continue
			}
			if t.name == agentPandoConfigName {
				mergePando = true
				continue
			}
			skipped = append(skipped, t.name)
		}
	}

	// The token is resolved after the existing files are surveyed so that a run
	// that is going to fail never spawns `pando secret` for nothing.
	if token := strings.TrimSpace(res.Config.Server.Token); token != "" {
		data.Token = true
		if local.plaintextToken {
			data.BearerHeader = "Bearer " + token
		} else {
			ciphertext, encErr := agentEncryptToken(cmd.Context(), local, token)
			if encErr != nil {
				return encErr
			}
			data.Encrypted = true
			data.AuthToken = ciphertext
		}
	}

	var backup string
	var divergences []agentDivergence
	written := make([]string, 0, len(targets))
	for _, t := range targets {
		if slices.Contains(skipped, t.name) {
			continue
		}
		rendered, renderErr := renderAgentTemplate(t.template, data)
		if renderErr != nil {
			return renderErr
		}
		dest := filepath.Join(repoPath, filepath.FromSlash(t.name))
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(dest), mkErr)
		}
		if mergePando && t.name == agentPandoConfigName {
			// A merge that cannot parse the file fails here, before the backup
			// and before any file is touched.
			mergeBackup, mergeDivergences, mergeErr := agentMergePandoConfig(dest, rendered, data.Persona)
			if mergeErr != nil {
				return mergeErr
			}
			backup, divergences = mergeBackup, mergeDivergences
			written = append(written, t.name)
			continue
		}
		if writeErr := os.WriteFile(dest, rendered, t.mode); writeErr != nil {
			return fmt.Errorf("write %s: %w", dest, writeErr)
		}
		// WriteFile only applies the mode when it creates the file, so --force
		// over a world-readable leftover would keep the wrong permissions.
		if chmodErr := os.Chmod(dest, t.mode); chmodErr != nil {
			return fmt.Errorf("set the mode of %s: %w", dest, chmodErr)
		}
		written = append(written, t.name)
	}

	tokenFile, err := agentTokenFile(res.Path, repoID)
	if err != nil {
		return err
	}

	p := flags.printer(cmd, local.asJSON)
	if local.asJSON {
		return render(p.JSON(agentInitPayload{
			Repo:      repoPath,
			RepoID:    repoID,
			Written:   written,
			Skipped:   skipped,
			KBPath:    data.KBPath,
			MCPURL:    data.MCPURL,
			TokenFile: tokenFile,
			AGUIPort:  data.AGUIPort,
			HasToken:  data.Token,
			Encrypted: data.Encrypted,

			Merged:      mergePando,
			Backup:      backup,
			Divergences: divergences,
		}))
	}
	printAgentInitReport(cmd, data, written, skipped, tokenFile, repoPath)
	printAgentMergeReport(cmd, backup, divergences)
	return nil
}

// printAgentMergeReport says what the merge did to a configuration file that was
// already there: where the copy of the previous version is, and every key
// gintrack would have set that the file already had with another value. Those
// are reported and never overwritten — in a live configuration they are load
// bearing (this repository's own [ToolDiscovery] and [MCPGateway] settings are
// what keep `gintrack_*` reachable), so imposing the template's values would
// break a working setup.
func printAgentMergeReport(cmd *cobra.Command, backup string, divergences []agentDivergence) {
	if backup == "" {
		return
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "\n%s already existed and was merged into, not replaced.\n", agentPandoConfigName)
	_, _ = fmt.Fprintf(out, "The previous version is in %s.\n", backup)
	if len(divergences) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\n%d key(s) already had another value and were left alone. Review them by hand:\n",
		len(divergences))
	for _, d := range divergences {
		_, _ = fmt.Fprintf(out, "  [%s] %s: kept %s, recommended %s\n", d.Table, d.Key, d.Have, d.Recommended)
		_, _ = fmt.Fprintf(out, "    %s\n", d.Reason)
	}
}

// printAgentInitReport prints what was written, what was left alone and the two
// commands to run next. It never prints a token value: the AG-UI token stays in its file and
// the companion token stays in the configuration.
func printAgentInitReport(cmd *cobra.Command, data agentTemplateData, written, skipped []string, tokenFile, repoPath string) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Wrote the Pando agent configuration for %s:\n", data.RepoID)
	for _, name := range written {
		_, _ = fmt.Fprintf(out, "  %s\n", name)
	}
	if len(skipped) > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Left alone, because these are gintrack's own files and you may have edited them:")
		for _, name := range skipped {
			_, _ = fmt.Fprintf(out, "  %s\n", name)
		}
		_, _ = fmt.Fprintln(out, "Pass --force to overwrite them with the current templates.")
	}
	_, _ = fmt.Fprintln(out)
	switch {
	case data.Encrypted:
		_, _ = fmt.Fprintf(out, "The companion bearer token was encrypted with `pando secret` and written to\n")
		_, _ = fmt.Fprintf(out, "[MCPServers.gintrack.Auth]; Pando decrypts it on load with its age keys. The file\n")
		_, _ = fmt.Fprintf(out, "is mode 0600; add it to .gitignore anyway, and note that only the machine holding\n")
		_, _ = fmt.Fprintf(out, "those keys can read it.\n\n")
	case data.Token:
		_, _ = fmt.Fprintf(out, "WARNING: --plaintext-token was given, so %s carries the companion\n", agentPandoConfigName)
		_, _ = fmt.Fprintf(out, "bearer token in clear in [MCPServers.gintrack.Headers]. It is mode 0600;\n")
		_, _ = fmt.Fprintf(out, "add it to .gitignore so it is never committed.\n\n")
	default:
		_, _ = fmt.Fprintf(out, "No companion token is configured, so [MCPServers.gintrack.Auth] is empty and\n")
		_, _ = fmt.Fprintf(out, "Pando will not be able to read the backlog. Set one and run this command again:\n")
		_, _ = fmt.Fprintf(out, "a re-run merges into the file rather than replacing it.\n\n")
	}
	_, _ = fmt.Fprintf(out, "The corpus Pando indexes is %s.\n", data.KBPath)
	_, _ = fmt.Fprintf(out, "`gintrack serve --agent --mcp-http` exports it and serves /mcp; it is outside the repository on purpose.\n\n")
	_, _ = fmt.Fprintln(out, "Next, in two terminals:")
	_, _ = fmt.Fprintf(out, "  pando agui-serve --cwd %s --port %d --no-tls --token-file %s\n",
		repoPath, data.AGUIPort, tokenFile)
	_, _ = fmt.Fprintln(out, "  gintrack serve --agent --mcp-http")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintf(out, "The AG-UI token is in %s (mode 0600). Point the companion's\n", tokenFile)
	_, _ = fmt.Fprintln(out, "agent.pando.tokenFile at that same file; it never reaches the browser.")
}

// agentEncryptToken turns the companion's bearer token into the `age1:`
// ciphertext that `[MCPServers.gintrack.Auth] Token` holds, by running
// `pando secret <token>`.
//
// The subprocess lives here, in cmd/gintrack, and never in internal/core,
// which stays WASM-clean and free of os/exec.
//
// The token travels as a command-line argument because `pando secret` reads
// its value from argv and accepts nothing on stdin. On the local machine the
// argument is therefore visible in the process list for the lifetime of the
// call; that is the residual exposure this design trades for never writing the
// secret to disk in clear. docs/20-agent-interface.md says so too.
func agentEncryptToken(ctx context.Context, local *agentInitFlags, token string) (string, error) {
	binary, err := agentResolvePando(local.pando)
	if err != nil {
		return "", err
	}
	args := []string{"secret", token}
	if keys := strings.TrimSpace(local.ageKeys); keys != "" {
		args = append(args, "--age-keys", keys)
	}
	run := exec.CommandContext(ctx, binary, args...)
	run.Stdin = strings.NewReader("")
	var stderr strings.Builder
	run.Stderr = &stderr
	out, err := run.Output()
	if err != nil {
		return "", failf(exitConflict,
			"%s secret failed (%v): %s; nothing was written, fix the Pando age keys or re-run with --plaintext-token",
			binary, err, agentRedact(strings.TrimSpace(stderr.String()), token))
	}
	ciphertext := strings.TrimSpace(string(out))
	if !strings.HasPrefix(ciphertext, agentSecretPrefix) {
		// Anything else is either an old pando that echoed the value back or a
		// binary that is not Pando at all. Refusing keeps the clear token out
		// of the file, and the value itself is never printed.
		return "", failf(exitConflict,
			"%s secret did not return an %s ciphertext, so the token would have been written in clear; "+
				"nothing was written, check the binary or re-run with --plaintext-token",
			binary, agentSecretPrefix)
	}
	return ciphertext, nil
}

// agentResolvePando locates the pando binary: the --pando path when it is
// given, the one on PATH otherwise.
func agentResolvePando(flag string) (string, error) {
	if trimmed := strings.TrimSpace(flag); trimmed != "" {
		binary, err := exec.LookPath(trimmed)
		if err != nil {
			return "", failf(exitConflict, "--pando %s is not an executable file: %v", trimmed, err)
		}
		return binary, nil
	}
	binary, err := exec.LookPath("pando")
	if err != nil {
		return "", failf(exitConflict,
			"pando was not found on PATH, so the companion token cannot be encrypted: nothing was written. "+
				"Install Pando, pass --pando <path>, or re-run with --plaintext-token to write the token in clear")
	}
	return binary, nil
}

// agentRedact removes a secret from a subprocess's diagnostics before it is
// shown, so that a chatty error can never leak the token.
func agentRedact(text, secret string) string {
	if text == "" {
		return "no output"
	}
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "<token>")
}

// renderAgentTemplate renders one embedded template.
func renderAgentTemplate(name string, data agentTemplateData) ([]byte, error) {
	tmpl, err := template.New(name).Funcs(template.FuncMap{"toml": tomlString}).
		ParseFS(agentTemplates, "templates/agent/"+name)
	if err != nil {
		return nil, fmt.Errorf("parse the %s template: %w", name, err)
	}
	var buf strings.Builder
	if execErr := tmpl.Execute(&buf, data); execErr != nil {
		return nil, fmt.Errorf("render the %s template: %w", name, execErr)
	}
	return []byte(buf.String()), nil
}

// agentRepoID is the id the corpus directory and the companion route are keyed
// by: the registration's id when the folder is registered, and the folder name
// otherwise, so that `agent init` works before `gintrack add`.
func agentRepoID(cfg *config.Config, repoPath string) string {
	clean := filepath.Clean(repoPath)
	for _, repo := range cfg.Repos {
		if filepath.Clean(repo.Path) == clean {
			return repo.ID
		}
	}
	return slugifyRepoID(filepath.Base(clean))
}

// slugifyRepoID lowercases a folder name and keeps only what an id may contain,
// matching how `gintrack add` derives an id from a folder.
func slugifyRepoID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.', r == ' ':
			b.WriteRune('-')
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "repo"
	}
	return id
}

// agentCompanionURL is the base URL Pando reaches the companion's MCP endpoint
// at. A bind address of 0.0.0.0 (or ::) means "every interface", which is not
// an address anything can connect to, so loopback is used instead.
func agentCompanionURL(flag string, cfg *config.Config) (string, error) {
	if trimmed := strings.TrimSpace(flag); trimmed != "" {
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			return "", usagef("--companion-url must start with http:// or https://, got %q", trimmed)
		}
		return strings.TrimRight(trimmed, "/"), nil
	}
	host := strings.TrimSpace(cfg.Server.Bind)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = config.DefaultBind
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	port := cfg.Server.Port
	if port == 0 {
		port = config.DefaultPort
	}
	return fmt.Sprintf("http://%s:%d", host, port), nil
}

// agentTokenFile returns the file holding the AG-UI bearer token for one
// repository, creating it with a fresh random token when it does not exist. It
// lives in the companion's state directory, never in the repository, and it is
// the file both `pando agui-serve --token-file` and the companion's
// `agent.pando.tokenFile` read.
func agentTokenFile(configPath, repoID string) (string, error) {
	dir := filepath.Join(config.StateDir(configPath), "agui")
	path := filepath.Join(dir, repoID+".token")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate the AG-UI token: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(raw)+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// tomlString renders a value as a TOML string. It prefers a literal string,
// which needs no escaping at all, and falls back to a basic string when the
// value contains a quote, a backslash or a control character.
func tomlString(v any) string {
	s := fmt.Sprint(v)
	if !strings.ContainsAny(s, "'\n\r\t\\") {
		return "'" + s + "'"
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
