import 'fake-indexeddb/auto';

import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { IndexStats, SnapshotBlob, VaultFile } from '@/core-bridge/api';

import {
  clear,
  hydrateOrBuild,
  isCacheAvailable,
  loadSnapshot,
  saveSnapshot,
  type SnapshotClient,
} from './index-cache';

/** Builds an IndexStats with the fields the cache actually reads. */
function stats(fingerprint: string, items = 3): IndexStats {
  return {
    projects: 1,
    items,
    pages: 2,
    comments: 1,
    durationMs: 1,
    fingerprint,
    diagnostics: [],
  };
}

/**
 * A core client stub. `loadVault` reports the fingerprint of the files it was
 * given, which is how a test says "the vault changed on disk".
 */
function fakeClient(options: { built: string; snapshot?: string } = { built: 'fp-built' }) {
  const exported: SnapshotBlob = {
    fingerprint: options.built,
    json: `{"schema":1,"fingerprint":"${options.built}"}`,
  };
  const client = {
    loadVault: vi.fn((files: VaultFile[]) => Promise.resolve(stats(options.built, files.length))),
    loadSnapshot: vi.fn((blob: SnapshotBlob) => Promise.resolve(stats(blob.fingerprint, 3))),
    exportSnapshot: vi.fn(() => Promise.resolve(exported)),
  };
  return client satisfies SnapshotClient & Record<string, unknown>;
}

const files: VaultFile[] = [
  { path: 'docs/.pmngr/project.yaml', text: 'schema: 1\nkey: DEMO\n' },
  { path: 'docs/index.md', text: '# Demo\n' },
];

describe('index cache', () => {
  beforeEach(() => {
    // A fresh factory per test: IndexedDB state must not leak between cases.
    globalThis.indexedDB = new IDBFactory();
  });

  it('reports that the cache is available', () => {
    expect(isCacheAvailable()).toBe(true);
  });

  it('returns null for a vault that was never cached', async () => {
    await expect(loadSnapshot('unknown')).resolves.toBeNull();
  });

  it('round-trips a snapshot', async () => {
    const saved = await saveSnapshot(
      'vault-1',
      { fingerprint: 'fp-1', json: '{"schema":1}' },
      () => 42,
    );

    expect(saved).toEqual({
      vaultId: 'vault-1',
      fingerprint: 'fp-1',
      snapshotJson: '{"schema":1}',
      savedAt: 42,
    });
    await expect(loadSnapshot('vault-1')).resolves.toEqual(saved);
  });

  it('replaces the entry of a vault instead of appending', async () => {
    await saveSnapshot('vault-1', { fingerprint: 'fp-1', json: 'a' });
    await saveSnapshot('vault-1', { fingerprint: 'fp-2', json: 'b' });

    const found = await loadSnapshot('vault-1');
    expect(found?.fingerprint).toBe('fp-2');
    expect(found?.snapshotJson).toBe('b');
  });

  it('keeps vaults apart and clears one at a time', async () => {
    await saveSnapshot('a', { fingerprint: 'fp-a', json: 'a' });
    await saveSnapshot('b', { fingerprint: 'fp-b', json: 'b' });

    await clear('a');

    await expect(loadSnapshot('a')).resolves.toBeNull();
    expect((await loadSnapshot('b'))?.fingerprint).toBe('fp-b');
  });

  describe('hydrateOrBuild', () => {
    it('builds and caches on a cold start', async () => {
      const client = fakeClient({ built: 'fp-cold' });
      const onHydrated = vi.fn();

      const result = await hydrateOrBuild(client, 'vault-1', files, { onHydrated });

      expect(result.cached).toBeNull();
      expect(onHydrated).not.toHaveBeenCalled();
      expect(client.loadSnapshot).not.toHaveBeenCalled();
      expect(client.loadVault).toHaveBeenCalledWith(files, {});
      expect(result.stats.fingerprint).toBe('fp-cold');
      expect(result.resaved).toBe(true);
      expect((await loadSnapshot('vault-1'))?.fingerprint).toBe('fp-cold');
    });

    it('hydrates from the cache before pushing the files', async () => {
      await saveSnapshot('vault-1', { fingerprint: 'fp-warm', json: '{"schema":1}' });
      const client = fakeClient({ built: 'fp-warm' });
      const order: string[] = [];
      client.loadSnapshot.mockImplementation((blob: SnapshotBlob) => {
        order.push('snapshot');
        return Promise.resolve(stats(blob.fingerprint));
      });
      client.loadVault.mockImplementation((given: VaultFile[]) => {
        order.push('files');
        return Promise.resolve(stats('fp-warm', given.length));
      });

      const result = await hydrateOrBuild(client, 'vault-1', files, {
        onHydrated: () => order.push('hydrated'),
      });

      expect(order).toEqual(['snapshot', 'hydrated', 'files']);
      expect(result.cached?.fingerprint).toBe('fp-warm');
      // The fingerprint did not move, so the cache is left alone.
      expect(result.resaved).toBe(false);
      expect(client.exportSnapshot).not.toHaveBeenCalled();
    });

    it('re-saves when the vault changed under the cache', async () => {
      await saveSnapshot('vault-1', { fingerprint: 'fp-old', json: '{"schema":1}' });
      const client = fakeClient({ built: 'fp-new' });

      const result = await hydrateOrBuild(client, 'vault-1', files);

      expect(result.cached?.fingerprint).toBe('fp-old');
      expect(result.stats.fingerprint).toBe('fp-new');
      expect(result.resaved).toBe(true);
      expect((await loadSnapshot('vault-1'))?.fingerprint).toBe('fp-new');
    });

    it('drops a snapshot the core refuses and still opens the vault', async () => {
      await saveSnapshot('vault-1', { fingerprint: 'fp-bad', json: 'not a snapshot' });
      const client = fakeClient({ built: 'fp-cold' });
      client.loadSnapshot.mockRejectedValue(new Error('schema 0 is not 1'));

      const result = await hydrateOrBuild(client, 'vault-1', files);

      expect(result.cached).toBeNull();
      expect(result.stats.fingerprint).toBe('fp-cold');
      expect((await loadSnapshot('vault-1'))?.fingerprint).toBe('fp-cold');
    });

    it('passes the root label through to the core', async () => {
      const client = fakeClient({ built: 'fp-cold' });

      await hydrateOrBuild(client, 'vault-1', files, { rootLabel: 'demo-shop' });

      expect(client.loadVault).toHaveBeenCalledWith(files, { rootLabel: 'demo-shop' });
    });
  });
});
