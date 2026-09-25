import { Text } from '@codemirror/state';
import { describe, expect, it } from 'vitest';

import { toCodeMirrorDiagnostics, type EditorLintFinding } from '@/components/editor/lint';

const doc = Text.of(['## Requirements', '', '  The system SHALL be fast.', 'last line']);

function finding(overrides: Partial<EditorLintFinding>): EditorLintFinding {
  return { line: 1, severity: 'warning', message: 'm', ...overrides };
}

describe('toCodeMirrorDiagnostics', () => {
  it('covers the line from its first non-blank character to its end', () => {
    const [d] = toCodeMirrorDiagnostics(doc, [finding({ line: 3, source: 'LINT-REQ-VAGUE' })]);
    const line = doc.line(3);
    expect(d).toMatchObject({
      from: line.from + 2,
      to: line.to,
      severity: 'warning',
      message: 'm',
      source: 'LINT-REQ-VAGUE',
    });
  });

  it('gives a blank line a one-character range so the finding stays reachable', () => {
    const [d] = toCodeMirrorDiagnostics(doc, [finding({ line: 2 })]);
    expect(d?.from).toBe(doc.line(2).from);
    expect(d?.to).toBe(doc.line(2).from + 1);
  });

  it('clamps a line past the end of a document that moved on', () => {
    const [d] = toCodeMirrorDiagnostics(doc, [finding({ line: 99 })]);
    expect(d?.from).toBe(doc.line(4).from);
    expect(d?.to).toBe(doc.line(4).to);
  });

  it('keeps error and warning apart and drops a severity CodeMirror does not know', () => {
    const got = toCodeMirrorDiagnostics(doc, [
      finding({ line: 1, severity: 'error' }),
      finding({ line: 3, severity: 'warning' }),
      finding({ line: 4, severity: 'off' as EditorLintFinding['severity'] }),
    ]);
    expect(got.map((d) => d.severity)).toEqual(['error', 'warning']);
  });
});
