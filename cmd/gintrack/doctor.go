package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/cmd/gintrack/output"
	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
)

// The marks doctor prints in front of a line.
const (
	markOK      = "✔"
	markWarning = "⚠"
	markError   = "✖"
)

// doctorFlags mirrors the flags of docs/07 section 4.8.
type doctorFlags struct {
	fix      bool
	renumber bool
	yes      bool
	repo     string
	strict   bool
	asJSON   bool
}

// checkResult is one line of the report.
type checkResult struct {
	Scope    string `json:"scope"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

// fileReport groups the diagnostics of one file, which is how doctor prints
// content findings.
type fileReport struct {
	Path        string           `json:"path"`
	Diagnostics []diagnosticInfo `json:"diagnostics"`
}

// repoReport is everything doctor found about one repository.
type repoReport struct {
	ID         string           `json:"id"`
	Path       string           `json:"path"`
	Checks     []checkResult    `json:"checks"`
	Files      []fileReport     `json:"files,omitempty"`
	Duplicates []core.Duplicate `json:"duplicates,omitempty"`
	Renumbered []core.Renumber  `json:"renumbered,omitempty"`
	Fixed      []string         `json:"fixed,omitempty"`
	Errors     int              `json:"errors"`
	Warnings   int              `json:"warnings"`
}

// doctorPayload is what `gintrack doctor --json` prints.
type doctorPayload struct {
	Config   []checkResult `json:"config"`
	Repos    []repoReport  `json:"repos"`
	Errors   int           `json:"errors"`
	Warnings int           `json:"warnings"`
}

// newDoctorCommand runs the health check.
func newDoctorCommand(flags *globalFlags) *cobra.Command {
	local := &doctorFlags{}

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the configuration, the repositories and their content",
		Long: `Validate everything: the configuration file and its permissions, the registered
repositories, and every backlog file they hold.

--fix applies the safe repairs only: it rewrites front matter in canonical key
order and renames files whose slug drifted from the title. --renumber is
separate and destructive, because ids are public identifiers: it prints the full
plan and asks before touching anything.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd, flags, local)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&local.fix, "fix", false, "apply the safe automatic fixes")
	f.BoolVar(&local.renumber, "renumber", false, "repair duplicate ids (destructive)")
	f.BoolVar(&local.yes, "yes", false, "do not ask for confirmation")
	f.StringVar(&local.repo, "repo", "", "limit the check to one repository id")
	f.BoolVar(&local.strict, "strict", false, "treat warnings as errors")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runDoctor checks everything and reports it.
func runDoctor(cmd *cobra.Command, flags *globalFlags, local *doctorFlags) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	payload := doctorPayload{Config: checkConfig(res)}

	repos := res.Config.WorkspaceRepos(res.Workspace)
	if local.repo != "" {
		repo, ok := res.Config.Repo(local.repo)
		if !ok {
			return notFoundf("no repository %q is registered", local.repo)
		}
		repos = []config.Repo{repo}
	}
	p := flags.printer(cmd, local.asJSON)
	for _, repo := range repos {
		report := checkRepo(cmd, local, repo, p)
		payload.Errors += report.Errors
		payload.Warnings += report.Warnings
		payload.Repos = append(payload.Repos, report)
	}
	for _, c := range payload.Config {
		switch c.Severity {
		case string(core.SeverityError):
			payload.Errors++
		case string(core.SeverityWarning):
			payload.Warnings++
		}
	}

	if p.JSONMode() {
		if err := p.JSON(payload); err != nil {
			return render(err)
		}
	} else {
		printDoctor(p, payload)
	}
	if payload.Errors > 0 || (local.strict && payload.Warnings > 0) {
		return failf(exitValidation, "%s, %s", plural(payload.Errors, "error", "errors"), plural(payload.Warnings, "warning", "warnings"))
	}
	return nil
}

// checkConfig inspects the configuration file itself.
func checkConfig(res *config.Resolution) []checkResult {
	var out []checkResult
	if !res.Exists {
		out = append(out, checkResult{
			Scope: "config", Severity: string(core.SeverityWarning),
			Message: res.Path + " does not exist yet; the built-in defaults are in use",
			Fix:     "gintrack config init",
		})
		return out
	}
	// The file holds the bearer token of the local API, so a mode another user
	// can read is a finding. Windows carries the restriction in its ACL, which
	// the Unix permission bits do not describe.
	mode, tooOpen := "unknown", false
	if info, err := os.Stat(res.Path); err == nil {
		mode = fmt.Sprintf("%04o", info.Mode().Perm())
		tooOpen = info.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows"
	}
	result := checkResult{
		Scope:    "config",
		Severity: "ok",
		Message: fmt.Sprintf("%s (%s), %s, %s", res.Path, mode,
			plural(len(res.Config.Workspaces), "workspace", "workspaces"),
			plural(len(res.Config.Repos), "repository", "repositories")),
	}
	if tooOpen {
		result.Severity = string(core.SeverityWarning)
		result.Message += ": the file holds the API token and should not be readable by other users"
		result.Fix = "chmod 600 " + res.Path
	}
	return append(out, result)
}

// checkRepo validates one repository and, when asked, repairs it.
func checkRepo(cmd *cobra.Command, local *doctorFlags, repo config.Repo, p *output.Printer) repoReport {
	report := repoReport{ID: repo.ID, Path: repo.Path}
	add := func(severity, message, fix string) {
		report.Checks = append(report.Checks, checkResult{Scope: repo.ID, Severity: severity, Message: message, Fix: fix})
		switch severity {
		case string(core.SeverityError):
			report.Errors++
		case string(core.SeverityWarning):
			report.Warnings++
		}
	}
	if _, err := os.Stat(repo.Path); err != nil {
		add(string(core.SeverityError), "unreadable path "+repo.Path, "gintrack rm "+repo.ID)
		return report
	}
	if !config.IsGitRepo(repo.Path) {
		add(string(core.SeverityWarning), repo.Path+" is not a git working tree", "")
	}

	view, err := openRepo(cmd.Context(), repo, true)
	if err != nil {
		add(string(core.SeverityError), err.Error(), "")
		return report
	}
	if len(view.Projects) == 0 {
		add(string(core.SeverityError), "no .pmngr/project.yaml found under "+repo.Path, "gintrack add --docs <folder>")
	}
	for _, ref := range view.Projects {
		for _, d := range ref.Diagnostics {
			add(string(d.Severity), ref.ConfigPath+": "+d.Message, "")
		}
		if ref.Config != nil && len(ref.Diagnostics) == 0 {
			add("ok", fmt.Sprintf("%s: git ok, %s/, .pmngr ok, project.yaml valid", ref.Key, ref.DocsPath), "")
		}
	}

	report.Duplicates = findDuplicates(view)
	// Duplicate ids are reported on their own, with the files that claim them
	// and the command that repairs them, so they are dropped from the per-file
	// list to keep every finding on exactly one line.
	report.Files = groupDiagnostics(view.Index.Warnings(), core.CodeIDDuplicate)
	report.Errors += len(report.Duplicates)
	for _, f := range report.Files {
		for _, d := range f.Diagnostics {
			if d.Severity == string(core.SeverityError) {
				report.Errors++
			} else {
				report.Warnings++
			}
		}
	}
	if local.fix {
		fixed, err := applyFixes(cmd.Context(), view)
		if err != nil {
			add(string(core.SeverityError), "fix: "+err.Error(), "")
		}
		report.Fixed = fixed
	}
	if local.renumber && len(report.Duplicates) > 0 {
		done, err := applyRenumber(cmd, local, view, report.Duplicates, p)
		if err != nil {
			add(string(core.SeverityError), "renumber: "+err.Error(), "")
		}
		report.Renumbered = done
	}
	return report
}

// findDuplicates collects the colliding ids of every project of a repository.
func findDuplicates(view *repoView) []core.Duplicate {
	var out []core.Duplicate
	for _, ref := range view.Projects {
		dups, err := core.FindDuplicateIDs(view.FS, ref.BacklogPath)
		if err != nil {
			continue
		}
		out = append(out, dups...)
	}
	return out
}

// groupDiagnostics sorts findings by file, which is how doctor prints them.
func groupDiagnostics(diags []core.Diagnostic, skip ...core.Code) []fileReport {
	byPath := make(map[string][]diagnosticInfo)
	for _, d := range diags {
		if containsCode(skip, d.Code) {
			continue
		}
		key := d.Path
		if key == "" {
			key = "(configuration)"
		}
		byPath[key] = append(byPath[key], newDiagnosticInfo(d))
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]fileReport, 0, len(paths))
	for _, p := range paths {
		out = append(out, fileReport{Path: p, Diagnostics: byPath[p]})
	}
	return out
}

// containsCode reports whether a diagnostic code is in a list.
func containsCode(codes []core.Code, code core.Code) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

// applyFixes repairs the safe class: front matter that is not in canonical form
// and file names whose slug drifted from the title. Both repairs are performed
// by the core serializer, so a fix can never invent a format of its own.
func applyFixes(ctx context.Context, view *repoView) ([]string, error) {
	page, err := view.Index.Items(ctx, core.Filter{Limit: core.MaxLimit, IncludeDeleted: true})
	if err != nil {
		return nil, fmt.Errorf("read the items: %w", err)
	}
	var fixed []string
	for i := range page.Items {
		it := page.Items[i]
		canonical, err := core.SerializeItem(&it)
		if err != nil {
			return fixed, fmt.Errorf("%s: %w", it.Path, err)
		}
		current, err := view.FS.ReadFile(it.Path)
		if err != nil {
			return fixed, fmt.Errorf("%s: %w", it.Path, err)
		}
		if !bytes.Equal(current, canonical) {
			if err := view.FS.WriteFile(it.Path, canonical); err != nil {
				return fixed, fmt.Errorf("%s: %w", it.Path, err)
			}
			fixed = append(fixed, it.Path+": front matter normalized")
		}
		want := core.FileName(it.ID, it.Title)
		if path.Base(it.Path) != want {
			target := path.Join(path.Dir(it.Path), want)
			if err := view.FS.Rename(it.Path, target); err != nil {
				return fixed, fmt.Errorf("%s: %w", it.Path, err)
			}
			fixed = append(fixed, it.Path+" renamed to "+want)
		}
	}
	return fixed, nil
}

// applyRenumber plans a renumbering, prints it, asks for confirmation and runs
// it. The plan and the rewrite both come from the core.
func applyRenumber(cmd *cobra.Command, local *doctorFlags, view *repoView, dups []core.Duplicate, p *output.Printer) ([]core.Renumber, error) {
	var done []core.Renumber
	for _, ref := range view.Projects {
		if ref.Config == nil {
			continue
		}
		mine := duplicatesOfProject(dups, ref)
		if len(mine) == 0 {
			continue
		}
		alloc := core.NewAllocator(view.FS, ref.BacklogPath, ref.Config)
		plan, err := core.PlanRenumber(cmd.Context(), mine, alloc)
		if err != nil {
			return done, fmt.Errorf("plan: %w", err)
		}
		if len(plan) == 0 {
			continue
		}
		p.Warnf("renumbering plan for %s:\n", ref.Key)
		for _, step := range plan {
			p.Warnf("  %s -> %s  %s -> %s\n", step.OldID, step.NewID, step.Path, step.NewPath)
		}
		if !local.yes && !confirm(cmd, p, "apply this plan?") {
			p.Warnf("skipped %s\n", ref.Key)
			continue
		}
		if _, err := core.ApplyRenumber(cmd.Context(), view.FS, ref.BacklogPath, plan); err != nil {
			return done, fmt.Errorf("apply: %w", err)
		}
		done = append(done, plan...)
	}
	return done, nil
}

// duplicatesOfProject keeps the duplicates whose id belongs to a project.
func duplicatesOfProject(dups []core.Duplicate, ref core.ProjectRef) []core.Duplicate {
	var out []core.Duplicate
	for _, d := range dups {
		if key, _, _, err := core.ParseItemID(string(d.ID)); err == nil && key == ref.Key {
			out = append(out, d)
		}
	}
	return out
}

// confirm asks a yes/no question on the terminal.
func confirm(cmd *cobra.Command, p *output.Printer, question string) bool {
	p.Warnf("%s [y/N] ", question)
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// printDoctor renders the human report.
func printDoctor(p *output.Printer, payload doctorPayload) {
	line := func(c checkResult) {
		p.Printf("%s %-16s %s\n", mark(c.Severity), c.Scope, c.Message)
		if c.Fix != "" {
			p.Printf("%s %-16s fix: %s\n", " ", "", c.Fix)
		}
	}
	for _, c := range payload.Config {
		line(c)
	}
	for _, repo := range payload.Repos {
		for _, c := range repo.Checks {
			line(c)
		}
		for _, f := range repo.Files {
			for _, d := range f.Diagnostics {
				p.Printf("%s %-16s %s\n", mark(d.Severity), repo.ID, f.Path)
				p.Printf("%s %-16s   %s %s\n", " ", "", d.Code, d.Message)
			}
		}
		for _, d := range repo.Duplicates {
			p.Printf("%s %-16s duplicate id %s\n", markError, repo.ID, d.ID)
			for _, f := range d.Files {
				p.Printf("%s %-16s   %s\n", " ", "", f.Path)
			}
			p.Printf("%s %-16s fix: gintrack doctor --renumber --repo %s\n", " ", "", repo.ID)
		}
		for _, f := range repo.Fixed {
			p.Printf("%s %-16s fixed %s\n", markOK, repo.ID, f)
		}
		for _, r := range repo.Renumbered {
			p.Printf("%s %-16s renumbered %s -> %s\n", markOK, repo.ID, r.OldID, r.NewID)
		}
	}
	p.Printf("%s, %s\n", plural(payload.Errors, "error", "errors"), plural(payload.Warnings, "warning", "warnings"))
}

// mark returns the symbol of a severity.
func mark(severity string) string {
	switch severity {
	case string(core.SeverityError):
		return markError
	case string(core.SeverityWarning):
		return markWarning
	default:
		return markOK
	}
}
