/**
 * Task-list addressing.
 *
 * `remark-gfm` renders `- [x] …` as a disabled checkbox and forgets where it
 * came from. This plugin stamps the source line of every task-list item onto
 * its checkbox, which is the address the core needs to flip exactly that one
 * line of the body (`core.SetTaskListItem`, docs/05-web-app.md §8.2).
 *
 * The line is the list item's own start line, which is the line the marker sits
 * on — the same line the Go scanner counts. Nothing here rewrites Markdown: the
 * app never edits the body in the browser, it asks the core to.
 */

import type { Element, Root } from 'hast';
import type { Plugin } from 'unified';
import { visit } from 'unist-util-visit';

/** The checkbox `mdast-util-to-hast` puts at the head of a task-list item. */
function checkboxOf(item: Element): Element | undefined {
  let found: Element | undefined;
  visit(item, 'element', (node: Element) => {
    if (found) return;
    if (node.tagName === 'input' && node.properties?.['type'] === 'checkbox') found = node;
  });
  return found;
}

export const rehypeTaskList: Plugin<[], Root> = () => (tree: Root) => {
  let index = 0;
  visit(tree, 'element', (node: Element) => {
    if (node.tagName !== 'li') return;
    const input = checkboxOf(node);
    if (!input) return;
    const line = node.position?.start.line;
    input.properties ??= {};
    input.properties['dataTaskIndex'] = String(index);
    index += 1;
    // A tree built without positions (a hand-made fixture) still renders; the
    // checkbox is then simply not addressable and stays read-only.
    if (typeof line === 'number') input.properties['dataTaskLine'] = String(line);
  });
};

/** Reads the stamped line back off a rendered checkbox. */
export function taskLineOf(properties: Record<string, unknown> | undefined): number | undefined {
  const raw = properties?.['dataTaskLine'];
  if (typeof raw !== 'string') return undefined;
  const line = Number.parseInt(raw, 10);
  return Number.isFinite(line) && line > 0 ? line : undefined;
}
