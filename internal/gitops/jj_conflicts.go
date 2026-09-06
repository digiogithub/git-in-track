package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// The structured conflict surface of a Jujutsu repository (GIT-US-0040).
//
// git reads the three sides of a conflict from the index stages a stopped
// integration left behind. jj has no index and no stopped integration: the
// rebase completed, and the conflict is recorded *inside* the resulting commit.
// A non-colocated repository has no git index to fall back on either, so
// materializing the conflict out of the commit is the only way to read it, and
// it is the way that works for both layouts.
//
// `jj file show -r @ <path>` renders that recorded conflict in jj's own marker
// dialect: a `%%%%%%%` section holding a diff from the merge base to the first
// side, and a `+++++++` section holding the second side verbatim. The three
// sides are reconstructed from it, and ConflictVersions.Markers reports `jj` so
// that no reader parses git's `<<<<<<<`/`=======`/`>>>>>>>` grammar over it.

// The markers jj materializes a conflict with (docs/06 section 14.5).
const (
	jujutsuConflictStart = "<<<<<<<"
	jujutsuConflictEnd   = ">>>>>>>"
	jujutsuConflictDiff  = "%%%%%%%"
	jujutsuConflictSnap  = "+++++++"
	// jujutsuConflictCont is the second line of a `%%%%%%%` header, the one
	// that names the side the diff runs to. It is a header, not content.
	jujutsuConflictCont = `\\\\\\\`
)

// ConflictFile produces the three sides of one conflicted path.
func (b *jujutsuBackend) ConflictFile(ctx context.Context, path string) (ConflictVersions, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return ConflictVersions{}, failf("conflict", CodeNotFound, "no path was given")
	}
	conflicted, err := b.conflicts(ctx)
	if err != nil {
		return ConflictVersions{}, err
	}
	conflict, ok := conflictOf(conflicted, path)
	if !ok {
		return ConflictVersions{}, notConflicted("conflict", path, b.path)
	}

	materialized, err := b.run(ctx, "file", "show", "-r", "@", path)
	if err != nil {
		return ConflictVersions{}, wrap("conflict", CodeCommitFailed, err,
			"read the conflicted content of %s in %s", path, b.path)
	}
	out := parseJujutsuConflict(materialized)
	out.Path, out.Kind = path, conflict.Kind

	// A conflicted commit with a single parent is a rebased revision: jj
	// replayed the user's own commit onto a destination, so the `+++++++` side
	// is the user's work and the side the diff runs to is the incoming one.
	// That is the same inversion git's rebase produces, and Ours must always be
	// the user's side (ADR-022).
	parents, parentsErr := b.parentCount(ctx)
	if parentsErr != nil {
		return ConflictVersions{}, parentsErr
	}
	if parents < 2 {
		out.swapSides()
	}

	if raw, readErr := os.ReadFile(filepath.Join(b.path, filepath.FromSlash(path))); readErr == nil {
		out.Working = string(raw)
	}
	out.Binary = isBinary(out.Base) || isBinary(out.Ours) || isBinary(out.Theirs)
	return out, nil
}

// parentCount reports how many parents the working-copy commit has.
func (b *jujutsuBackend) parentCount(ctx context.Context) (int, error) {
	raw, err := b.run(ctx, "log", "--no-graph",
		"-T", `self.parents().map(|p| "p").join("`+jujutsuFieldSeparator+`")`, "-r", "@")
	if err != nil {
		return 0, wrap("conflict", CodeCommitFailed, err,
			"read the parents of the working copy of %s", b.path)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return strings.Count(raw, jujutsuFieldSeparator) + 1, nil
}

// parseJujutsuConflict reconstructs the three sides of a materialized conflict.
//
// Everything outside a conflict region belongs to all three sides. Inside one,
// the `%%%%%%%` section is a diff whose `-` and context lines rebuild the merge
// base and whose `+` and context lines rebuild the first side, and the
// `+++++++` section is the second side verbatim.
//
// The reconstruction is deliberately total: a shape this parser does not
// recognize — a conflict of more than two sides, or a binary one jj could not
// materialize — yields whatever it could read, and the caller still has
// ConflictVersions.Working, which is the "edit manually" escape hatch.
func parseJujutsuConflict(materialized string) ConflictVersions {
	var base, first, second strings.Builder
	const (
		outside = iota
		inDiff
		inSnapshot
	)
	state := outside
	for _, line := range strings.Split(strings.TrimSuffix(materialized, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, jujutsuConflictStart):
			state = inDiff
			continue
		case strings.HasPrefix(line, jujutsuConflictEnd):
			state = outside
			continue
		case strings.HasPrefix(line, jujutsuConflictDiff):
			state = inDiff
			continue
		case strings.HasPrefix(line, jujutsuConflictSnap):
			state = inSnapshot
			continue
		case state == inDiff && strings.HasPrefix(line, jujutsuConflictCont):
			continue
		}
		switch state {
		case outside:
			writeLine(&base, line)
			writeLine(&first, line)
			writeLine(&second, line)
		case inDiff:
			writeDiffLine(&base, &first, line)
		case inSnapshot:
			writeLine(&second, line)
		}
	}
	out := ConflictVersions{
		Base: base.String(), Ours: first.String(), Theirs: second.String(),
		Markers: MarkersJujutsu,
	}
	// A side jj materialized as empty is a side the conflict deletes. jj draws
	// no distinction between "deleted" and "empty" in the rendered form, and
	// the Kind the caller sets carries the deletion, which is what the resolver
	// switches on.
	out.HasBase, out.HasOurs, out.HasTheirs = out.Base != "", out.Ours != "", out.Theirs != ""
	return out
}

// writeLine appends one whole line to a side.
func writeLine(side *strings.Builder, line string) {
	side.WriteString(line)
	side.WriteByte('\n')
}

// writeDiffLine dispatches one line of a `%%%%%%%` section: `-` belongs to the
// merge base, `+` to the first side, and a context line to both.
func writeDiffLine(base, first *strings.Builder, line string) {
	if line == "" {
		return
	}
	body := line[1:]
	switch line[0] {
	case '-':
		writeLine(base, body)
	case '+':
		writeLine(first, body)
	default:
		writeLine(base, body)
		writeLine(first, body)
	}
}
