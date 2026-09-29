package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

// pandoInstanceRow is one repository of `gintrack pando status`. It carries the
// path of the token file and never the token.
type pandoInstanceRow struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	OptedIn bool   `json:"optedIn"`
	// State is the supervisor's state, "disabled" for a repository with no
	// instance directory that did not opt in, or "stopped (stale)" for a state
	// file whose supervisor is gone.
	State     string `json:"state"`
	Stale     bool   `json:"stale,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Port      int    `json:"port,omitempty"`
	Version   string `json:"version,omitempty"`
	MCPURL    string `json:"mcpUrl,omitempty"`
	TokenFile string `json:"tokenFile,omitempty"`
	Error     string `json:"error,omitempty"`
}

// pandoStatusPayload is what `gintrack pando status --json` prints.
type pandoStatusPayload struct {
	Mode      string             `json:"mode"`
	Rule      int                `json:"rule"`
	Reason    string             `json:"reason"`
	Binary    string             `json:"binary,omitempty"`
	Instances []pandoInstanceRow `json:"instances"`
}

// pandoStatus reads the state of every project repository of the workspace
// from the instance directories: no server is contacted and nothing is
// started. repo narrows it to one repository id.
func pandoStatus(cfg *config.Config, configPath, workspace, repo string, lookPath func(string) (string, error)) (pandoStatusPayload, error) {
	res := config.ResolvePandoMode(cfg.Search.Pando, lookPath)
	out := pandoStatusPayload{Mode: res.Mode, Rule: res.Rule, Reason: res.Reason, Binary: res.Binary, Instances: []pandoInstanceRow{}}
	cache := cfg.CacheDir(configPath)
	found := repo == ""
	for _, r := range cfg.WorkspaceRepos(workspace) {
		if r.Role == config.RoleTeam || (repo != "" && r.ID != repo) {
			continue
		}
		found = true
		row := pandoInstanceRow{Repo: r.ID, Path: r.Path, OptedIn: r.SemanticSearch}
		st, live := supervisor.Inspect(supervisor.InstanceDir(cache, supervisor.InstanceKey(r.Path)))
		switch live {
		case supervisor.LiveAbsent:
			row.State = "disabled"
			if r.SemanticSearch {
				row.State = "not running"
			}
		case supervisor.LiveStale:
			row.State, row.Stale = "stopped (stale)", true
		default:
			row.State = string(st.State)
		}
		if live != supervisor.LiveAbsent {
			row.PID, row.Port, row.Version = st.PID, st.Port, st.Version
			row.MCPURL, row.TokenFile, row.Error = st.MCPURL, st.TokenFile, st.LastError
			if row.Stale {
				row.PID, row.Port = 0, 0
			}
		}
		out.Instances = append(out.Instances, row)
	}
	if !found {
		return out, notFoundf("no repository %q is registered in workspace %q", repo, workspace)
	}
	return out, nil
}

// pandoFlags are the flags shared by the subcommands.
type pandoFlags struct {
	repo         string
	asJSON       bool
	companionURL string
	token        string
}

// newPandoCommand groups `gintrack pando`.
func newPandoCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pando",
		Short: "Inspect and control the Pando instances gintrack serve manages",
		Long: `In managed mode "gintrack serve" starts one Pando per repository that opted in
to semantic search (repos[].semanticSearch) and supervises it. "gintrack pando
status" reads what those instances left on disk, so it works with no server
running; start, stop, restart and reset act on the running "gintrack serve".

"gintrack mcp" never starts or proxies Pando. An agent that wants Pando's own
tools connects to the mcpUrl printed here, with the bearer token read from the
tokenFile. That connection exposes Pando's write tools too: it is not read-only.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return usageError(cmd.Help()) },
	}
	cmd.AddCommand(newPandoStatusCommand(flags))
	for _, verb := range []string{"start", "stop", "restart", "reset"} {
		cmd.AddCommand(newPandoControlCommand(flags, verb))
	}
	return cmd
}

func newPandoStatusCommand(flags *globalFlags) *cobra.Command {
	local := &pandoFlags{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the Pando mode and the state of each repository's instance",
		Long: `Print the effective Pando mode and the rule that chose it, then for each
repository the state, pid, port, version, mcpUrl and tokenFile of its managed
instance. The token itself is never printed: read it from the tokenFile.

A state file whose supervising "gintrack serve" is gone is reported as
"stopped (stale)". Nothing is started or contacted.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := flags.resolve()
			if err != nil {
				return err
			}
			payload, err := pandoStatus(res.Config, res.Path, res.Workspace, local.repo, exec.LookPath)
			if err != nil {
				return err
			}
			p := flags.printer(cmd, local.asJSON)
			if p.JSONMode() {
				return render(p.JSON(payload))
			}
			p.Printf("mode: %s (rule %d): %s\n", payload.Mode, payload.Rule, payload.Reason)
			if payload.Binary != "" {
				p.Printf("binary: %s\n", payload.Binary)
			}
			for _, r := range payload.Instances {
				p.Printf("\n%s  %s\n", r.Repo, r.State)
				if r.PID > 0 {
					p.Printf("  pid: %d  port: %d\n", r.PID, r.Port)
				}
				for _, kv := range [][2]string{{"version", r.Version}, {"mcpUrl", r.MCPURL}, {"tokenFile", r.TokenFile}, {"error", r.Error}} {
					if kv[1] != "" {
						p.Printf("  %s: %s\n", kv[0], kv[1])
					}
				}
			}
			return nil
		},
	}
	local.bind(cmd, false)
	return cmd
}

func (f *pandoFlags) bind(cmd *cobra.Command, control bool) {
	cmd.Flags().StringVar(&f.repo, "repo", "", "repository id (default: every repository)")
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "print machine-readable JSON")
	if control {
		cmd.Flags().StringVar(&f.companionURL, "companion-url", "",
			"base URL of the running gintrack serve (default: the configured bind address and port)")
		cmd.Flags().StringVar(&f.token, "token", "",
			"bearer token of gintrack serve (default: server.token from the configuration)")
	}
}

// pandoControlResult is one repository's answer to a lifecycle verb.
type pandoControlResult struct {
	Repo    string          `json:"repo"`
	Action  string          `json:"action"`
	Managed json.RawMessage `json:"managed"`
}

func newPandoControlCommand(flags *globalFlags, verb string) *cobra.Command {
	local := &pandoFlags{}
	short := map[string]string{
		"start":   "Start the managed Pando of a repository",
		"stop":    "Stop the managed Pando of a repository, keeping its index",
		"restart": "Restart the managed Pando of a repository",
		"reset":   "Stop the managed Pando, delete its index and start it again",
	}[verb]
	cmd := &cobra.Command{
		Use:   verb,
		Short: short,
		Long: short + `

It acts through the running "gintrack serve" (POST /api/v1/search/managed/{repo}/` + verb + `),
found at the configured bind address and port unless --companion-url says
otherwise; without one it fails and starts nothing. Without --repo it acts on
every repository that opted in to semantic search. It does not change the
opt-in: a repository stopped here is started again by the next "gintrack serve".`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := flags.resolve()
			if err != nil {
				return err
			}
			repos, err := pandoTargets(res.Config, res.Workspace, local.repo)
			if err != nil {
				return err
			}
			base, err := agentCompanionURL(local.companionURL, res.Config)
			if err != nil {
				return err
			}
			token := strings.TrimSpace(local.token)
			if token == "" {
				token = strings.TrimSpace(res.Config.Server.Token)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			var results []pandoControlResult
			p := flags.printer(cmd, local.asJSON)
			for _, repo := range repos {
				r, err := pandoControl(ctx, http.DefaultClient, base, token, repo, verb)
				if err != nil {
					return err
				}
				results = append(results, r)
				if !p.JSONMode() {
					var v struct {
						State string `json:"state"`
						PID   int    `json:"pid"`
						Error string `json:"error"`
					}
					_ = json.Unmarshal(r.Managed, &v)
					line := fmt.Sprintf("%s: %s -> %s", repo, verb, v.State)
					if v.PID > 0 {
						line += " (pid " + strconv.Itoa(v.PID) + ")"
					}
					if v.Error != "" {
						line += ": " + v.Error
					}
					p.Line(line)
				}
			}
			if p.JSONMode() {
				return render(p.JSON(results))
			}
			return nil
		},
	}
	local.bind(cmd, true)
	return cmd
}

// pandoTargets is the repositories a lifecycle verb acts on.
func pandoTargets(cfg *config.Config, workspace, repo string) ([]string, error) {
	var out []string
	for _, r := range cfg.WorkspaceRepos(workspace) {
		if r.Role == config.RoleTeam {
			continue
		}
		if repo != "" {
			if r.ID == repo {
				return []string{r.ID}, nil
			}
			continue
		}
		if r.SemanticSearch {
			out = append(out, r.ID)
		}
	}
	switch {
	case repo != "":
		return nil, notFoundf("no repository %q is registered in workspace %q", repo, workspace)
	case len(out) == 0:
		return nil, usagef("no repository opted in to semantic search: pass --repo <id>")
	}
	return out, nil
}

// pandoControl posts one lifecycle verb to a running serve.
func pandoControl(ctx context.Context, client *http.Client, base, token, repo, verb string) (pandoControlResult, error) {
	endpoint := strings.TrimRight(base, "/") + "/api/v1/search/managed/" + url.PathEscape(repo) + "/" + verb
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return pandoControlResult{}, fmt.Errorf("build the request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return pandoControlResult{}, failf(exitFailure,
			"gintrack serve is not running at %s: start it, or point --companion-url at it (%v)", base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var prob struct {
			Detail string `json:"detail"`
			Title  string `json:"title"`
		}
		_ = json.Unmarshal(body, &prob)
		msg := firstNonEmptyStr(prob.Detail, prob.Title, strings.TrimSpace(string(body)))
		code := exitFailure
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			msg = "gintrack serve refused the token: pass --token or set server.token"
		case http.StatusNotFound:
			code = exitNotFound
		case http.StatusConflict:
			code = exitConflict
		}
		return pandoControlResult{}, failf(code, "%s %s: %s (HTTP %d)", verb, repo, msg, resp.StatusCode)
	}
	var out pandoControlResult
	if err := json.Unmarshal(body, &out); err != nil {
		return pandoControlResult{}, fmt.Errorf("read the answer of gintrack serve: %w", err)
	}
	return out, nil
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
