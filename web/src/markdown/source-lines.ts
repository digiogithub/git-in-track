/**
 * Source-line addressing for block elements.
 *
 * Feedback notes quote a stretch of the rendered document and have to point
 * back at the Markdown it came from (docs/05-web-app.md §8.5). This plugin
 * stamps the first and last source line of every block element — paragraphs,
 * headings, list items, quotes, code blocks and table rows — so a DOM selection
 * can be turned into a line range of the body without re-parsing anything.
 *
 * Lines are 1-based and relative to the source handed to the pipeline, which
 * is the body the provider returned (front matter excluded): the same numbering
 * the core uses to anchor a note.
 */

import type { Element, Root } from 'hast';
import type { Plugin } from 'unified';
import { visit } from 'unist-util-visit';

/** The elements a selection is resolved to, innermost first. */
export const SOURCE_LINE_ELEMENTS = [
  'p',
  'li',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'blockquote',
  'pre',
  'table',
  'tr',
  'td',
  'th',
  'dt',
  'dd',
] as const;

const STAMPED = new Set<string>(SOURCE_LINE_ELEMENTS);

export const rehypeSourceLines: Plugin<[], Root> = () => (tree: Root) => {
  visit(tree, 'element', (node: Element) => {
    if (!STAMPED.has(node.tagName)) return;
    const start = node.position?.start.line;
    const end = node.position?.end.line;
    // A node a plugin synthesised has no position and simply stays unaddressed.
    if (typeof start !== 'number' || typeof end !== 'number') return;
    node.properties ??= {};
    node.properties['dataLineStart'] = String(start);
    node.properties['dataLineEnd'] = String(Math.max(start, end));
  });
};
