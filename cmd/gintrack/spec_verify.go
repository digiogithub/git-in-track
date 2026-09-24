package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
	corevault "github.com/digiogithub/git-in-track/internal/vault"
)

// This file is `gintrack spec verify` (docs/07 section 4.20): the explicit
// stamp request of ADR-037 section 7, a shim over the vault method
// "requirement.stamp". Without --commit the vault runs over an in-memory
// overlay, so the dry run takes the very decisions a real run takes and writes
// nothing.

// specVerifyFlags mirrors the flags of docs/07 section 4.20.
type specVerifyFlags struct {
	commit bool
	by     string
	asJSON bool
}

// specVerifyPayload is what `gintrack spec verify --json` prints.
type specVerifyPayload struct {
	Commit    bool                             `json:"commit"`
	Stamped   []corevault.StampedRequirement   `json:"stamped"`
	Unstamped []corevault.UnstampedRequirement `json:"unstamped"`
	// Written lists the files the run wrote, or would have written.
	Written []string `json:"written"`
}

func newSpecVerifyCommand(flags *globalFlags) *cobra.Command {
	local := &specVerifyFlags{}
	cmd := &cobra.Command{
		Use:   "verify <ref>...",
		Short: "Stamp requirements whose linked tests all passed",
		Long: `Decide, for each requirement named — a ref such as ACME-SP-0003.R2, or a spec
id for every requirement of that spec — whether the results "gintrack spec
ingest" recorded allow the verified: stamp of ADR-037 section 7: every linked
test passed, at one commit, on the text the requirement holds now.

Without --commit nothing is written: the command reports what would be
stamped and, for the rest, why not (a reason code such as failed, partial,
no-results, no-tests, text, mixed-commits, no-commit, unchanged or
stamp-newer). With --commit the stamps are written into the specs, which is
meant for CI on the main branch after the tests ran and were ingested. It runs
no tests. A requirement left unstamped is not an error: the exit code is 0.

The stamp records who verified it: the handle the results carry, else --by,
else git.authorName from the configuration.`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSpecVerify(cmd, flags, local, args)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&local.commit, "commit", false, "write the stamps (default: report what would be stamped)")
	f.StringVar(&local.by, "by", "", "handle recorded on a stamp whose results name none (default: git.authorName)")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func runSpecVerify(cmd *cobra.Command, flags *globalFlags, local *specVerifyFlags, args []string) error {
	calls := make([]map[string]any, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		switch {
		case isSpecID(arg):
			calls = append(calls, map[string]any{"spec": arg})
		case validRequirementArg(arg):
			// The ref routes the call to the repository that holds the spec.
			calls = append(calls, map[string]any{"refs": []string{arg}, "ref": arg})
		default:
			return usagef("%q is neither a requirement ref nor a spec id: want <KEY>-SP-<NNNN>.R<n> or <KEY>-SP-<NNNN>", arg)
		}
	}
	s, err := openSpecSpace(cmd, flags, specSpaceOptions{seams: true, dryRun: !local.commit})
	if err != nil {
		return err
	}
	defer s.close()
	by := commentAuthor(strings.TrimSpace(local.by), flags.config())
	if by == "" {
		if local.commit {
			return usagef("a stamp records who verified it: pass --by or set git.authorName in the configuration")
		}
		by = "unknown"
	}

	payload := specVerifyPayload{
		Commit:    local.commit,
		Stamped:   []corevault.StampedRequirement{},
		Unstamped: []corevault.UnstampedRequirement{},
		Written:   []string{},
	}
	for _, call := range calls {
		call["by"] = by
		got, err := dispatch[struct {
			Stamped   []corevault.StampedRequirement   `json:"stamped"`
			Unstamped []corevault.UnstampedRequirement `json:"unstamped"`
			Writes    corevault.WriteSet               `json:"writes"`
		}](cmd.Context(), s.space, "requirement.stamp", call)
		if err != nil {
			return err
		}
		payload.Stamped = append(payload.Stamped, got.Stamped...)
		payload.Unstamped = append(payload.Unstamped, got.Unstamped...)
		for _, w := range got.Writes.Written {
			payload.Written = append(payload.Written, w.Path)
		}
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	verb := "would stamp"
	if local.commit {
		verb = "stamped"
	}
	for _, st := range payload.Stamped {
		p.Printf("%-11s %s  commit %s by %s\n", verb, st.Ref, shortCommit(st.Verified.Commit), st.Verified.By)
	}
	for _, u := range payload.Unstamped {
		p.Printf("%-11s %s  %s\n", "unstamped", u.Ref, u.Reason)
	}
	summary := fmt.Sprintf("%s %s, %d left unstamped",
		plural(len(payload.Stamped), "requirement", "requirements"), verb, len(payload.Unstamped))
	if !local.commit {
		summary += "; nothing was written (run with --commit to write the stamps)"
	}
	p.Printf("%s\n", summary)
	return nil
}

// validRequirementArg reports whether an argument parses as a requirement
// ref, bare or "<KEY>/"-qualified; the vault checks the qualifier.
func validRequirementArg(arg string) bool {
	if _, bare, ok := strings.Cut(arg, "/"); ok {
		arg = bare
	}
	_, err := core.ParseRequirementRef(arg)
	return err == nil
}

// shortCommit abbreviates a commit id the way git log --oneline does.
func shortCommit(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
