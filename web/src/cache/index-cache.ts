/**
 * IndexedDB cache for the index snapshot of a vault.
 *
 * The cache is derived data and never a source of truth (AGENTS.md): everything
 * in it can be rebuilt from the Markdown files at any time. Its only job is to
 * let a reopened vault paint its structure before the files have been read
 * again — the story's "restored on reopen without a full re-scan".
 *
 * One record per vault, keyed by the caller's vault id, holding the snapshot the
 * Go core exported plus the fingerprint it was taken at. A fingerprint that no
 * longer matches a freshly built index is what invalidates the entry.
 *
 * The raw IndexedDB API is used on purpose: the wrapper below is smaller than
 * the dependency it would replace. Every operation degrades to a no-op when the
 * browser has no IndexedDB at all (Safari in private mode, hardened profiles),
 * so a missing cache is slow, never broken.
 */
import type { IndexStats, SnapshotBlob, VaultFile } from '@/core-bridge/api';
import type { CoreClient } from '@/core-bridge/client';

import { CACHE_STORE_NAME, isCacheAvailable, request, withStore } from './cache-db';

export { CACHE_DB_NAME, CACHE_DB_VERSION, CACHE_STORE_NAME, isCacheAvailable } from './cache-db';

/** One cached index snapshot. */
export type CachedSnapshot = {
  /** Stable id of the vault, chosen by the caller (a repo id, a folder name). */
  vaultId: string;
  /** Fingerprint the snapshot was taken at; a different one means it is stale. */
  fingerprint: string;
  /** The snapshot itself, exactly as `snapshot.export` produced it. */
  snapshotJson: string;
  /** When the record was written, in epoch milliseconds. */
  savedAt: number;
};

/** The part of the core client this module needs, so tests can pass a stub. */
export type SnapshotClient = Pick<CoreClient, 'loadVault' | 'loadSnapshot' | 'exportSnapshot'>;

/** Stores the snapshot of a vault, replacing any previous one. */
export async function saveSnapshot(
  vaultId: string,
  blob: SnapshotBlob,
  now: () => number = Date.now,
): Promise<CachedSnapshot | null> {
  if (!isCacheAvailable()) return null;
  const record: CachedSnapshot = {
    vaultId,
    fingerprint: blob.fingerprint,
    snapshotJson: blob.json,
    savedAt: now(),
  };
  await withStore(CACHE_STORE_NAME, 'readwrite', async (store) => {
    await request(store.put(record));
  });
  return record;
}

/** Returns the cached snapshot of a vault, or null when there is none. */
export async function loadSnapshot(vaultId: string): Promise<CachedSnapshot | null> {
  if (!isCacheAvailable()) return null;
  const found = await withStore(CACHE_STORE_NAME, 'readonly', (store) => {
    const req = store.get(vaultId) as IDBRequest<CachedSnapshot | undefined>;
    return request(req);
  });
  return found ?? null;
}

/** Drops the cached snapshot of a vault, or the whole cache when no id is given. */
export async function clear(vaultId?: string): Promise<void> {
  if (!isCacheAvailable()) return;
  await withStore(CACHE_STORE_NAME, 'readwrite', async (store) => {
    await request(vaultId === undefined ? store.clear() : store.delete(vaultId));
  });
}

/** What `hydrateOrBuild` did, so the caller can tell a warm boot from a cold one. */
export type HydrateResult = {
  /** Stats of the hydrated snapshot, or null when the cache had nothing usable. */
  cached: IndexStats | null;
  /** Stats of the index built from the real files. */
  stats: IndexStats;
  /** Whether the cache was rewritten because the fingerprint moved. */
  resaved: boolean;
};

export type HydrateOptions = {
  /**
   * Called as soon as the cached snapshot is in the core, before the files are
   * pushed. This is the moment the UI can render the tree and the boards.
   */
  onHydrated?: (stats: IndexStats) => void;
  /** Passed through to `CoreClient.loadVault`. */
  rootLabel?: string;
  /**
   * Repository the snapshot belongs to inside the core workspace. It is
   * normally the same string as the cache key, and it is passed separately so
   * that the cache never has to assume the two are one (GIT-US-0016).
   */
  vaultId?: string;
  /** Passed through to `CoreClient.loadVault`; see ADR-018. */
  docsFolders?: string[];
};

/**
 * Opens a vault as fast as the cache allows.
 *
 * The cached snapshot is loaded first, so the UI has the structure of the vault
 * within milliseconds; the real files are pushed straight afterwards, which
 * replaces the hydrated index with the authoritative one. The cache is rewritten
 * only when the fingerprint changed, so an unchanged vault costs one read.
 *
 * A cache entry that the core refuses is dropped rather than retried: the vault
 * still opens, just cold.
 */
export async function hydrateOrBuild(
  client: SnapshotClient,
  vaultId: string,
  files: VaultFile[],
  options: HydrateOptions = {},
): Promise<HydrateResult> {
  let entry: CachedSnapshot | null = null;
  try {
    entry = await loadSnapshot(vaultId);
  } catch {
    entry = null;
  }

  let cached: IndexStats | null = null;
  if (entry) {
    try {
      cached = await client.loadSnapshot(
        { fingerprint: entry.fingerprint, json: entry.snapshotJson },
        options.vaultId,
      );
      options.onHydrated?.(cached);
    } catch {
      cached = null;
      entry = null;
      await clear(vaultId).catch(() => undefined);
    }
  }

  const stats = await client.loadVault(files, {
    ...(options.rootLabel === undefined ? {} : { rootLabel: options.rootLabel }),
    ...(options.vaultId === undefined ? {} : { vaultId: options.vaultId }),
    ...(options.docsFolders === undefined ? {} : { docsFolders: options.docsFolders }),
  });

  let resaved = false;
  if (!entry || entry.fingerprint !== stats.fingerprint) {
    try {
      const blob = await client.exportSnapshot(options.vaultId);
      await saveSnapshot(vaultId, blob);
      resaved = true;
    } catch {
      // A cache we cannot write is a cache we do without.
      resaved = false;
    }
  }
  return { cached, stats, resaved };
}
