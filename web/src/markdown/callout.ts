/**
 * `remark-callout` — GitHub alerts and Obsidian callouts.
 *
 *   > [!NOTE]
 *   > Useful information.
 *
 *   > [!info]- Collapsed by default
 *   > Body.
 *
 * GitHub supports five types; Obsidian adds a longer vocabulary plus the
 * `+`/`-` fold markers. Only known types are transformed, so an ordinary
 * blockquote that happens to start with brackets stays a blockquote, and the
 * emitted `callout-*` class can never be attacker-chosen.
 */

import type { Blockquote, Paragraph, Root, RootContent } from 'mdast';
import type { Plugin } from 'unified';
import { visit } from 'unist-util-visit';

// Loads the `hName`/`hProperties` augmentation of `mdast`'s `Data`.
import type {} from 'mdast-util-to-hast';

/** GitHub alert types first, then the Obsidian additions we render. */
export const CALLOUT_TYPES = [
  'note',
  'tip',
  'important',
  'warning',
  'caution',
  'info',
  'todo',
  'abstract',
  'summary',
  'question',
  'success',
  'failure',
  'danger',
  'bug',
  'example',
  'quote',
] as const;

export type CalloutType = (typeof CALLOUT_TYPES)[number];

const KNOWN = new Set<string>(CALLOUT_TYPES);
const MARKER = /^\[!([A-Za-z][A-Za-z0-9_-]*)\]([+-]?)[ \t]*(.*)$/;

function titleCase(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export type Callout = {
  type: CalloutType;
  title: string;
  collapsible: boolean;
  open: boolean;
};

/** Reads the callout marker from a blockquote, or `null` when there is none. */
export function readCalloutMarker(node: Blockquote): (Callout & { rest: string }) | null {
  const first = node.children[0];
  if (!first || first.type !== 'paragraph') return null;
  const text = first.children[0];
  if (!text || text.type !== 'text') return null;

  const newline = text.value.indexOf('\n');
  const head = newline === -1 ? text.value : text.value.slice(0, newline);
  const rest = newline === -1 ? '' : text.value.slice(newline + 1);

  const match = MARKER.exec(head);
  if (!match) return null;

  const type = (match[1] ?? '').toLowerCase();
  if (!KNOWN.has(type)) return null;

  const fold = match[2] ?? '';
  const title = (match[3] ?? '').trim();

  return {
    type: type as CalloutType,
    title: title || titleCase(type),
    collapsible: fold !== '',
    open: fold === '+',
    rest,
  };
}

export const remarkCallout: Plugin<[], Root> = () => (tree: Root) => {
  visit(tree, 'blockquote', (node: Blockquote) => {
    const marker = readCalloutMarker(node);
    if (!marker) return;

    const first = node.children[0] as Paragraph;
    const text = first.children[0];
    if (!text || text.type !== 'text') return;

    // Drop the marker line; whatever followed it on the same paragraph stays.
    if (marker.rest) {
      text.value = marker.rest;
    } else {
      first.children.shift();
    }
    const body: RootContent[] =
      first.children.length === 0 ? node.children.slice(1) : [...node.children];

    const titleNode: Paragraph = {
      type: 'paragraph',
      children: [{ type: 'text', value: marker.title }],
      data: {
        hName: marker.collapsible ? 'summary' : 'div',
        hProperties: { className: ['callout-title'] },
      },
    };

    const bodyNode: Blockquote = {
      type: 'blockquote',
      children: body as Blockquote['children'],
      data: { hName: 'div', hProperties: { className: ['callout-body'] } },
    };

    node.children = [titleNode, bodyNode] as Blockquote['children'];
    node.data = {
      hName: marker.collapsible ? 'details' : 'div',
      hProperties: {
        className: ['callout', `callout-${marker.type}`],
        dataCallout: marker.type,
        ...(marker.collapsible && marker.open ? { open: true } : {}),
      },
    };
  });
};
