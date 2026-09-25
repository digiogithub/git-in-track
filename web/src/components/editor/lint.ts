import type { Diagnostic as CodeMirrorDiagnostic } from '@codemirror/lint';
import type { Text } from '@codemirror/state';

/**
 * One finding the editor underlines: a 1-based line of the document, a
 * severity and a message. Whoever computes it — the core, for the live spec
 * lint (GIT-US-0132) — reports lines, not offsets; this module turns them into
 * the ranges CodeMirror draws.
 */
export type EditorLintFinding = {
  line: number;
  severity: 'error' | 'warning' | 'info';
  message: string;
  /** Shown before the message in the tooltip, e.g. the rule code. */
  source?: string;
};

/** Lints a document; called by the editor after typing pauses. */
export type EditorLintSource = (text: string) => Promise<EditorLintFinding[]>;

/**
 * Maps line findings onto CodeMirror ranges. A finding covers its line from
 * the first non-blank character to the end, so the underline sits under the
 * text rather than the indentation; a blank line gets a one-character range at
 * its start so the finding is still reachable. A line past the end of the
 * document (the document changed since the lint ran) clamps to the last line,
 * and a severity CodeMirror does not know is dropped.
 */
export function toCodeMirrorDiagnostics(
  doc: Text,
  findings: readonly EditorLintFinding[],
): CodeMirrorDiagnostic[] {
  const out: CodeMirrorDiagnostic[] = [];
  for (const finding of findings) {
    if (!isSeverity(finding.severity) || !Number.isFinite(finding.line)) continue;
    const number = Math.min(Math.max(1, Math.trunc(finding.line)), doc.lines);
    const line = doc.line(number);
    const indent = line.text.length - line.text.trimStart().length;
    const from = line.from + (indent === line.text.length ? 0 : indent);
    const to = Math.max(line.to, Math.min(from + 1, doc.length));
    out.push({
      from,
      to,
      severity: finding.severity,
      message: finding.message,
      ...(finding.source ? { source: finding.source } : {}),
    });
  }
  return out;
}

function isSeverity(value: string): value is EditorLintFinding['severity'] {
  return value === 'error' || value === 'warning' || value === 'info';
}
