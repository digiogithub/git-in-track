package gitops

import (
	"encoding/json"
	"testing"
)

// The VCS-neutral vocabulary of GIT-US-0039 (ADR-022).
//
// These tests pin the two halves of the generalization: that the neutral
// concepts describe git exactly as they used to, and that the published JSON
// did not move under the refactor — the API of GIT-US-0020/0021/0022 is
// unchanged, and everything new is additive.

// TestGitLine pins the mapping from what git says HEAD is onto a line of work.
func TestGitLine(t *testing.T) {
	tests := []struct {
		name string
		head string
		want Line
	}{
		{
			name: "a checked-out branch is a named line that can be pushed",
			head: "main",
			want: Line{Name: "main", Kind: LineBranch, PushTarget: "main"},
		},
		{
			name: "a detached HEAD is anonymous and has nowhere to push",
			head: "HEAD",
			want: Line{Name: "HEAD", Anonymous: true, Kind: LineNone},
		},
		{
			name: "an unreadable HEAD is no line at all, and is not detached",
			head: "",
			want: Line{Kind: LineNone},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := gitLine(tc.head); got != tc.want {
				t.Errorf("gitLine(%q) = %+v, want %+v", tc.head, got, tc.want)
			}
		})
	}
}

// TestGitIntegration pins what a git backend reports about an unfinished
// integration: git undoes with `--abort` and carries on with `--continue`, and
// a jj backend will answer the same questions with the operation log instead.
func TestGitIntegration(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		want      Integration
	}{
		{
			name:      "nothing in progress is not unfinished",
			operation: "",
			want:      Integration{},
		},
		{
			name:      "a stopped rebase is unfinished, aborted and continued",
			operation: OpRebase,
			want: Integration{
				Operation: OpRebase, Unfinished: true,
				Undo: UndoAbort, Resume: ResumeContinue,
			},
		},
		{
			name:      "a stopped merge reports the same mechanisms",
			operation: OpMerge,
			want: Integration{
				Operation: OpMerge, Unfinished: true,
				Undo: UndoAbort, Resume: ResumeContinue,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := gitIntegration(tc.operation); got != tc.want {
				t.Errorf("gitIntegration(%q) = %+v, want %+v", tc.operation, got, tc.want)
			}
		})
	}
}

// TestUndoHint checks that the command a refusal names is the one the VCS that
// reported the integration actually uses, so a jj repository is never told to
// run `git rebase --abort`.
func TestUndoHint(t *testing.T) {
	tests := []struct {
		name string
		in   Integration
		want string
	}{
		{
			name: "git names its own abort",
			in:   gitIntegration(OpRebase),
			want: `, or "git rebase --abort"`,
		},
		{
			name: "an operation log names jj undo",
			in:   Integration{Operation: "rebase", Unfinished: true, Undo: UndoOperationLog},
			want: `, or "jj undo"`,
		},
		{
			name: "a backend that cannot undo names nothing",
			in:   Integration{Operation: "rebase", Unfinished: true, Undo: UndoNone},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := undoHint(tc.in); got != tc.want {
				t.Errorf("undoHint = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveStateFromNeutralFields proves that the headline state is derived
// from the neutral fields — an unfinished integration and an anonymous line of
// work — and keeps the precedence it had when those were git's operation
// marker and detached flag.
func TestResolveStateFromNeutralFields(t *testing.T) {
	tests := []struct {
		name   string
		status SyncStatus
		want   State
	}{
		{
			name: "conflicts outrank an unfinished integration",
			status: SyncStatus{
				Line:        gitLine("main"),
				Conflicted:  []Conflict{{Path: "a.md", Kind: ConflictContent}},
				Integration: gitIntegration(OpRebase),
			},
			want: StateConflicted,
		},
		{
			name:   "an unfinished integration outranks an anonymous line",
			status: SyncStatus{Line: gitLine("HEAD"), Integration: gitIntegration(OpMerge)},
			want:   StateInProgress,
		},
		{
			name:   "an anonymous line of work is detached",
			status: SyncStatus{Line: gitLine("HEAD")},
			want:   StateDetached,
		},
		{
			name: "a Jujutsu working copy is neither detached nor in progress",
			status: SyncStatus{
				Line:        jujutsuLine(),
				Jujutsu:     true,
				Integration: Integration{Undo: UndoOperationLog, Resume: ResumeNone},
			},
			want: StateJujutsu,
		},
		{
			name:   "a named line with a remote and no drift is up to date",
			status: SyncStatus{Line: gitLine("main"), Remote: "origin", Upstream: "origin/main"},
			want:   StateUpToDate,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.status
			st.resolveState()
			if st.State != tc.want {
				t.Errorf("state = %q, want %q", st.State, tc.want)
			}
		})
	}
}

// TestPublishedJSONIsUnchanged is the contract with every existing client: the
// refactor moved Go fields, not wire keys. `branch`, `detached` and `operation`
// are exactly where GIT-US-0021 put them.
func TestPublishedJSONIsUnchanged(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  map[string]any
	}{
		{
			name:  "a status still spells the line of work branch and detached",
			value: Status{Line: gitLine("HEAD"), Staged: []string{}},
			want:  map[string]any{"branch": "HEAD", "detached": true},
		},
		{
			name: "a sync status still spells the unfinished integration operation",
			value: SyncStatus{
				Line:        gitLine("main"),
				Integration: gitIntegration(OpRebase),
			},
			want: map[string]any{"branch": "main", "detached": false, "operation": "rebase"},
		},
		{
			name:  "a push result still spells its target branch",
			value: PushResult{Remote: "origin", Target: "main"},
			want:  map[string]any{"remote": "origin", "branch": "main"},
		},
		{
			name:  "the scoped-commit capability keeps its published key",
			value: Capabilities{Backend: "system", ScopedCommit: true},
			want:  map[string]any{"pathspecCommit": true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %#v, want %#v (in %s)", key, got[key], want, raw)
				}
			}
		})
	}
}
