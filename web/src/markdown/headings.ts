/**
 * Heading anchors and outline extraction.
 *
 * `rehype-slug` assigns the ids (github-slugger, so `#deep-link` fragments
 * behave the way they do on GitHub); this module adds the permalink affordance
 * and reads the outline back off the *sanitised* tree, so the table of contents
 * can never advertise an anchor that sanitisation removed.
 */

import type { Element, Root } from 'hast';
import type { Plugin } from 'unified';
import { visit } from 'unist-util-visit';

import { textContent } from '@/markdown/code';
import type { Heading } from '@/markdown/types';

const HEADINGS = new Set(['h1', 'h2', 'h3', 'h4', 'h5', 'h6']);
/** `mdast-util-to-hast` labels the footnote section; it is not page structure. */
const GENERATED_IDS = new Set(['footnote-label']);

export const rehypeHeadingAnchors: Plugin<[], Root> = () => (tree: Root) => {
  visit(tree, 'element', (node: Element) => {
    if (!HEADINGS.has(node.tagName)) return;
    const id = node.properties?.['id'];
    if (typeof id !== 'string' || id === '') return;

    node.properties ??= {};
    const existing = node.properties['className'];
    node.properties['className'] = [
      ...(Array.isArray(existing) ? existing.map((entry) => String(entry)) : []),
      'heading',
    ];
    node.children.push({
      type: 'element',
      tagName: 'a',
      properties: {
        href: `#${id}`,
        className: ['heading-anchor'],
        // Hidden from assistive technology and from the tab order: the anchor
        // is a mouse affordance, and it must not pollute the heading's
        // accessible name (the table of contents is the keyboard route).
        ariaHidden: 'true',
        tabIndex: -1,
      },
      children: [{ type: 'text', value: '#' }],
    });
  });
};

/** Document outline, in document order. Anchor markers are excluded from the text. */
export function collectHeadings(tree: Root): Heading[] {
  const headings: Heading[] = [];
  visit(tree, 'element', (node: Element) => {
    if (!HEADINGS.has(node.tagName)) return;
    const id = node.properties?.['id'];
    if (typeof id !== 'string' || id === '' || GENERATED_IDS.has(id)) return;

    const text = node.children
      .filter((child) => !(child.type === 'element' && isAnchorMarker(child)))
      .map((child) => (child.type === 'element' ? textContent(child) : childText(child)))
      .join('')
      .trim();

    headings.push({ depth: Number(node.tagName.slice(1)), id, text });
  });
  return headings;
}

function isAnchorMarker(node: Element): boolean {
  const className = node.properties?.['className'];
  return Array.isArray(className) && className.includes('heading-anchor');
}

function childText(node: { type: string; value?: string }): string {
  return node.type === 'text' ? (node.value ?? '') : '';
}
