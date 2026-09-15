import { describe, expect, it } from 'vitest';

import { toSearchResult } from '@/api/companion-provider';
import { FakeProvider } from '@/api/fake-provider';
import type { SearchHit } from '@/api/provider';

/** One semantic candidate, as the fake hands it to the search panel. */
const related: SearchHit = {
  kind: 'page',
  path: 'knowledge/security/identity-provider.md',
  title: 'Identity provider',
  snippet: 'The identity provider issues the token.',
  score: 0.71,
  source: 'pando',
};

describe('search response mapping', () => {
  it('reads the hits envelope and the degraded flag', () => {
    const result = toSearchResult({
      hits: [
        { kind: 'item', id: 'ACME-US-0001', path: 'a.md', title: 'A', score: 3 },
        {
          kind: 'page',
          path: 'b.md',
          title: 'B',
          snippet: 'why it matched',
          score: 0.6,
          source: 'pando',
        },
      ],
      degraded: true,
    });

    expect(result.degraded).toBe(true);
    expect(result.hits.map((hit) => hit.source)).toEqual(['core', 'pando']);
    expect(result.hits[1]?.snippet).toBe('why it matched');
  });

  it('reads the older shapes as exact hits from the local index', () => {
    const bare = toSearchResult([{ kind: 'item', path: 'a.md', title: 'A' }]);
    const wrapped = toSearchResult({ results: [{ kind: 'page', path: 'b.md', title: 'B' }] });

    expect(bare.hits).toHaveLength(1);
    expect(bare.hits[0]?.source).toBe('core');
    expect(bare.degraded).toBeUndefined();
    expect(wrapped.hits[0]?.source).toBe('core');
  });

  it('reads an unknown origin as the local index', () => {
    const result = toSearchResult({ hits: [{ kind: 'item', path: 'a.md', source: 'martian' }] });
    expect(result.hits[0]?.source).toBe('core');
  });
});

describe('FakeProvider search', () => {
  it('answers with exact hits only by default', async () => {
    const provider = new FakeProvider();
    const result = await provider.search({ text: 'sso' });

    expect(result.hits.length).toBeGreaterThan(0);
    expect(result.hits.every((hit) => hit.source === 'core')).toBe(true);
    expect(result.degraded).toBeUndefined();
    expect(provider.capabilities.fullTextSearch).toBe('core');
  });

  it('appends the scripted semantic hits after the exact ones', async () => {
    const provider = new FakeProvider({
      search: { fullTextSearch: 'pando', semantic: [related] },
    });
    const result = await provider.search({ text: 'sso' });

    expect(provider.capabilities.fullTextSearch).toBe('pando');
    const sources = result.hits.map((hit) => hit.source);
    expect(sources.at(-1)).toBe('pando');
    expect(sources.indexOf('pando')).toBe(sources.lastIndexOf('pando'));
    expect(result.hits.at(-1)?.snippet).toBe('The identity provider issues the token.');
  });

  it('reports a degraded semantic half when the fixture says so', async () => {
    const provider = new FakeProvider({ search: { fullTextSearch: 'pando', degraded: true } });
    await expect(provider.search({ text: 'sso' })).resolves.toMatchObject({ degraded: true });
  });
});
