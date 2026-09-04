/** Small hast helpers shared by the mermaid and highlighting plugins. */

import type { Element, Root } from 'hast';
import { visit } from 'unist-util-visit';

export const MERMAID_LANGUAGE = 'mermaid';

export function classNames(node: Element): string[] {
  const value = node.properties?.['className'];
  return Array.isArray(value) ? value.map((entry) => String(entry)) : [];
}

/** Reads the language of a `<pre><code class="language-x">` block, or `null`. */
export function codeLanguage(node: Element): string | null {
  if (node.tagName !== 'pre') return null;
  const code = node.children.find(
    (c): c is Element => c.type === 'element' && c.tagName === 'code',
  );
  if (!code) return null;
  const prefixed = classNames(code).find((c) => c.startsWith('language-'));
  return prefixed ? prefixed.slice('language-'.length).toLowerCase() : null;
}

/** Concatenated text content of a hast subtree. */
export function textContent(node: Element | Root): string {
  let out = '';
  visit(node, 'text', (text) => {
    out += text.value;
  });
  return out;
}

/**
 * True when the tree has a fence worth highlighting. Lives outside
 * `highlight.ts` so the pipeline can answer the question without downloading
 * the Shiki chunk.
 */
export function hasHighlightableCode(tree: Root): boolean {
  let found = false;
  visit(tree, 'element', (node: Element) => {
    const language = codeLanguage(node);
    if (language && language !== MERMAID_LANGUAGE) found = true;
  });
  return found;
}
