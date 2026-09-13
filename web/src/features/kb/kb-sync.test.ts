/**
 * The pure rules behind the knowledge-base sync UI (story GIT-US-0093).
 */

import { describe, expect, it } from 'vitest';

import type { KbNode, KbPageSyncStatus, KbSyncState } from '@/api/provider';
import {
  conflictPathOf,
  folderOf,
  KB_SYNC_LABELS,
  pagesUnder,
  summariseKbSync,
  treePagesUnder,
} from '@/features/kb/kb-sync';

describe('summariseKbSync', () => {
  it('reports the worst state under a folder, and how many pages hold it', () => {
    const summary = summariseKbSync(['in_sync', 'conflict', 'local_ahead', 'conflict']);

    // The reason to summarise a folder is to find the page that needs looking
    // at, so the one that needs it most wins.
    expect(summary).toEqual({ state: 'conflict', count: 2, pages: 4 });
  });

  it('orders remote_ahead above local_ahead and both above in_sync', () => {
    expect(summariseKbSync(['in_sync', 'local_ahead'])?.state).toBe('local_ahead');
    expect(summariseKbSync(['local_ahead', 'remote_ahead'])?.state).toBe('remote_ahead');
    expect(summariseKbSync(['unlinked', 'in_sync'])?.state).toBe('in_sync');
  });

  it('summarises an empty folder to nothing, so it carries no badge', () => {
    expect(summariseKbSync([])).toBeNull();
  });
});

describe('conflictPathOf', () => {
  it('names the file the companion writes, exactly', () => {
    expect(conflictPathOf('docs/handbook/onboarding.md')).toBe(
      'docs/handbook/onboarding.conflict.md',
    );
  });

  it('handles a path that does not end in .md without doubling the suffix', () => {
    expect(conflictPathOf('docs/handbook/onboarding')).toBe('docs/handbook/onboarding.conflict.md');
  });
});

describe('folderOf', () => {
  it('is the containing folder, and empty at the vault root', () => {
    expect(folderOf('docs/handbook/onboarding.md')).toBe('docs/handbook');
    expect(folderOf('index.md')).toBe('');
  });
});

describe('pagesUnder', () => {
  const rows: KbPageSyncStatus[] = [
    { path: 'docs/index.md', linked: false, state: 'unlinked' },
    { path: 'docs/handbook/a.md', linked: true, state: 'in_sync' },
    { path: 'docs/handbook/deep/b.md', linked: true, state: 'conflict' },
  ];

  it('selects a folder and everything below it', () => {
    expect(pagesUnder(rows, 'docs/handbook').map((row) => row.path)).toEqual([
      'docs/handbook/a.md',
      'docs/handbook/deep/b.md',
    ]);
  });

  it('selects everything for the empty path', () => {
    expect(pagesUnder(rows, '')).toHaveLength(3);
  });
});

describe('treePagesUnder', () => {
  const tree: KbNode[] = [
    {
      path: 'docs',
      name: 'docs',
      kind: 'dir',
      children: [
        { path: 'docs/index.md', name: 'index.md', kind: 'page' },
        {
          path: 'docs/handbook',
          name: 'handbook',
          kind: 'dir',
          children: [{ path: 'docs/handbook/a.md', name: 'a.md', kind: 'page' }],
        },
      ],
    },
  ];

  it('counts every page under a folder, recursively', () => {
    expect(treePagesUnder(tree, 'docs')).toEqual(['docs/index.md', 'docs/handbook/a.md']);
    expect(treePagesUnder(tree, 'docs/handbook')).toEqual(['docs/handbook/a.md']);
  });

  it('counts the whole tree for the empty path, and nothing for an unknown one', () => {
    expect(treePagesUnder(tree, '')).toHaveLength(2);
    expect(treePagesUnder(tree, 'docs/missing')).toEqual([]);
  });
});

describe('labels', () => {
  it('gives every state a word, because colour is never the only signal', () => {
    const states: KbSyncState[] = [
      'unlinked',
      'in_sync',
      'local_ahead',
      'remote_ahead',
      'conflict',
    ];
    for (const state of states) {
      expect(KB_SYNC_LABELS[state]).toMatch(/\S/);
    }
  });
});
