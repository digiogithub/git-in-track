/**
 * The navigation tools (task GIT-T-0086).
 *
 * `open_item`, `open_kb_page` and `focus_board_card` all do the same thing to
 * the app — they move it — so they share one rule: **an argument may never
 * decide a URL on its own**. Ids are matched against the shape ids actually
 * have, paths are normalised and refused if they try to climb out of the
 * vault, and when the page gave us a way to resolve them, an id that resolves
 * to nothing produces a `not_found` result and no navigation at all. A model
 * that guessed gets told it guessed, rather than the user getting a blank
 * route.
 */

import { z } from 'zod';

import { defineTool, type ToolContext, type ToolResult } from '@/features/agent/tools/types';
import { bareItemId, projectKeyOf } from '@/features/backlog/item-meta';
import { normalizePath } from '@/markdown';

/** `ACME-US-0042`, optionally qualified as `ACME/ACME-US-0042`. */
export const ITEM_ID_PATTERN = /^[A-Za-z0-9]{1,16}-[A-Za-z]{1,6}-\d{1,8}$/;

/** Board slugs are lowercase kebab, as the board routes spell them. */
export const BOARD_SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/;

/** How long the default card focus waits for the board to paint. */
const FOCUS_TIMEOUT_MS = 600;
const FOCUS_INTERVAL_MS = 50;

const idArgs = z.strictObject({
  id: z.string().trim().min(1).max(128),
});

const kbArgs = z.strictObject({
  path: z.string().trim().min(1).max(1024),
});

const boardArgs = z.strictObject({
  board: z.string().trim().min(1).max(64),
  itemId: z.string().trim().min(1).max(128),
});

/**
 * A vault-relative page path, or a refusal.
 *
 * `..`, a leading `/` and anything carrying a scheme or a backslash are all
 * rejected outright rather than normalised away: a path that tried to leave
 * the app is a fact the agent should hear, not something to quietly clamp.
 */
export function safeKbPath(raw: string): { path: string } | { reason: string } {
  const value = raw.trim();
  if (value.startsWith('/') || value.startsWith('\\')) {
    return { reason: 'A knowledge-base path is relative to the vault root.' };
  }
  if (value.includes('\\') || /^[a-z][a-z0-9+.-]*:/i.test(value)) {
    return { reason: 'A knowledge-base path may not carry a scheme or a backslash.' };
  }
  if (value.split('/').includes('..')) {
    return { reason: 'A knowledge-base path may not climb above the vault root.' };
  }
  const path = normalizePath(value);
  if (path === '') return { reason: 'The path was empty once normalised.' };
  return { path };
}

/**
 * Walks the DOM for a board card, retrying while the board is still painting.
 *
 * The hook is `data-item-id`, the bare item id a board card carries alongside
 * its project-qualified `data-ref` (`web/src/features/boards/BoardCardTile.tsx`).
 * A qualified argument is reduced to the bare id first, so `GIT/GIT-US-0061`
 * and `GIT-US-0061` find the same card.
 */
export async function focusBoardCardInDom(itemId: string): Promise<boolean> {
  const deadline = Date.now() + FOCUS_TIMEOUT_MS;
  // `CSS.escape` is a static method that checks its receiver, so it has to be
  // called on `CSS` rather than pulled off it.
  const escape = (value: string): string =>
    globalThis.CSS?.escape === undefined
      ? value.replace(/["\\]/g, '')
      : globalThis.CSS.escape(value);
  const selector = `[data-item-id="${escape(bareItemId(itemId))}"]`;
  for (;;) {
    const node = globalThis.document?.querySelector(selector);
    if (node instanceof HTMLElement) {
      node.scrollIntoView({ block: 'center' });
      return true;
    }
    if (Date.now() >= deadline) return false;
    await new Promise((resolve) => setTimeout(resolve, FOCUS_INTERVAL_MS));
  }
}

function notFound(what: string, value: string): ToolResult {
  return { error: 'not_found', message: `No ${what} named ${value} exists in this workspace.` };
}

export const openItemTool = defineTool({
  name: 'open_item',
  description:
    'Open one backlog item (epic, story, task or milestone) in the workspace, by its id. ' +
    'Use it when the conversation is about a specific item and the user should see it.',
  parameters: {
    type: 'object',
    properties: {
      id: {
        type: 'string',
        description: 'Item id, for example GIT-US-0061. A PROJECT/ID qualifier is accepted.',
      },
    },
    required: ['id'],
    additionalProperties: false,
  },
  schema: idArgs,
  async execute(args, context): Promise<ToolResult> {
    const id = bareItemId(args.id);
    if (!ITEM_ID_PATTERN.test(id)) {
      return { error: 'invalid_arguments', field: 'id', message: 'That is not an item id.' };
    }
    const project = projectKeyOf(args.id, context.project ?? '');
    if (project === '') {
      return { error: 'no_project', message: 'No project is open, so the item has no route.' };
    }
    const found = (await context.lookup?.item?.(id)) ?? null;
    if (context.lookup?.item !== undefined && found === null) return notFound('item', id);
    context.navigate({ to: '/p/$project/items/$id', params: { project, id } });
    return {
      opened: id,
      route: `/p/${project}/items/${id}`,
      ...(found === null ? {} : { title: found.title }),
    };
  },
});

export const openKbPageTool = defineTool({
  name: 'open_kb_page',
  description:
    'Open one knowledge-base page by its vault-relative path, for example docs/05-web-app.md.',
  parameters: {
    type: 'object',
    properties: {
      path: {
        type: 'string',
        description: 'Vault-relative page path, forward slashes, including the .md extension.',
      },
    },
    required: ['path'],
    additionalProperties: false,
  },
  schema: kbArgs,
  async execute(args, context): Promise<ToolResult> {
    const safe = safeKbPath(args.path);
    if (!('path' in safe)) {
      return { error: 'out_of_app', field: 'path', message: safe.reason };
    }
    if (context.project === null) {
      return { error: 'no_project', message: 'No project is open, so the page has no route.' };
    }
    if (context.lookup?.kbPage !== undefined && !(await context.lookup.kbPage(safe.path))) {
      return notFound('knowledge-base page', safe.path);
    }
    context.navigate({
      to: '/p/$project/kb/$',
      params: { project: context.project, _splat: safe.path },
    });
    return { opened: safe.path, route: `/p/${context.project}/kb/${safe.path}` };
  },
});

export const focusBoardCardTool = defineTool({
  name: 'focus_board_card',
  description:
    'Open a board and scroll one item card into view. Use it to point at where a piece of ' +
    'work sits in the flow rather than opening the item itself.',
  parameters: {
    type: 'object',
    properties: {
      board: { type: 'string', description: 'Board slug, for example delivery.' },
      itemId: { type: 'string', description: 'Item id of the card to bring into view.' },
    },
    required: ['board', 'itemId'],
    additionalProperties: false,
  },
  schema: boardArgs,
  async execute(args, context): Promise<ToolResult> {
    if (!BOARD_SLUG_PATTERN.test(args.board)) {
      return { error: 'invalid_arguments', field: 'board', message: 'That is not a board slug.' };
    }
    const itemId = bareItemId(args.itemId);
    if (!ITEM_ID_PATTERN.test(itemId)) {
      return { error: 'invalid_arguments', field: 'itemId', message: 'That is not an item id.' };
    }
    context.navigate({ to: '/boards/$slug', params: { slug: args.board } });
    const focus = context.focusCard ?? focusBoardCardInDom;
    const focused = await focus(itemId);
    return {
      board: args.board,
      itemId,
      route: `/boards/${args.board}`,
      focused,
      ...(focused ? {} : { message: 'The board opened, but that card is not on it.' }),
    };
  },
});

/** Re-exported for the tests, which drive the executors directly. */
export type { ToolContext };
