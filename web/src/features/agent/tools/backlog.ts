/**
 * The backlog tools (tasks GIT-T-0090 and GIT-T-0094).
 *
 * `apply_backlog_filter` writes the filter into the URL and nowhere else. The
 * URL *is* the filter in this app (docs/05 §3.1), so the tool's whole job is
 * to turn model output into search params `validateItemSearch` already
 * accepts; it introduces no parallel state and it never touches a store.
 * Validation is strict on purpose — `itemSearchSchema` is `.catch()`-guarded
 * so a hand-edited URL degrades gracefully, but an agent that named a status
 * that does not exist should be told, not silently given an empty table.
 *
 * `show_items` navigates nowhere. It resolves the ids it was given and returns
 * them, card-shaped, on the tool result; `ui/ItemCards.tsx` renders that
 * result inline in the transcript. Resolution goes through the context lookup
 * the page wires to the TanStack Query cache, so a card the user already saw
 * costs no request.
 */

import { z } from 'zod';

import type { Item } from '@/api/provider';
import { defineTool, type ToolResult } from '@/features/agent/tools/types';
import { bareItemId } from '@/features/backlog/item-meta';
import {
  defaultPriorities,
  filterableItemTypes,
  parseItemSearch,
  sortFields,
  statusCategories,
  toSearchInput,
} from '@/features/backlog/search';

/** How many cards one `show_items` call may put in the transcript. */
export const MAX_SHOWN_ITEMS = 24;

const list = (inner: z.ZodType<string>) => z.array(inner).min(1).max(32).optional();
const text = z.string().trim().min(1).max(200).optional();

/**
 * Strict on purpose: `z.strictObject` is what turns "the model invented a
 * field" into a named error instead of a silently ignored key.
 */
export const backlogFilterArgs = z.strictObject({
  q: text,
  type: list(z.enum(filterableItemTypes)),
  status: list(z.string().trim().min(1)),
  category: z.enum(statusCategories).optional(),
  priority: list(z.enum(defaultPriorities)),
  label: list(z.string().trim().min(1)),
  assignee: text,
  milestone: text,
  parent: text,
  sort: z.enum(sortFields).optional(),
  order: z.enum(['asc', 'desc']).optional(),
});

export type BacklogFilterArgs = z.output<typeof backlogFilterArgs>;

/** Comma-separated lists, the shape the item route's search params use. */
export function toItemSearchParams(args: BacklogFilterArgs): Record<string, string | undefined> {
  const join = (value: string[] | undefined) =>
    value === undefined || value.length === 0 ? undefined : value.join(',');
  const raw = {
    q: args.q,
    type: join(args.type),
    status: join(args.status),
    category: args.category,
    priority: join(args.priority),
    label: join(args.label),
    assignee: args.assignee,
    milestone: args.milestone,
    parent: args.parent,
    sort: args.sort,
    order: args.order,
  };
  // Round-tripping through the route's own parser is the only check that
  // matters: whatever comes out is, by construction, something the router will
  // accept back.
  return { ...toSearchInput(parseItemSearch(raw)) };
}

export const applyBacklogFilterTool = defineTool({
  name: 'apply_backlog_filter',
  description:
    'Narrow the backlog table to a filter by writing it into the URL. The URL is the filter, ' +
    'so the resulting view is shareable and survives a reload. Pass only the fields to set; ' +
    'omitted fields are cleared.',
  parameters: {
    type: 'object',
    properties: {
      q: { type: 'string', description: 'Full-text needle across id, title and body.' },
      type: {
        type: 'array',
        description: 'Item types to keep.',
        items: { type: 'string', enum: [...filterableItemTypes] },
      },
      status: {
        type: 'array',
        description: 'Workflow status ids declared by the project.',
        items: { type: 'string' },
      },
      category: {
        type: 'string',
        description: 'Coarse status category.',
        enum: [...statusCategories],
      },
      priority: {
        type: 'array',
        description: 'Priorities to keep.',
        items: { type: 'string', enum: [...defaultPriorities] },
      },
      label: { type: 'array', description: 'Labels to keep.', items: { type: 'string' } },
      assignee: { type: 'string', description: 'Assignee handle.' },
      milestone: { type: 'string', description: 'Milestone id.' },
      parent: { type: 'string', description: 'Parent epic or story id.' },
      sort: { type: 'string', description: 'Sort field.', enum: [...sortFields] },
      order: { type: 'string', description: 'Sort direction.', enum: ['asc', 'desc'] },
    },
    required: [],
    additionalProperties: false,
  },
  schema: backlogFilterArgs,
  execute(args, context): Promise<ToolResult> {
    if (context.project === null) {
      return Promise.resolve({
        error: 'no_project',
        message: 'No project is open, so there is no backlog to filter.',
      });
    }
    const search = toItemSearchParams(args);
    context.navigate({
      to: '/p/$project/items',
      params: { project: context.project },
      search,
    });
    const applied = Object.fromEntries(
      Object.entries(search).filter(([, value]) => value !== undefined),
    );
    return Promise.resolve({ applied, route: `/p/${context.project}/items` });
  },
});

/** One item as the inline transcript cards render it. */
export type ItemCard = {
  id: string;
  type: Item['type'];
  title: string;
  status?: string;
  priority?: Item['priority'];
};

function toCard(item: Item): ItemCard {
  return {
    id: item.id,
    type: item.type,
    title: item.title,
    ...(item.status === undefined ? {} : { status: item.status }),
    ...(item.priority === undefined ? {} : { priority: item.priority }),
  };
}

/** The `show_items` result, as it is both sent to the agent and rendered. */
export type ShowItemsResult = {
  items: ItemCard[];
  unresolved: string[];
};

/** Reads a `show_items` tool result back off a transcript message. */
export function parseShowItemsResult(raw: string): ShowItemsResult | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof parsed !== 'object' || parsed === null) return null;
  const record = parsed as { items?: unknown; unresolved?: unknown };
  if (!Array.isArray(record.items)) return null;
  const items = record.items.filter(
    (entry): entry is ItemCard =>
      typeof entry === 'object' &&
      entry !== null &&
      typeof (entry as ItemCard).id === 'string' &&
      typeof (entry as ItemCard).title === 'string',
  );
  const unresolved = Array.isArray(record.unresolved)
    ? record.unresolved.filter((entry): entry is string => typeof entry === 'string')
    : [];
  return { items, unresolved };
}

export const showItemsTool = defineTool({
  name: 'show_items',
  description:
    'Render item cards inline in this conversation, without navigating away. Use it to show ' +
    'the user a short list of items you are talking about.',
  parameters: {
    type: 'object',
    properties: {
      ids: {
        type: 'array',
        description: 'Item ids to show, for example ["GIT-US-0061"].',
        items: { type: 'string' },
        minItems: 1,
        maxItems: MAX_SHOWN_ITEMS,
      },
    },
    required: ['ids'],
    additionalProperties: false,
  },
  schema: z.strictObject({
    ids: z.array(z.string().trim().min(1).max(128)).min(1).max(MAX_SHOWN_ITEMS),
  }),
  async execute(args, context): Promise<ToolResult> {
    const lookup = context.lookup?.item;
    if (lookup === undefined) {
      return { error: 'not_supported', message: 'This runtime cannot resolve items.' };
    }
    const ids = [...new Set(args.ids.map(bareItemId))];
    const items: ItemCard[] = [];
    const unresolved: string[] = [];
    for (const id of ids) {
      let found: Item | null = null;
      try {
        found = await lookup(id);
      } catch {
        found = null;
      }
      if (found === null) unresolved.push(id);
      else items.push(toCard(found));
    }
    return { items, unresolved };
  },
});
