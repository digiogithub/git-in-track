/**
 * Toolbar commands. Each one is a plain function over an `EditorView` so the
 * toolbar stays free of CodeMirror internals and the behaviour is unit
 * testable against a headless view.
 */

import type { ChangeSpec } from '@codemirror/state';
import { EditorSelection } from '@codemirror/state';
import type { EditorView } from '@codemirror/view';

/** Wraps every selection in `before`/`after`, or unwraps when already wrapped. */
export function wrapSelection(view: EditorView, before: string, after = before): void {
  const changes = view.state.changeByRange((range) => {
    const selected = view.state.sliceDoc(range.from, range.to);
    const alreadyWrapped =
      selected.length >= before.length + after.length &&
      selected.startsWith(before) &&
      selected.endsWith(after);
    if (alreadyWrapped) {
      const inner = selected.slice(before.length, selected.length - after.length);
      return {
        changes: { from: range.from, to: range.to, insert: inner },
        range: EditorSelection.range(range.from, range.from + inner.length),
      };
    }
    const insert = `${before}${selected}${after}`;
    return {
      changes: { from: range.from, to: range.to, insert },
      range: EditorSelection.range(range.from + before.length, range.from + before.length + selected.length),
    };
  });
  view.dispatch(changes, { scrollIntoView: true, userEvent: 'input' });
  view.focus();
}

/** Toggles a line prefix (`## `, `- [ ] `, …) on every line the selection touches. */
export function toggleLinePrefix(view: EditorView, prefix: string): void {
  const { state } = view;
  const changes: ChangeSpec[] = [];
  const seen = new Set<number>();
  for (const range of state.selection.ranges) {
    let pos = range.from;
    while (pos <= range.to) {
      const line = state.doc.lineAt(pos);
      if (!seen.has(line.number)) {
        seen.add(line.number);
        if (line.text.startsWith(prefix)) {
          changes.push({ from: line.from, to: line.from + prefix.length, insert: '' });
        } else {
          changes.push({ from: line.from, insert: prefix });
        }
      }
      if (line.to >= range.to) break;
      pos = line.to + 1;
    }
  }
  if (changes.length > 0) {
    view.dispatch({ changes, userEvent: 'input' });
  }
  view.focus();
}

/** Inserts a Markdown link around the selection (the selection becomes the text). */
export function insertLink(view: EditorView): void {
  const changes = view.state.changeByRange((range) => {
    const selected = view.state.sliceDoc(range.from, range.to) || 'text';
    const insert = `[${selected}](url)`;
    const urlStart = range.from + selected.length + 3;
    return {
      changes: { from: range.from, to: range.to, insert },
      range: EditorSelection.range(urlStart, urlStart + 3),
    };
  });
  view.dispatch(changes, { scrollIntoView: true, userEvent: 'input' });
  view.focus();
}
