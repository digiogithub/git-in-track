import { toHtml } from 'hast-util-to-html';
import { describe, expect, it } from 'vitest';

import { renderMarkdown } from '@/markdown/pipeline';
import type { LinkResolution, WikiTarget } from '@/markdown/types';
import { isItemId, parseWikiTarget } from '@/markdown/wikilink';

/** A resolver that mirrors what the KB viewer does, with a tiny fixed vault. */
const pages = new Set(['architecture/overview', 'index', 'guides/dir/page']);

function resolveLink(target: WikiTarget): LinkResolution {
  if (target.kind === 'item') {
    const key = target.projectKey ?? 'ACME';
    return { href: `/p/${key}/items/${target.target}`, kind: 'item' };
  }
  if (pages.has(target.target)) {
    return { href: `/p/ACME/kb/docs/${target.target}.md`, kind: 'page' };
  }
  return { href: `/p/ACME/kb/docs/${target.target}.md`, kind: 'missing' };
}

async function html(source: string): Promise<string> {
  const result = await renderMarkdown(source, { resolveLink, highlight: false });
  return toHtml(result.root);
}

describe('parseWikiTarget', () => {
  it('parses a bare page', () => {
    expect(parseWikiTarget('page')).toMatchObject({ kind: 'page', target: 'page', embed: false });
  });

  it('parses a page path and drops a trailing .md', () => {
    expect(parseWikiTarget('dir/page.md')).toMatchObject({ kind: 'page', target: 'dir/page' });
  });

  it('parses an item id and derives the project key', () => {
    expect(parseWikiTarget('ACME-US-0042')).toMatchObject({
      kind: 'item',
      target: 'ACME-US-0042',
      projectKey: 'ACME',
    });
  });

  it('parses a cross-project item reference', () => {
    expect(parseWikiTarget('WEB/WEB-US-0031')).toMatchObject({
      kind: 'item',
      target: 'WEB-US-0031',
      projectKey: 'WEB',
    });
  });

  it('parses a cross-project page reference', () => {
    expect(parseWikiTarget('WEB:architecture/overview')).toMatchObject({
      kind: 'page',
      target: 'architecture/overview',
      projectKey: 'WEB',
    });
  });

  it('parses an alias', () => {
    expect(parseWikiTarget('ACME-US-0042|the SSO story')).toMatchObject({
      kind: 'item',
      target: 'ACME-US-0042',
      alias: 'the SSO story',
    });
  });

  it('parses an anchor, on pages and on comments', () => {
    expect(parseWikiTarget('architecture/overview#Session revocation')).toMatchObject({
      kind: 'page',
      target: 'architecture/overview',
      anchor: 'Session revocation',
    });
    expect(parseWikiTarget('ACME-US-0042#20260901T104512Z-jose')).toMatchObject({
      kind: 'item',
      anchor: '20260901T104512Z-jose',
    });
  });

  it('parses an alias and an anchor together', () => {
    expect(parseWikiTarget('dir/page#Section|Read this')).toMatchObject({
      target: 'dir/page',
      anchor: 'Section',
      alias: 'Read this',
    });
  });

  it('rejects an empty target', () => {
    expect(parseWikiTarget('   ')).toBeNull();
  });

  it('recognises the item id grammar', () => {
    expect(isItemId('ACME-US-0042')).toBe(true);
    expect(isItemId('ACME-T-10234')).toBe(true);
    expect(isItemId('acme-us-0042')).toBe(false);
    expect(isItemId('architecture/overview')).toBe(false);
  });
});

describe('remarkWikilink', () => {
  it('renders a resolved page link', async () => {
    const out = await html('See [[architecture/overview]].');
    expect(out).toContain('href="/p/ACME/kb/docs/architecture/overview.md"');
    expect(out).toContain('data-kind="page"');
    expect(out).toContain('architecture/overview</a>');
    expect(out).not.toContain('data-unresolved');
  });

  it('marks an unresolved page as a broken link instead of dropping it', async () => {
    const out = await html('See [[not/written/yet]].');
    expect(out).toContain('data-unresolved="true"');
    expect(out).toContain('wikilink-missing');
  });

  it('renders an item link with a data-item-ref', async () => {
    const out = await html('Blocked by [[ACME-US-0042]].');
    expect(out).toContain('href="/p/ACME/items/ACME-US-0042"');
    expect(out).toContain('data-item-ref="ACME-US-0042"');
  });

  it('renders a cross-project item link under the other project key', async () => {
    const out = await html('See [[WEB/WEB-US-0031]].');
    expect(out).toContain('href="/p/WEB/items/WEB-US-0031"');
  });

  it('uses the alias as link text', async () => {
    const out = await html('See [[ACME-US-0042|the SSO story]].');
    expect(out).toContain('>the SSO story</a>');
  });

  it('slugs a page anchor and keeps a comment anchor verbatim', async () => {
    expect(await html('[[architecture/overview#Session revocation]]')).toContain(
      'href="/p/ACME/kb/docs/architecture/overview.md#session-revocation"',
    );
    expect(await html('[[ACME-US-0042#20260901T104512Z-jose]]')).toContain(
      'href="/p/ACME/items/ACME-US-0042#20260901T104512Z-jose"',
    );
  });

  it('renders an image embed as an image', async () => {
    const out = await html('![[diagram.png]]');
    expect(out).toContain('<img');
    expect(out).toContain('wikilink-embed');
  });

  it('renders a non-image embed as a link', async () => {
    const out = await html('![[architecture/overview]]');
    expect(out).toContain('<a');
    expect(out).toContain('wikilink-embed');
  });

  it('leaves wikilinks alone inside code spans and fences', async () => {
    const out = await html('`[[architecture/overview]]`');
    expect(out).toContain('<code>[[architecture/overview]]</code>');
  });

  it('never nests a wikilink inside an existing link', async () => {
    const out = await html('[label](https://example.com "[[architecture/overview]]")');
    expect(out).not.toContain('<a href="/p/ACME/kb');
  });

  it('collects every target in document order', async () => {
    const result = await renderMarkdown('[[a]] then [[ACME-US-0042]] and ![[b.png]]', {
      resolveLink,
      highlight: false,
    });
    expect(result.wikilinks.map((w) => w.target)).toEqual(['a', 'ACME-US-0042', 'b.png']);
  });

  it('renders the syntax literally when wikilinks are disabled (R-WIKI-4)', async () => {
    const result = await renderMarkdown('See [[architecture/overview]].', {
      wikilinks: false,
      highlight: false,
    });
    expect(toHtml(result.root)).toContain('[[architecture/overview]]');
  });
});
