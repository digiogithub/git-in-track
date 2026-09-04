package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
)

func TestExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "no error", err: nil, want: exitOK},
		{name: "plain error", err: errors.New("boom"), want: exitFailure},
		{name: "usage", err: usagef("bad flag"), want: exitUsage},
		{name: "not found", err: notFoundf("no repository"), want: exitNotFound},
		{name: "wrapped item not found", err: fmt.Errorf("get: %w", core.ErrItemNotFound), want: exitNotFound},
		{name: "missing file", err: fmt.Errorf("read: %w", core.ErrNotExist), want: exitNotFound},
		{name: "stale revision", err: &core.StaleRevisionError{ID: "DEMO-US-0001"}, want: exitConflict},
		{name: "denied transition", err: &core.TransitionError{ID: "DEMO-US-0001"}, want: exitConflict},
		{name: "duplicate id", err: fmt.Errorf("scan: %w", core.ErrDuplicateID), want: exitConflict},
		{name: "invalid configuration", err: config.FieldErrors{{Field: "server.port", Message: "out of range"}}, want: exitValidation},
		{name: "invalid item", err: &core.DiagnosticError{Diagnostic: core.Diagnostic{Code: core.CodeTitle}}, want: exitValidation},
		{name: "explicit code wins", err: fail(exitGit, core.ErrItemNotFound), want: exitGit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestExitErrorUnwraps(t *testing.T) {
	t.Parallel()

	err := fail(exitConflict, fmt.Errorf("move: %w", core.ErrTransitionDenied))
	if !errors.Is(err, core.ErrTransitionDenied) {
		t.Error("errors.Is lost the cause")
	}
	if err.Error() != "move: workflow transition denied" {
		t.Errorf("message = %q", err.Error())
	}
	if fail(exitFailure, nil) != nil {
		t.Error("fail wrapped a nil error")
	}
}

func TestArgValidators(t *testing.T) {
	t.Parallel()

	if err := exactArgs(1)(nil, nil); exitCode(err) != exitUsage {
		t.Errorf("exactArgs = %v", err)
	}
	if err := exactArgs(1)(nil, []string{"a"}); err != nil {
		t.Errorf("exactArgs = %v", err)
	}
	if err := rangeArgs(0, 1)(nil, []string{"a", "b"}); exitCode(err) != exitUsage {
		t.Errorf("rangeArgs = %v", err)
	}
	if err := noArgs(nil, []string{"a"}); exitCode(err) != exitUsage {
		t.Errorf("noArgs = %v", err)
	}
}
