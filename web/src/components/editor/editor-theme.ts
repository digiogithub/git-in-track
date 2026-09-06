/**
 * One CodeMirror theme for both colour schemes.
 *
 * The editor used to swap between `oneDark` and the default highlight style,
 * which meant a second palette (a cold blue one) living inside a warm app. It
 * does not any more: every colour below is a design token read at paint time,
 * so the editor follows the theme for the same reason the rest of the app
 * does — the variables under `<html>` changed — and there is no dark branch to
 * keep in sync. See docs/13-design-system.md.
 */

import { HighlightStyle } from '@codemirror/language';
import { EditorView } from '@codemirror/view';
import { tags as t } from '@lezer/highlight';

export const editorTheme = EditorView.theme({
  '&': {
    backgroundColor: 'transparent',
    color: 'hsl(var(--foreground))',
  },
  '.cm-content': {
    fontFamily: 'var(--font-mono)',
    fontSize: '13px',
    lineHeight: '1.65',
    padding: '10px 12px',
    caretColor: 'hsl(var(--accent))',
  },
  '.cm-gutters': { display: 'none' },
  '&.cm-focused': { outline: 'none' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'hsl(var(--accent))', borderLeftWidth: '2px' },
  '.cm-placeholder': { color: 'hsl(var(--subtle-foreground))' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: 'hsl(var(--accent) / 0.22)',
  },
  '.cm-activeLine': { backgroundColor: 'hsl(var(--surface-muted) / 0.5)' },
  '.cm-selectionMatch': { backgroundColor: 'hsl(var(--accent) / 0.14)' },
  '.cm-matchingBracket, &.cm-focused .cm-matchingBracket': {
    backgroundColor: 'hsl(var(--accent) / 0.18)',
    outline: 'none',
  },
  '.cm-tooltip': {
    backgroundColor: 'hsl(var(--popover))',
    border: '1px solid hsl(var(--border))',
    borderRadius: 'var(--radius)',
    boxShadow: 'var(--shadow-md)',
    color: 'hsl(var(--popover-foreground))',
    overflow: 'hidden',
  },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li': {
    fontFamily: 'var(--font-mono)',
    fontSize: '12px',
    padding: '3px 8px',
  },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]': {
    backgroundColor: 'hsl(var(--accent-subtle))',
    color: 'hsl(var(--foreground))',
  },
  '.cm-completionDetail': { color: 'hsl(var(--muted-foreground))', fontStyle: 'normal' },
});

/**
 * Markdown and YAML are the only two grammars the editor loads, so the scale is
 * short on purpose: structure (headings, emphasis) is carried by weight, and
 * colour is spent only on the things you scan for — links, code and keys.
 */
export const editorHighlightStyle = HighlightStyle.define([
  {
    tag: [t.heading, t.heading1, t.heading2, t.heading3],
    fontWeight: '600',
    color: 'hsl(var(--foreground))',
  },
  { tag: t.strong, fontWeight: '600', color: 'hsl(var(--foreground))' },
  { tag: t.emphasis, fontStyle: 'italic' },
  { tag: t.strikethrough, textDecoration: 'line-through', color: 'hsl(var(--subtle-foreground))' },
  {
    tag: [t.link, t.url],
    color: 'hsl(var(--accent))',
    textDecoration: 'underline',
    textUnderlineOffset: '2px',
  },
  { tag: [t.monospace, t.special(t.string)], color: 'hsl(var(--info))' },
  { tag: [t.quote], color: 'hsl(var(--muted-foreground))', fontStyle: 'italic' },
  { tag: [t.list, t.processingInstruction], color: 'hsl(var(--accent))' },
  {
    tag: [t.propertyName, t.definition(t.propertyName), t.labelName],
    color: 'hsl(var(--status-in-review))',
  },
  { tag: [t.string, t.character], color: 'hsl(var(--success))' },
  { tag: [t.number, t.bool, t.null], color: 'hsl(var(--warning))' },
  { tag: [t.keyword, t.operatorKeyword], color: 'hsl(var(--status-in-review))' },
  { tag: [t.comment, t.meta], color: 'hsl(var(--subtle-foreground))', fontStyle: 'italic' },
  { tag: [t.invalid], color: 'hsl(var(--destructive))' },
  { tag: [t.contentSeparator], color: 'hsl(var(--border-strong))' },
]);
