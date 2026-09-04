import {
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completionKeymap,
  type Completion,
  type CompletionContext,
  type CompletionResult,
} from '@codemirror/autocomplete';
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
import { markdown, markdownLanguage } from '@codemirror/lang-markdown';
import { yaml as yamlLanguage } from '@codemirror/lang-yaml';
import {
  LanguageDescription,
  bracketMatching,
  defaultHighlightStyle,
  indentOnInput,
  syntaxHighlighting,
} from '@codemirror/language';
import { highlightSelectionMatches, searchKeymap } from '@codemirror/search';
import { Compartment, EditorState } from '@codemirror/state';
import { oneDark } from '@codemirror/theme-one-dark';
import {
  EditorView,
  drawSelection,
  highlightActiveLine,
  highlightSpecialChars,
  keymap,
  placeholder as placeholderExt,
} from '@codemirror/view';
import { Bold, Code, Heading2, Italic, Link2, ListChecks, PanelRightClose, PanelRightOpen } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';

import { insertLink, toggleLinePrefix, wrapSelection } from '@/components/editor/markdown-commands';
import { MarkdownPreview } from '@/components/editor/MarkdownPreview';
import { useIsDarkTheme } from '@/components/editor/theme';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/cn';

export type EditorReference = { id: string; title: string };

export type MarkdownEditorProps = {
  value: string;
  onChange: (value: string) => void;
  /** Accessible name of the text area. */
  label: string;
  /** Debounce applied to `onChange`; the buffer is flushed on blur and unmount. */
  debounceMs?: number;
  readOnly?: boolean;
  placeholder?: string;
  className?: string;
  /** Item ids offered as `[[wikilink]]` completions. */
  references?: EditorReference[];
};

const lightTheme = EditorView.theme({
  '&': { backgroundColor: 'transparent', color: 'inherit' },
  '.cm-content': { fontFamily: 'var(--font-mono)', fontSize: '13px', padding: '10px 12px' },
  '.cm-gutters': { display: 'none' },
  '&.cm-focused': { outline: 'none' },
});

function referenceCompletions(references: EditorReference[]) {
  const options: Completion[] = references.map((ref) => ({
    label: ref.id,
    detail: ref.title,
    type: 'variable',
  }));
  return (context: CompletionContext): CompletionResult | null => {
    const before = context.matchBefore(/\[\[[\w-]*/);
    if (!before || (before.from === before.to && !context.explicit)) return null;
    return { from: before.from + 2, options, validFor: /^[\w-]*$/ };
  };
}

/**
 * CodeMirror 6 Markdown editor (docs/05-web-app.md §8).
 *
 * Controlled through `value`/`onChange`: local typing is debounced, and an
 * external change to `value` replaces the document without losing focus.
 */
export function MarkdownEditor({
  value,
  onChange,
  label,
  debounceMs = 300,
  readOnly = false,
  placeholder,
  className,
  references = [],
}: MarkdownEditorProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  const localValueRef = useRef(value);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const themeCompartment = useRef(new Compartment()).current;
  const editableCompartment = useRef(new Compartment()).current;
  const completionCompartment = useRef(new Compartment()).current;
  const [showPreview, setShowPreview] = useState(false);
  const isDark = useIsDarkTheme();

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  const flush = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
      onChangeRef.current(localValueRef.current);
    }
  }, []);

  const themeExtension = useMemo(
    () => (isDark ? [oneDark, lightTheme] : [syntaxHighlighting(defaultHighlightStyle), lightTheme]),
    [isDark],
  );

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return undefined;

    const view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: localValueRef.current,
        extensions: [
          history(),
          drawSelection(),
          highlightSpecialChars(),
          highlightActiveLine(),
          highlightSelectionMatches(),
          bracketMatching(),
          closeBrackets(),
          indentOnInput(),
          EditorView.lineWrapping,
          keymap.of([
            ...closeBracketsKeymap,
            ...defaultKeymap,
            ...historyKeymap,
            ...searchKeymap,
            ...completionKeymap,
          ]),
          markdown({
            base: markdownLanguage,
            codeLanguages: [
              LanguageDescription.of({ name: 'yaml', alias: ['yml'], load: () => Promise.resolve(yamlLanguage()) }),
            ],
          }),
          EditorView.contentAttributes.of({ 'aria-label': label, role: 'textbox' }),
          themeCompartment.of([]),
          editableCompartment.of([]),
          completionCompartment.of([]),
          ...(placeholder ? [placeholderExt(placeholder)] : []),
          EditorView.updateListener.of((update) => {
            if (!update.docChanged) return;
            localValueRef.current = update.state.doc.toString();
            if (timerRef.current !== null) clearTimeout(timerRef.current);
            timerRef.current = setTimeout(() => {
              timerRef.current = null;
              onChangeRef.current(localValueRef.current);
            }, debounceMs);
          }),
          EditorView.domEventHandlers({
            blur: () => {
              flush();
              return false;
            },
          }),
        ],
      }),
    });
    viewRef.current = view;

    return () => {
      flush();
      view.destroy();
      viewRef.current = null;
    };
    // The view is created once; `label`, `placeholder` and `debounceMs` are
    // configuration, not reactive state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    viewRef.current?.dispatch({ effects: themeCompartment.reconfigure(themeExtension) });
  }, [themeCompartment, themeExtension]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: editableCompartment.reconfigure([
        EditorView.editable.of(!readOnly),
        EditorState.readOnly.of(readOnly),
      ]),
    });
  }, [editableCompartment, readOnly]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: completionCompartment.reconfigure(
        references.length > 0
          ? [autocompletion({ override: [referenceCompletions(references)] })]
          : [],
      ),
    });
  }, [completionCompartment, references]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    if (value === localValueRef.current) return;
    localValueRef.current = value;
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: value },
      selection: { anchor: Math.min(view.state.selection.main.anchor, value.length) },
    });
  }, [value]);

  const run = (fn: (view: EditorView) => void) => () => {
    const view = viewRef.current;
    if (!view || readOnly) return;
    fn(view);
    flush();
  };

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <div className="flex flex-wrap items-center gap-1" role="toolbar" aria-label={`${label} formatting`}>
        <ToolbarButton label="Bold" onClick={run((v) => { wrapSelection(v, '**'); })}>
          <Bold aria-hidden="true" className="h-4 w-4" />
        </ToolbarButton>
        <ToolbarButton label="Italic" onClick={run((v) => { wrapSelection(v, '_'); })}>
          <Italic aria-hidden="true" className="h-4 w-4" />
        </ToolbarButton>
        <ToolbarButton label="Heading" onClick={run((v) => { toggleLinePrefix(v, '## '); })}>
          <Heading2 aria-hidden="true" className="h-4 w-4" />
        </ToolbarButton>
        <ToolbarButton label="Task list item" onClick={run((v) => { toggleLinePrefix(v, '- [ ] '); })}>
          <ListChecks aria-hidden="true" className="h-4 w-4" />
        </ToolbarButton>
        <ToolbarButton label="Link" onClick={run((v) => { insertLink(v); })}>
          <Link2 aria-hidden="true" className="h-4 w-4" />
        </ToolbarButton>
        <ToolbarButton label="Code" onClick={run((v) => { wrapSelection(v, '`'); })}>
          <Code aria-hidden="true" className="h-4 w-4" />
        </ToolbarButton>
        <div className="ml-auto">
          <Button
            variant="ghost"
            size="sm"
            aria-pressed={showPreview}
            onClick={() => {
              flush();
              setShowPreview((current) => !current);
            }}
          >
            {showPreview ? (
              <PanelRightClose aria-hidden="true" className="h-4 w-4" />
            ) : (
              <PanelRightOpen aria-hidden="true" className="h-4 w-4" />
            )}
            {showPreview ? 'Hide preview' : 'Preview'}
          </Button>
        </div>
      </div>

      <div className={cn('grid gap-3', showPreview ? 'md:grid-cols-2' : 'grid-cols-1')}>
        <div
          ref={hostRef}
          data-testid="markdown-editor"
          className="min-h-[18rem] overflow-auto rounded-md border border-input bg-background focus-within:ring-2 focus-within:ring-ring"
        />
        {showPreview ? <MarkdownPreview value={value} className="min-h-[18rem]" /> : null}
      </div>
    </div>
  );
}

function ToolbarButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <Button variant="ghost" size="icon" aria-label={label} title={label} onClick={onClick}>
      {children}
    </Button>
  );
}
