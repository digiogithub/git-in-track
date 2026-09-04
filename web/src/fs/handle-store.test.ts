import 'fake-indexeddb/auto';

import { beforeEach, describe, expect, it } from 'vitest';

import {
  clearHandleRecords,
  getHandleRecord,
  listHandleRecords,
  removeHandleRecord,
  requestPersistentStorage,
  saveHandleRecord,
  supportsHandleStore,
  type RepoHandleRecord,
} from './handle-store';
import type { DirectoryHandleLike } from './types';

/**
 * Real directory handles are structured-clonable but cannot be constructed in
 * a test environment, so the record carries a plain serialisable stand-in.
 */
function record(id: string, name = id): RepoHandleRecord {
  return {
    id,
    name,
    handle: { kind: 'directory', name } as unknown as DirectoryHandleLike,
    docsFolder: 'docs',
    kind: 'project',
    projects: ['ACME'],
  };
}

describe('handle store', () => {
  beforeEach(async () => {
    await clearHandleRecords();
  });

  it('detects IndexedDB support', () => {
    expect(supportsHandleStore()).toBe(true);
  });

  it('persists a record and reads it back', async () => {
    await saveHandleRecord(record('repo-1', 'acme-repo'));

    const stored = await getHandleRecord('repo-1');
    expect(stored?.name).toBe('acme-repo');
    expect(stored?.docsFolder).toBe('docs');
    expect(stored?.handle.name).toBe('acme-repo');
  });

  it('lists records ordered by id', async () => {
    await saveHandleRecord(record('repo-2'));
    await saveHandleRecord(record('repo-1'));

    await expect(listHandleRecords()).resolves.toMatchObject([{ id: 'repo-1' }, { id: 'repo-2' }]);
  });

  it('replaces a record with the same id', async () => {
    await saveHandleRecord(record('repo-1'));
    await saveHandleRecord({ ...record('repo-1'), docsFolder: 'documentation' });

    const stored = await listHandleRecords();
    expect(stored).toHaveLength(1);
    expect(stored[0]?.docsFolder).toBe('documentation');
  });

  it('removes one record and clears them all', async () => {
    await saveHandleRecord(record('repo-1'));
    await saveHandleRecord(record('repo-2'));

    await removeHandleRecord('repo-1');
    await expect(getHandleRecord('repo-1')).resolves.toBeUndefined();
    await expect(listHandleRecords()).resolves.toHaveLength(1);

    await clearHandleRecords();
    await expect(listHandleRecords()).resolves.toEqual([]);
  });

  it('reports false when persistent storage is unavailable', async () => {
    await expect(requestPersistentStorage()).resolves.toBe(false);
  });
});
