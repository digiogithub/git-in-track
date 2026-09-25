package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/trace"
)

// This file is the `gintrack spec` command family (docs/07 section 4.19).
// `spec ingest` (GIT-US-0115) lives here; lint, impact, coverage, verify and
// trace (GIT-US-0125) live in the spec_*.go files beside it. The commands stay
// thin: parsing, mapping, matching and the cache live in internal/trace, and
// the queries are the vault methods the MCP tools call.

func newSpecCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Work with specs and their requirements",
		Args:  noArgs,
	}
	cmd.AddCommand(
		newSpecIngestCommand(flags),
		newSpecLintCommand(flags),
		newSpecImpactCommand(flags),
		newSpecCoverageCommand(flags),
		newSpecVerifyCommand(flags),
		newSpecTraceCommand(flags),
		newSpecHookCommand(flags),
		newSpecTemplatesCommand(flags),
	)
	return cmd
}

// specIngestFlags mirrors the flags of docs/07 section 4.19.
type specIngestFlags struct {
	format string
	repo   string
	base   string
	commit string
	cache  string
	asJSON bool
}

// specIngestPayload is what `gintrack spec ingest --json` prints.
type specIngestPayload struct {
	Root         string                    `json:"root"`
	Commit       string                    `json:"commit,omitempty"`
	Cache        string                    `json:"cache"`
	Reports      []trace.IngestReport      `json:"reports"`
	Stored       trace.MergeStats          `json:"stored"`
	Requirements []trace.RequirementResult `json:"requirements"`
	// Verify lists the verification caches (<docs>/.pmngr/verify.json) the
	// ingest recorded entries in, one per project (GIT-US-0141).
	Verify []trace.VerifyRecord `json:"verify"`
}

func newSpecIngestCommand(flags *globalFlags) *cobra.Command {
	local := &specIngestFlags{}
	cmd := &cobra.Command{
		Use:   "ingest <report>...",
		Short: "Record test results from go test -json, JUnit XML or Vitest JSON reports",
		Long: `Parse test reports, map every test to the "<path>#<symbol>" trace ref the
requirement trace uses, and record the last result of each test in the local
test-result cache. The cache is derived, per machine and outside the
repository; nothing is written into a spec.

Every requirement whose linked tests a report touched also gets an entry in
its project's verification cache, <docs>/.pmngr/verify.json: the block rev
of its text now, the commit, its linked tests with their results and the
aggregate. It is derived and git-ignored; coverage, "spec verify --commit"
and moving a story to done read it.

Supported formats: go (go test -json), junit (JUnit XML) and vitest (the Vitest
or Jest JSON reporter). The format is detected per file unless --format is
given. "-" reads a report from standard input.

After ingesting, the requirements whose linked tests (Verifies: markers and
trace.tests entries) have results are listed with the aggregate outcome:
pass, fail, partial or untested.`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSpecIngest(cmd, flags, local, args)
		},
	}
	f := cmd.Flags()
	f.StringVar(&local.format, "format", "", "report format: go, junit or vitest (default: detect)")
	f.StringVar(&local.repo, "repo", ".", "repository the tests belong to (its working-tree root is found upwards)")
	f.StringVar(&local.base, "base", "", "repository-relative directory the report's relative paths start from, e.g. web")
	f.StringVar(&local.commit, "commit", "", "commit the tests ran at (default: the working tree's HEAD)")
	f.StringVar(&local.cache, "cache", "", "test-result cache file (default: under the index cache directory)")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func runSpecIngest(cmd *cobra.Command, flags *globalFlags, local *specIngestFlags, args []string) error {
	format := trace.ReportFormat(local.format)
	if format != "" && !format.Valid() {
		return usageError(fmt.Errorf("unknown --format %q: use go, junit or vitest", local.format))
	}
	root, err := trace.FindRoot(local.repo)
	if err != nil {
		return usageError(err)
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return failf(exitNotFound, "repository %s is not a directory", local.repo)
	}
	cachePath := local.cache
	if cachePath == "" {
		res, err := flags.resolve()
		if err != nil {
			return err
		}
		cachePath = trace.DefaultResultCachePath(res.Config.CacheDir(res.Path), root)
	}
	commit := local.commit
	if commit == "" {
		commit = headCommit(cmd, root)
	}

	resolver := trace.NewTestResolver(os.DirFS(root), root, local.base)
	now := time.Now()
	payload := specIngestPayload{
		Root: root, Commit: commit, Cache: cachePath,
		Requirements: []trace.RequirementResult{}, Verify: []trace.VerifyRecord{},
	}
	var fresh []trace.TestResult
	for _, name := range args {
		got, raws, err := parseReportFile(cmd, name, format)
		if err != nil {
			return err
		}
		results, rep := trace.Stamp(resolver, raws, commit, now)
		rep.File, rep.Format = name, got
		payload.Reports = append(payload.Reports, rep)
		fresh = append(fresh, results...)
	}

	store := trace.NewResultStore(cachePath)
	payload.Stored, err = store.Merge(root, fresh)
	if err != nil {
		return fmt.Errorf("record the results: %w", err)
	}
	all, _, err := store.Load()
	if err != nil {
		return fmt.Errorf("read the results: %w", err)
	}
	fsys, ix, g, err := trace.RepositoryTrace(cmd.Context(), root)
	if err != nil {
		return fmt.Errorf("build the requirement trace of %s: %w", root, err)
	}
	if g != nil {
		for _, rr := range trace.MatchRequirements(g, all) {
			if len(rr.Tests) > 0 {
				payload.Requirements = append(payload.Requirements, rr)
			}
		}
		by := commentAuthor("", flags.config())
		entries := trace.VerificationEntries(ix, g, all, fresh, by)
		if payload.Verify, err = trace.RecordVerification(fsys, ix, entries); err != nil {
			return fmt.Errorf("record the verification cache: %w", err)
		}
	}
	return renderSpecIngest(cmd, flags, local, payload)
}

// parseReportFile parses one report file, "-" being standard input.
func parseReportFile(cmd *cobra.Command, name string, format trace.ReportFormat) (trace.ReportFormat, []trace.RawResult, error) {
	in := cmd.InOrStdin()
	if name != "-" {
		f, err := os.Open(filepath.Clean(name))
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, failf(exitNotFound, "report %s does not exist", name)
		}
		if err != nil {
			return "", nil, fmt.Errorf("open the report: %w", err)
		}
		defer func() { _ = f.Close() }()
		in = f
	}
	got, raws, err := trace.ParseReport(in, format)
	if err != nil {
		return "", nil, fail(exitValidation, fmt.Errorf("%s: %w", name, err))
	}
	return got, raws, nil
}

// headCommit returns the commit the working tree sits on, "" when the
// repository has no VCS or no commit yet: a result without a commit is still
// worth recording.
func headCommit(cmd *cobra.Command, root string) string {
	backend, err := gitops.Open(root, gitops.Options{})
	if err != nil {
		return ""
	}
	commits, err := backend.Commits(cmd.Context(), gitops.LogRequest{Limit: 1})
	if err != nil || len(commits) == 0 {
		return ""
	}
	return commits[0].SHA
}

func renderSpecIngest(cmd *cobra.Command, flags *globalFlags, local *specIngestFlags, payload specIngestPayload) error {
	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	for _, r := range payload.Reports {
		p.Printf("%s (%s): %s — %d passed, %d failed, %d skipped, %d unmapped\n",
			r.File, r.Format, plural(r.Tests, "test", "tests"), r.Passed, r.Failed, r.Skipped, r.Unmapped)
	}
	rebuilt := ""
	if payload.Stored.Rebuilt {
		rebuilt = ", rebuilt from a corrupt file"
	}
	p.Printf("cache %s: %d added, %d replaced, %d total%s\n",
		payload.Cache, payload.Stored.Added, payload.Stored.Replaced, payload.Stored.Total, rebuilt)
	for _, v := range payload.Verify {
		rebuilt := ""
		if v.Rebuilt {
			rebuilt = ", rebuilt from a corrupt file"
		}
		p.Printf("verification cache %s: %d added, %d replaced, %d total%s\n",
			v.Path, v.Added, v.Replaced, v.Total, rebuilt)
	}
	for _, rr := range payload.Requirements {
		passed := 0
		for _, t := range rr.Tests {
			if t.Result == trace.OutcomePass {
				passed++
			}
		}
		p.Printf("%-24s %-8s %d/%d linked tests passed\n", rr.Ref, rr.Result, passed, len(rr.Tests))
	}
	return nil
}
