import { EditorSelection, EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { afterEach, describe, expect, it } from 'vitest';

import { insertLink, toggleLinePrefix, wrapSelection } from '@/components/editor/markdown-commands';

let view: EditorView | null = null;

function makeView(doc: string, from: number, to = from): EditorView {
  view = new EditorView({
    state: EditorState.create({ doc, selection: EditorSelection.single(from, to) }),
  });
  return view;
}

afterEach(() => {
  view?.destroy();
  view = null;
});

describe('markdown commands', () => {
  it('wraps and unwraps the selection', () => {
    const editor = makeView('bold me', 0, 7);
    wrapSelection(editor, '**');
    expect(editor.state.doc.toString()).toBe('**bold me**');
    editor.dispatch({ selection: EditorSelection.single(0, editor.state.doc.length) });
    wrapSelection(editor, '**');
    expect(editor.state.doc.toString()).toBe('bold me');
  });

  it('toggles a heading prefix on the current line', () => {
    const editor = makeView('Title\nBody', 2);
    toggleLinePrefix(editor, '## ');
    expect(editor.state.doc.toString()).toBe('## Title\nBody');
    toggleLinePrefix(editor, '## ');
    expect(editor.state.doc.toString()).toBe('Title\nBody');
  });

  it('toggles a task-list prefix on every selected line', () => {
    const editor = makeView('one\ntwo', 0, 7);
    toggleLinePrefix(editor, '- [ ] ');
    expect(editor.state.doc.toString()).toBe('- [ ] one\n- [ ] two');
  });

  it('inserts a link around the selection', () => {
    const editor = makeView('docs', 0, 4);
    insertLink(editor);
    expect(editor.state.doc.toString()).toBe('[docs](url)');
  });
});
