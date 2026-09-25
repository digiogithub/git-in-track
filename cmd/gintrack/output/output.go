// Package output renders what the gintrack commands have to say.
//
// Every command prints through a Printer, which switches between the aligned
// tables a human reads and the JSON a script pipes into jq. Keeping both
// renderings here is what makes `--json` uniform across the command tree, and
// keeps the command files free of formatting code.
//
// The contract of docs/07-cli-and-api.md section 4 is that with --json the
// machine-readable payload goes to stdout and every human line goes to stderr,
// so `gintrack item list --json | jq` is always safe.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// columnGap is the number of spaces between two table columns.
const columnGap = 2

// Table writes an aligned, dependency-free table. Columns are padded to the
// widest cell, the last column is never padded, and a nil or empty row set
// prints the header alone.
func Table(w io.Writer, headers []string, rows [][]string) error {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	writeRow(&b, headers, widths)
	for _, row := range rows {
		writeRow(&b, row, widths)
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write the table: %w", err)
	}
	return nil
}

// writeRow appends one padded row, without trailing spaces.
func writeRow(b *strings.Builder, cells []string, widths []int) {
	last := lastNonEmpty(cells)
	for i, cell := range cells {
		if i > last {
			break
		}
		b.WriteString(cell)
		if i == last {
			continue
		}
		n := utf8.RuneCountInString(cell)
		width := n
		if i < len(widths) && widths[i] > width {
			width = widths[i]
		}
		b.WriteString(strings.Repeat(" ", width-n+columnGap))
	}
	b.WriteString("\n")
}

// lastNonEmpty returns the index of the last cell worth printing, so that a row
// ending in empty cells does not end in a run of spaces.
func lastNonEmpty(cells []string) int {
	last := -1
	for i, c := range cells {
		if c != "" {
			last = i
		}
	}
	return last
}

// JSON writes an indented, deterministic JSON document followed by a newline.
// HTML escaping is off so that a title with an ampersand reads the same on the
// terminal as in the file it came from.
func JSON(w io.Writer, v any) error {
	return encodeJSON(w, v, "  ")
}

// CompactJSON writes v as one line of JSON followed by a newline, with the
// same escaping as JSON. It is for payloads whose size is part of the
// contract, such as a token-budgeted report.
func CompactJSON(w io.Writer, v any) error {
	return encodeJSON(w, v, "")
}

func encodeJSON(w io.Writer, v any, indent string) error {
	enc := json.NewEncoder(w)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return nil
}

// Printer renders a command's output in the mode the flags asked for.
//
// The zero value is not usable; call New.
type Printer struct {
	out     io.Writer
	err     io.Writer
	asJSON  bool
	quiet   bool
	compact bool
}

// New returns a printer writing to out, with human notes going to notes.
func New(out, notes io.Writer, asJSON bool) *Printer {
	return &Printer{out: out, err: notes, asJSON: asJSON}
}

// SetQuiet suppresses the human lines Printf and Line write.
func (p *Printer) SetQuiet(quiet bool) { p.quiet = quiet }

// SetCompact makes JSON print one compact line instead of indented output.
func (p *Printer) SetCompact(compact bool) { p.compact = compact }

// JSONMode reports whether the printer emits JSON.
func (p *Printer) JSONMode() bool { return p.asJSON }

// Out returns the stream the payload goes to.
func (p *Printer) Out() io.Writer { return p.out }

// Notes returns the stream human lines go to: stderr in JSON mode, stdout
// otherwise.
func (p *Printer) Notes() io.Writer {
	if p.asJSON {
		return p.err
	}
	return p.out
}

// Table prints a table, or nothing at all in JSON mode.
func (p *Printer) Table(headers []string, rows [][]string) error {
	if p.asJSON || p.quiet {
		return nil
	}
	return Table(p.out, headers, rows)
}

// JSON prints a payload, or nothing at all in text mode.
func (p *Printer) JSON(v any) error {
	if !p.asJSON {
		return nil
	}
	if p.compact {
		return CompactJSON(p.out, v)
	}
	return JSON(p.out, v)
}

// Printf writes a human line to the notes stream. A failed write to a terminal
// is not worth an exit code, so the error is dropped on purpose.
func (p *Printer) Printf(format string, args ...any) {
	if p.quiet {
		return
	}
	_, _ = fmt.Fprintf(p.Notes(), format, args...)
}

// Line writes one human line to the notes stream.
func (p *Printer) Line(s string) { p.Printf("%s\n", s) }

// Warnf writes a warning to the error stream, whatever the mode.
func (p *Printer) Warnf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.err, format, args...)
}
