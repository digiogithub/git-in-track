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

  it('carries the Pando index a semantic hit came from, and a file kind', () => {
    const result = toSearchResult({
      hits: [
        { kind: 'page', path: 'docs/21.md', title: '21', source: 'pando', index: 'kb' },
        {
          kind: 'file',
          path: 'internal/server/search_code.go',
          title: 'searchCode',
          source: 'pando',
          index: 'code',
        },
        // An index this client does not know is left absent, never guessed.
        { kind: 'page', path: 'c.md', source: 'pando', index: 'martian' },
      ],
    });

    expect(result.hits.map((hit) => hit.index)).toEqual(['kb', 'code', undefined]);
    expect(result.hits[1]?.kind).toBe('file');
  });

  it('keeps a requirement hit with its ref, spec, status and anchor (GIT-US-0118)', () => {
    const result = toSearchResult({
      hits: [
        {
          kind: 'requirement',
          id: 'ACME-SP-0003.R2',
          path: 'docs/.pmngr/specs/ACME-SP-0003-tokens.md',
          title: 'Rotate refresh tokens',
          source: 'pando',
          index: 'kb',
          spec: 'ACME-SP-0003',
          status: 'todo',
          anchor: 'acme-sp-0003-r2',
        },
      ],
    });

    expect(result.hits[0]).toMatchObject({
      kind: 'requirement',
      id: 'ACME-SP-0003.R2',
      spec: 'ACME-SP-0003',
      status: 'todo',
      anchor: 'acme-sp-0003-r2',
    });
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

describe('FakeProvider semantic search settings (story GIT-US-0091)', () => {
  it('refuses every call on a runtime scripted without the surface', async () => {
    const provider = new FakeProvider();

    expect(provider.capabilities.searchSettings).toBe(false);
    await expect(provider.getSearchSettings()).rejects.toMatchObject({ code: 'not_supported' });
    await expect(provider.reindexSearch()).rejects.toMatchObject({ code: 'not_supported' });
  });

  it('answers a settings document a card can render', async () => {
    const provider = new FakeProvider({
      search: { fullTextSearch: 'pando', settings: { projectId: 'acme-api' } },
    });

    expect(provider.capabilities.searchSettings).toBe(true);
    await expect(provider.getSearchSettings()).resolves.toMatchObject({
      backend: 'pando',
      configured: true,
      projectId: 'acme-api',
      reachable: true,
    });
  });

  it('reports whether a patch reached the configuration file', async () => {
    const written = new FakeProvider({ search: { settings: {} } });
    const inMemory = new FakeProvider({ search: { settings: {}, persisted: false } });

    await expect(written.updateSearchSettings({ projectId: 'acme' })).resolves.toMatchObject({
      projectId: 'acme',
      persisted: true,
    });
    await expect(inMemory.updateSearchSettings({ projectId: 'acme' })).resolves.toMatchObject({
      persisted: false,
    });
  });

  it('refuses a remote Pando URL unless allowRemote is on', async () => {
    const provider = new FakeProvider({ search: { settings: {} } });

    await expect(
      provider.updateSearchSettings({ mcpUrl: 'http://pando.example.com:9777/mcp' }),
    ).rejects.toMatchObject({ code: 'validation_failed' });
    await expect(
      provider.updateSearchSettings({
        mcpUrl: 'http://pando.example.com:9777/mcp',
        allowRemote: true,
      }),
    ).resolves.toMatchObject({ allowRemote: true });
  });

  it('queues a reindex, and answers the scripted refusal instead when told to', async () => {
    const provider = new FakeProvider({ search: { settings: {} } });
    const job = await provider.reindexSearch();

    expect(job.phase).toBe('code');
    await expect(provider.getSearchSettings()).resolves.toMatchObject({
      reindex: { jobId: job.jobId },
    });

    const busy = new FakeProvider({
      search: {
        settings: {},
        reindexError: { code: 'search_reindex_running', message: 'A reindex is already running.' },
      },
    });
    await expect(busy.reindexSearch()).rejects.toMatchObject({ code: 'search_reindex_running' });
  });
});
