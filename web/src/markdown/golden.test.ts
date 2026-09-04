/**
 * Golden test: one fixture page through the whole pipeline, compared against a
 * committed HTML snapshot. Review the diff by hand when it changes — a golden
 * file regenerated blindly is a test that no longer tests anything.
 *
 * Regenerate with `npx vitest run src/markdown/golden.test.ts -u`.
 */

import { toHtml } from 'hast-util-to-html';
import { describe, expect, it } from 'vitest';

import source from '@/markdown/fixtures/kitchen-sink.md?raw';
import { renderMarkdown } from '@/markdown/pipeline';
import type { LinkResolution, WikiTarget } from '@/markdown/types';

const PAGES = new Set(['docs/architecture/overview.md', 'docs/architecture/sso.md']);
const BASE_PATH = 'docs/architecture/index.md';

function resolveLink(target: WikiTarget): LinkResolution {
  if (target.kind === 'item') {
    return { href: `/p/${target.projectKey ?? 'ACME'}/items/${target.target}`, kind: 'item' };
  }
  const project = target.projectKey ?? 'ACME';
  const path = `docs/${target.target}.md`;
  return {
    href: `/p/${project}/kb/${path}`,
    kind: PAGES.has(path) ? 'page' : 'missing',
  };
}

function resolveHref(vaultPath: string): LinkResolution {
  return {
    href: `/p/ACME/kb/${vaultPath}`,
    kind: PAGES.has(vaultPath) ? 'page' : 'missing',
  };
}

describe('golden fixture', () => {
  it('renders the kitchen-sink page exactly as recorded', async () => {
    const result = await renderMarkdown(source, {
      basePath: BASE_PATH,
      resolveLink,
      resolveHref,
    });

    expect(result.frontMatter).toBe('title: Kitchen sink\ntags: [fixture, markdown]');
    expect(result.hasMermaid).toBe(true);
    expect(result.headings.map((heading) => heading.id)).toEqual([
      'kitchen-sink',
      'wikilinks',
      'task-list',
      'table',
      'callouts',
      'code',
      'assets',
      'footnotes',
    ]);

    await expect(`${toHtml(result.root)}\n`).toMatchFileSnapshot(
      './fixtures/kitchen-sink.golden.html',
    );
  });
});
