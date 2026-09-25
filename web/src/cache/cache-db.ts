/**
 * The IndexedDB database every derived browser cache lives in.
 *
 * ADR-037 §7 and docs/03 R-REQ-11 put the verification cache "in the same
 * IndexedDB database as the WASM index", so the index snapshots
 * (`index-cache.ts`) and the verification documents (`verify-cache.ts`) share
 * one database with one object store each. The upgrade handler below is the
 * single place both stores are created, so neither module can open the
 * database at a version the other does not know.
 *
 * Everything stored here is derived data and never a source of truth
 * (AGENTS.md): it can be deleted at any time and is rebuilt from the files.
 */

export const CACHE_DB_NAME = 'gintrack-cache';
/**
 * Version 1 held the index snapshots only; version 2 adds the verification
 * cache store (GIT-US-0150). An upgrade only creates missing stores, so a
 * version-1 database keeps its snapshots.
 */
export const CACHE_DB_VERSION = 2;
export const CACHE_STORE_NAME = 'index-snapshots';
export const VERIFY_STORE_NAME = 'verify-caches';

/** Reports whether this browser exposes IndexedDB at all. */
export function isCacheAvailable(): boolean {
  return typeof indexedDB !== 'undefined' && indexedDB !== null;
}

/** Wraps one IDBRequest in a promise. */
export function request<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    req.onsuccess = () => {
      resolve(req.result);
    };
    req.onerror = () => {
      reject(req.error ?? new Error('IndexedDB request failed'));
    };
  });
}

/** Opens (and, on a version bump, upgrades) the cache database. */
function openDatabase(): Promise<IDBDatabase> {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const open = indexedDB.open(CACHE_DB_NAME, CACHE_DB_VERSION);
    open.onupgradeneeded = () => {
      const db = open.result;
      if (!db.objectStoreNames.contains(CACHE_STORE_NAME)) {
        db.createObjectStore(CACHE_STORE_NAME, { keyPath: 'vaultId' });
      }
      if (!db.objectStoreNames.contains(VERIFY_STORE_NAME)) {
        db.createObjectStore(VERIFY_STORE_NAME, { keyPath: 'key' });
      }
    };
    open.onsuccess = () => {
      const db = open.result;
      // Another tab upgrading the database must not wait on this connection.
      db.onversionchange = () => {
        db.close();
      };
      resolve(db);
    };
    open.onerror = () => {
      reject(open.error ?? new Error('cannot open the cache database'));
    };
    open.onblocked = () => {
      reject(new Error('the cache database is blocked by another tab'));
    };
  });
}

/** Runs one transaction against one store of the cache and closes the connection. */
export async function withStore<T>(
  storeName: string,
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore) => Promise<T>,
): Promise<T> {
  const db = await openDatabase();
  try {
    const tx = db.transaction(storeName, mode);
    const done = new Promise<void>((resolve, reject) => {
      tx.oncomplete = () => {
        resolve();
      };
      tx.onerror = () => {
        reject(tx.error ?? new Error('cache transaction failed'));
      };
      tx.onabort = () => {
        reject(tx.error ?? new Error('cache transaction aborted'));
      };
    });
    const value = await run(tx.objectStore(storeName));
    await done;
    return value;
  } finally {
    db.close();
  }
}
