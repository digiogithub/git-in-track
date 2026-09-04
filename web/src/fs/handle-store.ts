/**
 * IndexedDB persistence for picked directory handles (docs/05-web-app.md §6.1).
 *
 * Directory handles are structured-clonable, so a repository mounted today
 * reappears on the next visit without another picker dialog. The *permission*
 * is not part of the record: it is re-checked on boot and, when it has
 * expired, re-requested inside one user gesture.
 *
 * This is derived state: deleting the database only costs the user one folder
 * pick, never any data (AGENTS.md — files are the only source of truth).
 */

import type { DirectoryHandleLike } from './types';

export const HANDLE_DB_NAME = 'gintrack';
export const HANDLE_DB_VERSION = 1;
export const HANDLE_STORE_NAME = 'handles';

export type RepoHandleKind = 'project' | 'team';

export type RepoHandleRecord = {
  /** Stable repo id, also used as the provider's `RepoInfo.id`. */
  id: string;
  /** Folder name shown in the UI. */
  name: string;
  /** The persisted File System Access handle. */
  handle: DirectoryHandleLike;
  /** Vault-relative path of the documentation folder (`''` for the root). */
  docsFolder: string;
  kind: RepoHandleKind;
  /** Project keys found during the last index, for the workspace card. */
  projects?: string[];
  mountedAt?: string;
  lastIndexedAt?: string;
};

function request<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => {
      resolve(req.result);
    };
    req.onerror = () => {
      reject(req.error ?? new Error('IndexedDB request failed'));
    };
  });
}

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open(HANDLE_DB_NAME, HANDLE_DB_VERSION);
    open.onupgradeneeded = () => {
      const db = open.result;
      if (!db.objectStoreNames.contains(HANDLE_STORE_NAME)) {
        db.createObjectStore(HANDLE_STORE_NAME, { keyPath: 'id' });
      }
    };
    open.onsuccess = () => {
      resolve(open.result);
    };
    open.onerror = () => {
      reject(open.error ?? new Error('Cannot open the gintrack database'));
    };
  });
}

async function withStore<T>(
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore) => Promise<T>,
): Promise<T> {
  const db = await openDatabase();
  try {
    const transaction = db.transaction(HANDLE_STORE_NAME, mode);
    const result = await run(transaction.objectStore(HANDLE_STORE_NAME));
    await new Promise<void>((resolve, reject) => {
      transaction.oncomplete = () => {
        resolve();
      };
      transaction.onabort = () => {
        reject(transaction.error ?? new Error('IndexedDB transaction aborted'));
      };
      transaction.onerror = () => {
        reject(transaction.error ?? new Error('IndexedDB transaction failed'));
      };
    });
    return result;
  } finally {
    db.close();
  }
}

/** True when this environment has IndexedDB (it is absent in some sandboxes). */
export function supportsHandleStore(): boolean {
  return typeof indexedDB !== 'undefined';
}

/** Inserts or replaces one repository record. */
export async function saveHandleRecord(record: RepoHandleRecord): Promise<void> {
  await withStore('readwrite', async (store) => {
    await request(store.put(record));
  });
}

/** Every mounted repository, ordered by id for a stable workspace list. */
export async function listHandleRecords(): Promise<RepoHandleRecord[]> {
  if (!supportsHandleStore()) return [];
  const records = await withStore('readonly', (store) =>
    request(store.getAll() as IDBRequest<RepoHandleRecord[]>),
  );
  return records.sort((a, b) => a.id.localeCompare(b.id));
}

export async function getHandleRecord(id: string): Promise<RepoHandleRecord | undefined> {
  if (!supportsHandleStore()) return undefined;
  return withStore('readonly', (store) =>
    request(store.get(id) as IDBRequest<RepoHandleRecord | undefined>),
  );
}

export async function removeHandleRecord(id: string): Promise<void> {
  if (!supportsHandleStore()) return;
  await withStore('readwrite', async (store) => {
    await request(store.delete(id));
  });
}

export async function clearHandleRecords(): Promise<void> {
  if (!supportsHandleStore()) return;
  await withStore('readwrite', async (store) => {
    await request(store.clear());
  });
}

/**
 * Asks the browser to keep our IndexedDB data out of the eviction path. Best
 * effort: a refusal only means the index cache may have to be rebuilt.
 */
export async function requestPersistentStorage(): Promise<boolean> {
  const storage = (globalThis as { navigator?: { storage?: { persist?: () => Promise<boolean> } } })
    .navigator?.storage;
  if (!storage?.persist) return false;
  try {
    return await storage.persist();
  } catch {
    return false;
  }
}
