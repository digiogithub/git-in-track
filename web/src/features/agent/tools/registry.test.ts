import { PERMISSION_TOOL_NAME, QUESTION_TOOL_NAME } from '@pando-ai/sdk/agui/client';
import { describe, expect, it, vi } from 'vitest';
import { z } from 'zod';

import type { Item } from '@/api/provider';
import { toItemSearchParams } from '@/features/agent/tools/backlog';
import { focusBoardCardInDom, safeKbPath } from '@/features/agent/tools/navigation';
import {
  assertNoHitlShadowing,
  createToolRunner,
  frontendTools,
  isFrontendTool,
  toolDeclarations,
} from '@/features/agent/tools/registry';
import { defineTool, type ToolContext } from '@/features/agent/tools/types';
import { validateItemSearch } from '@/features/backlog/search';

function item(id: string, patch: Partial<Item> = {}): Item {
  return {
    id,
    type: 'story',
    title: `Title of ${id}`,
    status: 'todo',
    priority: 'high',
    body: '',
    path: `docs/.pmngr/stories/${id}.md`,
    rev: 'sha256:0000000000000000',
    ...patch,
  };
}

function context(overrides: Partial<ToolContext> = {}) {
  const navigate = vi.fn();
  const ctx: ToolContext = { navigate, project: 'GIT', ...overrides };
  return { ctx, navigate };
}

async function run(ctx: ToolContext, name: string, args: unknown): Promise<unknown> {
  return JSON.parse(await createToolRunner(ctx).run(name, args));
}

describe('the declarations', () => {
  it('declares five tools with a JSON-schema parameter object each', () => {
    const declarations = toolDeclarations();
    expect(declarations.map((tool) => tool.name)).toEqual([
      'open_item',
      'open_kb_page',
      'focus_board_card',
      'apply_backlog_filter',
      'show_items',
    ]);
    for (const tool of declarations) {
      expect(tool.description).toBeTruthy();
      expect(tool.parameters).toMatchObject({ type: 'object', additionalProperties: false });
    }
  });

  it('returns the very same array every time, so the agent-pool key cannot move', () => {
    expect(toolDeclarations()).toBe(toolDeclarations());
    expect(Object.isFrozen(toolDeclarations())).toBe(true);
  });

  it('gives every declared tool a validator', () => {
    for (const tool of frontendTools) {
      expect(tool.validate({ nothing: true }).ok).toBe(false);
    }
    expect(isFrontendTool('open_item')).toBe(true);
    expect(isFrontendTool('pando_permission_request')).toBe(false);
  });

  it('refuses a registry that would shadow a human-in-the-loop prompt', () => {
    const shadow = defineTool({
      name: PERMISSION_TOOL_NAME,
      description: 'nope',
      parameters: { type: 'object', properties: {}, additionalProperties: false },
      schema: z.strictObject({}),
      execute: () => Promise.resolve({}),
    });
    expect(() => assertNoHitlShadowing([shadow])).toThrow(/may not shadow/);
    expect(() => assertNoHitlShadowing(frontendTools)).not.toThrow();
    expect(frontendTools.map((tool) => tool.name)).not.toContain(QUESTION_TOOL_NAME);
  });
});

describe('the runner never leaves a run parked', () => {
  it('answers an unknown tool with an error result instead of hanging', async () => {
    const { ctx, navigate } = context();
    const result = await run(ctx, 'delete_everything', {});
    expect(result).toMatchObject({ error: 'unknown_tool' });
    expect(navigate).not.toHaveBeenCalled();
  });

  it('answers invalid arguments by naming the field', async () => {
    const { ctx, navigate } = context();
    const result = await run(ctx, 'apply_backlog_filter', { category: 'nonsense' });
    expect(result).toMatchObject({ error: 'invalid_arguments', field: 'category' });
    expect(navigate).not.toHaveBeenCalled();
  });

  it('answers an unknown field rather than ignoring it', async () => {
    const { ctx } = context();
    const result = await run(ctx, 'apply_backlog_filter', { sprint: 'S-1' });
    expect(result).toMatchObject({ error: 'invalid_arguments' });
  });

  it('turns a throwing executor into a result', async () => {
    const { ctx } = context({
      lookup: {
        item: () => {
          throw new Error('cache exploded');
        },
      },
      navigate: () => {
        throw new Error('router exploded');
      },
    });
    const result = await run(ctx, 'open_item', { id: 'GIT-US-0061' });
    expect(result).toMatchObject({ error: 'tool_failed', message: 'cache exploded' });
  });
});

describe('open_item', () => {
  it('navigates to the item route and reports the title', async () => {
    const { ctx, navigate } = context({
      lookup: { item: () => Promise.resolve(item('GIT-US-0061')) },
    });

    const result = await run(ctx, 'open_item', { id: 'GIT-US-0061' });

    expect(navigate).toHaveBeenCalledWith({
      to: '/p/$project/items/$id',
      params: { project: 'GIT', id: 'GIT-US-0061' },
    });
    expect(result).toMatchObject({ opened: 'GIT-US-0061', title: 'Title of GIT-US-0061' });
  });

  it('reports not found and navigates nowhere', async () => {
    const { ctx, navigate } = context({ lookup: { item: () => Promise.resolve(null) } });

    expect(await run(ctx, 'open_item', { id: 'GIT-US-9999' })).toMatchObject({
      error: 'not_found',
    });
    expect(navigate).not.toHaveBeenCalled();
  });

  it('refuses anything that is not an item id', async () => {
    const { ctx, navigate } = context();
    expect(await run(ctx, 'open_item', { id: '../../etc/passwd' })).toMatchObject({
      error: 'invalid_arguments',
      field: 'id',
    });
    expect(navigate).not.toHaveBeenCalled();
  });
});

describe('open_kb_page', () => {
  it('navigates to the page route', async () => {
    const { ctx, navigate } = context({ lookup: { kbPage: () => Promise.resolve(true) } });

    const result = await run(ctx, 'open_kb_page', { path: 'docs/05-web-app.md' });

    expect(navigate).toHaveBeenCalledWith({
      to: '/p/$project/kb/$',
      params: { project: 'GIT', _splat: 'docs/05-web-app.md' },
    });
    expect(result).toMatchObject({ opened: 'docs/05-web-app.md' });
  });

  it('reports a page the vault does not have', async () => {
    const { ctx, navigate } = context({ lookup: { kbPage: () => Promise.resolve(false) } });
    expect(await run(ctx, 'open_kb_page', { path: 'docs/ghost.md' })).toMatchObject({
      error: 'not_found',
    });
    expect(navigate).not.toHaveBeenCalled();
  });

  it('refuses a path that tries to leave the app', async () => {
    const { ctx, navigate } = context();
    for (const path of ['../../etc/passwd', '/etc/passwd', 'https://example.com/x.md']) {
      expect(await run(ctx, 'open_kb_page', { path })).toMatchObject({ error: 'out_of_app' });
    }
    expect(navigate).not.toHaveBeenCalled();
    expect(safeKbPath('docs/a.md')).toEqual({ path: 'docs/a.md' });
  });
});

describe('focus_board_card', () => {
  it('opens the board and scrolls the card into view', async () => {
    const focusCard = vi.fn(() => Promise.resolve(true));
    const { ctx, navigate } = context({ focusCard });

    const result = await run(ctx, 'focus_board_card', {
      board: 'delivery',
      itemId: 'GIT-US-0061',
    });

    expect(navigate).toHaveBeenCalledWith({ to: '/boards/$slug', params: { slug: 'delivery' } });
    expect(focusCard).toHaveBeenCalledWith('GIT-US-0061');
    expect(result).toMatchObject({ focused: true, board: 'delivery' });
  });

  it('says so when the card is not on the board', async () => {
    const { ctx } = context({ focusCard: () => Promise.resolve(false) });
    expect(
      await run(ctx, 'focus_board_card', { board: 'delivery', itemId: 'GIT-US-0061' }),
    ).toMatchObject({ focused: false });
  });

  it('refuses a slug that is not one', async () => {
    const { ctx, navigate } = context({ focusCard: () => Promise.resolve(true) });
    expect(
      await run(ctx, 'focus_board_card', { board: '../admin', itemId: 'GIT-US-0061' }),
    ).toMatchObject({ error: 'invalid_arguments', field: 'board' });
    expect(navigate).not.toHaveBeenCalled();
  });

  // The default focus walks the real DOM, so it has to agree with the attribute
  // a board card actually renders: `data-item-id`, the bare id next to the
  // project-qualified `data-ref` (BoardCardTile).
  it('finds the card a board renders and scrolls it into view', async () => {
    const card = document.createElement('li');
    card.setAttribute('data-ref', 'GIT/GIT-US-0061');
    card.setAttribute('data-item-id', 'GIT-US-0061');
    const scrollIntoView = vi.fn();
    card.scrollIntoView = scrollIntoView;
    document.body.append(card);

    try {
      expect(await focusBoardCardInDom('GIT-US-0061')).toBe(true);
      expect(scrollIntoView).toHaveBeenCalledWith({ block: 'center' });
      // A project-qualified argument reaches the same card.
      expect(await focusBoardCardInDom('GIT/GIT-US-0061')).toBe(true);
      expect(scrollIntoView).toHaveBeenCalledTimes(2);
    } finally {
      card.remove();
    }
  });
});

describe('apply_backlog_filter', () => {
  it('writes params the route accepts, and nothing else', async () => {
    const { ctx, navigate } = context();

    const result = await run(ctx, 'apply_backlog_filter', {
      type: ['story', 'task'],
      category: 'in_progress',
      priority: ['high'],
      q: 'agent',
    });

    expect(navigate).toHaveBeenCalledTimes(1);
    const call = navigate.mock.calls[0]?.[0] as {
      to: string;
      search: Record<string, string | undefined>;
    };
    expect(call.to).toBe('/p/$project/items');
    expect(call.search).toMatchObject({
      type: 'story,task',
      category: 'in_progress',
      priority: 'high',
      q: 'agent',
    });
    // The router's own validator is the only judge that matters.
    expect(
      validateItemSearch(call.search as Parameters<typeof validateItemSearch>[0]),
    ).toMatchObject({
      type: ['story', 'task'],
      category: 'in_progress',
      priority: ['high'],
      q: 'agent',
    });
    expect(result).toMatchObject({ route: '/p/GIT/items' });
  });

  it('serialises lists the way the URL spells them', () => {
    expect(toItemSearchParams({ label: ['web', 'security'] })).toMatchObject({
      label: 'web,security',
    });
  });
});

describe('show_items', () => {
  it('returns resolved cards and the ids it could not resolve', async () => {
    const seen: string[] = [];
    const { ctx, navigate } = context({
      lookup: {
        item: (id: string) => {
          seen.push(id);
          return Promise.resolve(id === 'GIT-US-0061' ? item(id) : null);
        },
      },
    });

    const result = (await run(ctx, 'show_items', {
      ids: ['GIT-US-0061', 'GIT-US-9999', 'GIT-US-0061'],
    })) as { items: { id: string }[]; unresolved: string[] };

    // The duplicate is asked for once: resolution goes through the cache.
    expect(seen).toEqual(['GIT-US-0061', 'GIT-US-9999']);
    expect(result.items.map((entry) => entry.id)).toEqual(['GIT-US-0061']);
    expect(result.unresolved).toEqual(['GIT-US-9999']);
    expect(navigate).not.toHaveBeenCalled();
  });

  it('says so when the runtime cannot resolve items at all', async () => {
    const { ctx } = context();
    expect(await run(ctx, 'show_items', { ids: ['GIT-US-0061'] })).toMatchObject({
      error: 'not_supported',
    });
  });
});
