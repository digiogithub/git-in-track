/**
 * IndexedDB store for the verification cache of browser-only mode (GIT-US-0150).
 *
 * The verification cache (ADR-037 §7, docs/03 R-REQ-11 and R-REQ-11b) is the
 * per-requirement evidence `gintrack spec ingest` records: natively the file
 * `<docs>/.pmngr/verify.json`, in the browser the same versioned document held
 * by the Go core's `core.MemVerifyCache`. Both sit behind the core interface
 * `core.VerifyCache`; the browser half leaves persistence to its host, which
 * keeps the document `MemVerifyCache.Export()` returns and hands it back to
 * `core.NewMemVerifyCache` on reopen. This module is that host storage: one
 * record per project of a mounted repository, in the same database as the
 * index snapshots (`cache-db.ts`).
 *
 * The document is stored as opaque text. Decoding, validation, merging and
 * the "corrupt or of another version reads as empty" rule all live in the core
 * (`DecodeVerifyCache`), so nothing here duplicates them (AGENTS.md).
 *
 * It is derived data and never the source of truth: deleting the record loses
 * only evidence that was never promoted to a `verified:` stamp. Every
 * operation therefore swallows its failure — no IndexedDB, a blocked upgrade,
 * a quota error — and reports "nothing cached" instead, so a broken cache
 * never breaks the page.
 *
 * What it enables today: browser-only mode has no test ingest and no coverage
 * host (`listCoverage` answers `unavailable`), so nothing in the app records
 * into or reads from this store yet. It is the persistence a browser-side
 * ingest or coverage reader will use, so that evidence recorded in a tab
 * survives a reload instead of living only in the worker's memory.
 */
import { VERIFY_STORE_NAME, isCacheAvailable, request, withStore } from './cache-db';

/** One persisted verification cache document. */
export type CachedVerifyDocument = {
  /** Record key: the vault id and the project key, see `verifyCacheKey`. */
  key: string;
  /** Stable id of the mounted repository, as the index cache uses it. */
  vaultId: string;
  /** Project key inside that repository (`GIT`, `ACME`, …). */
  project: string;
  /** The document exactly as `core.MemVerifyCache.Export()` produced it. */
  document: string;
  /** When the record was written, in epoch milliseconds. */
  savedAt: number;
};

/**
 * Host-side storage of one project's verification cache document: the
 * persistence half of the core's `MemVerifyCache`. `load` returns what
 * `NewMemVerifyCache` should be given (null for an empty cache); `save` keeps
 * what `Export` returned. Neither ever rejects.
 */
export interface VerifyCacheStore {
  load(vaultId: string, project: string): Promise<string | null>;
  save(vaultId: string, project: string, document: string): Promise<boolean>;
  clear(vaultId?: string, project?: string): Promise<void>;
}

// A separator that cannot appear in a vault id or a project key, so the keys
// of one vault form a contiguous range.
const SEP = '\u0000';

/** The record key of one project of one vault. */
export function verifyCacheKey(vaultId: string, project: string): string {
  return `${vaultId}${SEP}${project}`;
}

/**
 * Returns the persisted document of a project, or null when there is none or
 * the store cannot be read. The caller hands it to the core unparsed.
 */
export async function loadVerifyDocument(
  vaultId: string,
  project: string,
): Promise<string | null> {
  if (!isCacheAvailable()) return null;
  try {
    const found = await withStore(VERIFY_STORE_NAME, 'readonly', (store) => {
      const req = store.get(verifyCacheKey(vaultId, project)) as IDBRequest<
        CachedVerifyDocument | undefined
      >;
      return request(req);
    });
    return typeof found?.document === 'string' && found.document !== '' ? found.document : null;
  } catch {
    return null;
  }
}

/**
 * Persists the document of a project, replacing any previous one. An empty
 * document — a cache that never recorded anything — removes the record.
 * Resolves true when the record was written or removed, false when the store
 * could not be used.
 */
export async function saveVerifyDocument(
  vaultId: string,
  project: string,
  document: string,
  now: () => number = Date.now,
): Promise<boolean> {
  if (!isCacheAvailable()) return false;
  const key = verifyCacheKey(vaultId, project);
  try {
    await withStore(VERIFY_STORE_NAME, 'readwrite', async (store) => {
      if (document === '') {
        await request(store.delete(key));
        return;
      }
      const record: CachedVerifyDocument = { key, vaultId, project, document, savedAt: now() };
      await request(store.put(record));
    });
    return true;
  } catch {
    return false;
  }
}

/**
 * Drops persisted documents: one project's, every project of a vault, or the
 * whole store when no vault is given. Failures are ignored.
 */
export async function clearVerifyDocuments(vaultId?: string, project?: string): Promise<void> {
  if (!isCacheAvailable()) return;
  try {
    await withStore(VERIFY_STORE_NAME, 'readwrite', async (store) => {
      if (vaultId === undefined) {
        await request(store.clear());
      } else if (project !== undefined) {
        await request(store.delete(verifyCacheKey(vaultId, project)));
      } else {
        const prefix = `${vaultId}${SEP}`;
        await request(store.delete(IDBKeyRange.bound(prefix, `${prefix}￿`)));
      }
    });
  } catch {
    // A cache we cannot clear is stale derived data, rebuilt by the next ingest.
  }
}

/** The IndexedDB-backed `VerifyCacheStore` of browser-only mode. */
export const indexedDbVerifyCacheStore: VerifyCacheStore = {
  load: loadVerifyDocument,
  save: (vaultId, project, document) => saveVerifyDocument(vaultId, project, document),
  clear: clearVerifyDocuments,
};
