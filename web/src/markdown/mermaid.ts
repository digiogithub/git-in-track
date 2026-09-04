/**
 * `rehype-mermaid-placeholder` — never renders a diagram at parse time.
 *
 * A ```mermaid fence becomes `<pre class="mermaid" data-mermaid>` holding the
 * verbatim source. That element is the graceful-degradation state (the source
 * stays readable when the diagram fails or JavaScript is off) and the hook the
 * React `MermaidBlock` uses to dynamically import `mermaid` on first visibility.
 */

import type { Element, Root } from 'hast';
import type { Plugin } from 'unified';
import { visit } from 'unist-util-visit';

import { codeLanguage, MERMAID_LANGUAGE, textContent } from '@/markdown/code';

export type RehypeMermaidResult = { found: boolean };

export const rehypeMermaid: Plugin<[RehypeMermaidResult?], Root> =
  (result = { found: false }) =>
  (tree: Root) => {
    visit(tree, 'element', (node: Element, index, parent) => {
      if (!parent || index === undefined) return;
      if (codeLanguage(node) !== MERMAID_LANGUAGE) return;

      result.found = true;
      const replacement: Element = {
        type: 'element',
        tagName: 'pre',
        properties: { className: ['mermaid'], dataMermaid: 'true' },
        children: [{ type: 'text', value: textContent(node).replace(/\n$/, '') }],
      };
      parent.children[index] = replacement;
    });
  };
