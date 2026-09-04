package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
)

// The process exit codes of docs/07-cli-and-api.md, section 4.
const (
	exitOK         = 0
	exitFailure    = 1
	exitUsage      = 2
	exitValidation = 3
	exitNotFound   = 4
	exitConflict   = 5
	exitGit        = 6
)

// exitError is an error carrying the exit code the process should end with.
type exitError struct {
	code int
	err  error
}

// Error implements the error interface.
func (e *exitError) Error() string { return e.err.Error() }

// Unwrap exposes the cause, so that errors.Is still classifies it.
func (e *exitError) Unwrap() error { return e.err }

// fail wraps an error with an explicit exit code.
func fail(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: code, err: err}
}

// failf builds an error with an explicit exit code.
func failf(code int, format string, args ...any) error {
	return &exitError{code: code, err: fmt.Errorf(format, args...)}
}

// usageError marks an error as a bad invocation (exit 2).
func usageError(err error) error { return fail(exitUsage, err) }

// usagef reports a bad invocation.
func usagef(format string, args ...any) error { return failf(exitUsage, format, args...) }

// notFoundf reports something the workspace does not contain (exit 4).
func notFoundf(format string, args ...any) error { return failf(exitNotFound, format, args...) }

// exitCode maps an error onto the documented exit codes. An explicit code wins;
// otherwise the core sentinels decide, so that a stale revision is always 5 and
// a missing item is always 4 whichever command reported it.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var coded *exitError
	if errors.As(err, &coded) {
		return coded.code
	}
	var diag *core.DiagnosticError
	if errors.As(err, &diag) {
		return exitValidation
	}
	switch {
	case errors.Is(err, core.ErrItemNotFound), errors.Is(err, core.ErrNotExist):
		return exitNotFound
	case errors.Is(err, core.ErrRevMismatch), errors.Is(err, core.ErrTransitionDenied), errors.Is(err, core.ErrDuplicateID):
		return exitConflict
	case errors.Is(err, config.ErrInvalid), errors.Is(err, core.ErrInvalidFrontMatter):
		return exitValidation
	default:
		return exitFailure
	}
}

// exactArgs is cobra.ExactArgs with the usage exit code attached.
func exactArgs(n int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return usagef("accepts %d argument(s), received %d", n, len(args))
		}
		return nil
	}
}

// rangeArgs is cobra.RangeArgs with the usage exit code attached.
func rangeArgs(minArgs, maxArgs int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) < minArgs || len(args) > maxArgs {
			return usagef("accepts between %d and %d argument(s), received %d", minArgs, maxArgs, len(args))
		}
		return nil
	}
}

// noArgs is cobra.NoArgs with the usage exit code attached.
func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usagef("unexpected argument %q", args[0])
	}
	return nil
}
