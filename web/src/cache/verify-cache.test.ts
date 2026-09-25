import 'fake-indexeddb/auto';

import { IDBFactory } from 'fake-indexeddb';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { CACHE_DB_NAME, CACHE_STORE_NAME, VERIFY_STORE_NAME } from './cache-db';
import { loadSnapshot } from './index-cache';
import {
  clearVerifyDocuments,
  indexedDbVerifyCacheStore,
  loadVerifyDocument,
  saveVerifyDocument,
  verifyCacheKey,
} from './verify-cache';

/** A document shaped as core.MemVerifyCache.Export() renders it (docs/03 R-REQ-11b). */
const doc = `{
  "version": 1,
  "entries": [
    {
      "ref": "ACME-SP-0003.R2",
      "rev": "sha256:9f2c",
      "commit": "4b1d000000000000000000000000000000000000",
      "tests": [{ "test": "src/alloc_test.go#TestNextID", "result": "pass" }],
      "result": "pass",
      "at": "2026-09-24T10:00:00Z",
      "by": "jose"
    }
  ]
}
`;

const realIndexedDB = globalThis.indexedDB;

describe('verification cache store', () => {
  beforeEach(() => {
    // A fresh factory per test: IndexedDB state must not leak between cases.
    globalThis.indexedDB = new IDBFactory();
  });

  afterEach(() => {
    globalThis.indexedDB = realIndexedDB;
  });

  it('returns null for a project that never recorded anything', async () => {
    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBeNull();
  });

  it('round-trips the exported document byte for byte', async () => {
    await expect(saveVerifyDocument('vault-1', 'ACME', doc, () => 7)).resolves.toBe(true);
    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBe(doc);
  });

  it('keeps one record per project of each vault', async () => {
    await saveVerifyDocument('vault-1', 'ACME', 'acme');
    await saveVerifyDocument('vault-1', 'OPS', 'ops');
    await saveVerifyDocument('vault-2', 'ACME', 'other');

    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBe('acme');
    await expect(loadVerifyDocument('vault-1', 'OPS')).resolves.toBe('ops');
    await expect(loadVerifyDocument('vault-2', 'ACME')).resolves.toBe('other');
  });

  it('replaces the previous document of a project', async () => {
    await saveVerifyDocument('vault-1', 'ACME', 'old');
    await saveVerifyDocument('vault-1', 'ACME', 'new');
    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBe('new');
  });

  it('removes the record when the exported cache is empty', async () => {
    await saveVerifyDocument('vault-1', 'ACME', doc);
    await expect(saveVerifyDocument('vault-1', 'ACME', '')).resolves.toBe(true);
    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBeNull();
  });

  it('clears one project, one vault, or everything', async () => {
    await saveVerifyDocument('vault-1', 'ACME', 'a');
    await saveVerifyDocument('vault-1', 'OPS', 'b');
    await saveVerifyDocument('vault-10', 'ACME', 'c');
    await saveVerifyDocument('vault-2', 'ACME', 'd');

    await clearVerifyDocuments('vault-1', 'ACME');
    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBeNull();
    await expect(loadVerifyDocument('vault-1', 'OPS')).resolves.toBe('b');

    // A vault id that is a prefix of another must not take the other with it.
    await clearVerifyDocuments('vault-1');
    await expect(loadVerifyDocument('vault-1', 'OPS')).resolves.toBeNull();
    await expect(loadVerifyDocument('vault-10', 'ACME')).resolves.toBe('c');

    await clearVerifyDocuments();
    await expect(loadVerifyDocument('vault-10', 'ACME')).resolves.toBeNull();
    await expect(loadVerifyDocument('vault-2', 'ACME')).resolves.toBeNull();
  });

  it('exposes the same operations as a VerifyCacheStore', async () => {
    const store = indexedDbVerifyCacheStore;
    await expect(store.save('vault-1', 'ACME', doc)).resolves.toBe(true);
    await expect(store.load('vault-1', 'ACME')).resolves.toBe(doc);
    await store.clear('vault-1');
    await expect(store.load('vault-1', 'ACME')).resolves.toBeNull();
  });

  it('degrades to an empty cache when the browser has no IndexedDB', async () => {
    // @ts-expect-error: simulate a browser without IndexedDB.
    globalThis.indexedDB = undefined;
    await expect(saveVerifyDocument('vault-1', 'ACME', doc)).resolves.toBe(false);
    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBeNull();
    await expect(clearVerifyDocuments()).resolves.toBeUndefined();
  });

  it('never rejects when IndexedDB refuses to open', async () => {
    const broken = {
      open: () => {
        throw new DOMException('blocked by the profile', 'SecurityError');
      },
    };
    globalThis.indexedDB = broken as unknown as IDBFactory;
    await expect(saveVerifyDocument('vault-1', 'ACME', doc)).resolves.toBe(false);
    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBeNull();
    await expect(clearVerifyDocuments('vault-1')).resolves.toBeUndefined();
  });

  it('upgrades a version-1 cache database without losing the index snapshots', async () => {
    // The database as the index cache created it before GIT-US-0150.
    await new Promise<void>((resolve, reject) => {
      const open = indexedDB.open(CACHE_DB_NAME, 1);
      open.onupgradeneeded = () => {
        const store = open.result.createObjectStore(CACHE_STORE_NAME, { keyPath: 'vaultId' });
        store.put({ vaultId: 'vault-1', fingerprint: 'fp', snapshotJson: '{}', savedAt: 1 });
      };
      open.onsuccess = () => {
        open.result.close();
        resolve();
      };
      open.onerror = () => {
        reject(open.error ?? new Error('open failed'));
      };
    });

    await expect(saveVerifyDocument('vault-1', 'ACME', doc)).resolves.toBe(true);
    await expect(loadVerifyDocument('vault-1', 'ACME')).resolves.toBe(doc);
    await expect(loadSnapshot('vault-1')).resolves.toMatchObject({ fingerprint: 'fp' });
  });

  it('stores records under the documented key and store', async () => {
    await saveVerifyDocument('vault-1', 'ACME', doc, () => 42);
    const record = await new Promise<unknown>((resolve, reject) => {
      const open = indexedDB.open(CACHE_DB_NAME);
      open.onsuccess = () => {
        const db = open.result;
        const req = db.transaction(VERIFY_STORE_NAME).objectStore(VERIFY_STORE_NAME).get(
          verifyCacheKey('vault-1', 'ACME'),
        );
        req.onsuccess = () => {
          db.close();
          resolve(req.result);
        };
        req.onerror = () => {
          reject(req.error ?? new Error('get failed'));
        };
      };
      open.onerror = () => {
        reject(open.error ?? new Error('open failed'));
      };
    });
    expect(record).toEqual({
      key: verifyCacheKey('vault-1', 'ACME'),
      vaultId: 'vault-1',
      project: 'ACME',
      document: doc,
      savedAt: 42,
    });
  });
});
