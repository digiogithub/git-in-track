import { describe, expect, it } from 'vitest';

import type { KbNode } from '@/api/provider';
import {
  breadcrumbs,
  buildKbIndex,
  createKbResolvers,
  defaultPagePath,
  flattenTree,
  resolvePagePath,
  resolveRequestedPath,
} from '@/features/kb/kb-links';
import { parseWikiTarget } from '@/markdown';

const tree: KbNode[] = [
  {
    path: 'docs',
    name: 'docs',
    kind: 'dir',
    children: [
      { path: 'docs/index.md', name: 'index.md', kind: 'page', title: 'Home' },
      { path: 'docs/Setup.md', name: 'Setup.md', kind: 'page', title: 'Setup' },
      {
        path: 'docs/architecture',
        name: 'architecture',
        kind: 'dir',
        children: [
          {
            path: 'docs/architecture/overview.md',
            name: 'overview.md',
            kind: 'page',
            title: 'Overview',
          },
          { path: 'docs/architecture/sso.md', name: 'sso.md', kind: 'page', title: 'SSO' },
        ],
      },
      {
        path: 'docs/guides',
        name: 'guides',
        kind: 'dir',
        children: [
          { path: 'docs/guides/sso.md', name: 'sso.md', kind: 'page', title: 'SSO how-to' },
        ],
      },
    ],
  },
];

const index = buildKbIndex(tree);

describe('buildKbIndex', () => {
  it('collects every page and the common root folder', () => {
    expect(index.root).toBe('docs');
    expect(index.pages).toContain('docs/architecture/overview.md');
    expect(flattenTree(tree)).toHaveLength(8);
  });
});

describe('resolvePagePath', () => {
  const from = 'docs/index.md';

  it('resolves a path relative to the current page', () => {
    expect(resolvePagePath(index, from, 'architecture/overview')).toBe(
      'docs/architecture/overview.md',
    );
  });

  it('resolves a path relative to the docs root', () => {
    expect(resolvePagePath(index, 'docs/architecture/sso.md', 'docs/index.md')).toBe(
      'docs/index.md',
    );
  });

  it('resolves a unique basename from anywhere', () => {
    expect(resolvePagePath(index, 'docs/guides/sso.md', 'overview')).toBe(
      'docs/architecture/overview.md',
    );
  });

  it('refuses an ambiguous basename (W-LINK-AMBIGUOUS)', () => {
    expect(resolvePagePath(index, 'docs/index.md', 'sso')).toBeNull();
  });

  it('falls back to a case-insensitive path match', () => {
    expect(resolvePagePath(index, 'docs/index.md', 'setup')).toBe('docs/Setup.md');
  });

  it('returns null for a page nobody has written yet', () => {
    expect(resolvePagePath(index, from, 'architecture/not-yet')).toBeNull();
  });
});

describe('createKbResolvers', () => {
  const { resolveLink, resolveHref } = createKbResolvers('ACME', index, 'docs/index.md');

  const link = (raw: string) => {
    const target = parseWikiTarget(raw);
    if (!target) throw new Error(`unparsable target: ${raw}`);
    return resolveLink(target);
  };

  it('routes a resolved page', () => {
    expect(link('architecture/overview')).toMatchObject({
      href: '/p/ACME/kb/docs/architecture/overview.md',
      kind: 'page',
    });
  });

  it('routes an item to the backlog detail screen', () => {
    expect(link('ACME-US-0042')).toMatchObject({
      href: '/p/ACME/items/ACME-US-0042',
      kind: 'item',
    });
  });

  it('routes a cross-project item under the other project key', () => {
    expect(link('WEB/WEB-US-0031')).toMatchObject({ href: '/p/WEB/items/WEB-US-0031' });
  });

  it('routes a cross-project page optimistically', () => {
    expect(link('WEB:architecture/overview')).toMatchObject({
      href: '/p/WEB/kb/architecture/overview.md',
      kind: 'page',
    });
  });

  it('marks an unwritten page as missing and proposes where it would live', () => {
    expect(link('architecture/not-yet')).toEqual({
      href: '/p/ACME/kb/docs/architecture/not-yet.md',
      kind: 'missing',
    });
  });

  it('routes relative Markdown links through the same vault', () => {
    expect(resolveHref('docs/architecture/sso.md')).toMatchObject({ kind: 'page' });
    expect(resolveHref('docs/nope.md')).toMatchObject({ kind: 'missing' });
  });
});

describe('route paths', () => {
  it('opens the index page when the splat is empty', () => {
    expect(resolveRequestedPath('', index)).toBe('docs/index.md');
    expect(defaultPagePath(index)).toBe('docs/index.md');
  });

  it('accepts a path without the .md extension', () => {
    expect(resolveRequestedPath('docs/architecture/overview', index)).toBe(
      'docs/architecture/overview.md',
    );
  });

  it('keeps an unknown path so the viewer can show its 404 state', () => {
    expect(resolveRequestedPath('docs/ghost.md', index)).toBe('docs/ghost.md');
  });

  it('builds a breadcrumb trail', () => {
    expect(breadcrumbs('docs/architecture/overview.md')).toEqual([
      { name: 'docs', path: 'docs', isPage: false },
      { name: 'architecture', path: 'docs/architecture', isPage: false },
      { name: 'overview.md', path: 'docs/architecture/overview.md', isPage: true },
    ]);
  });
});
