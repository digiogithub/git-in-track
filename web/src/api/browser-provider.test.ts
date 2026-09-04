import { beforeEach, describe, expect, it, vi } from 'vitest';

import { BrowserProvider } from '@/api/browser-provider';
import { ProviderError, type ChangeEvent, type Item, type ProjectSummary } from '@/api/provider';
import type { CoreMethodName } from '@/core-bridge/api';
import type { CoreClient } from '@/core-bridge/client';
import {
  clearHandleRecords,
  clearVaultRegistry,
  MemoryVault,
  registerVault,
  saveHandleRecord,
  type DirectoryHandleLike,
  type FsPermissionState,
  type RepoHandleRecord,
} from '@/fs';

/**
 * The handle store is exercised by its own test against `fake-indexeddb`.
 * Here it is replaced by an array, because a fake handle carrying permission
 * methods is not structured-clonable (real browser handles are).
 */
const store = vi.hoisted(() => ({ records: [] as RepoHandleRecord[] }));

vi.mock('@/fs/handle-store', () => ({
  supportsHandleStore: () => true,
  listHandleRecords: () =>
    Promise.resolve([...store.records].sort((a, b) => a.id.localeCompare(b.id))),
  getHandleRecord: (id: string) => Promise.resolve(store.records.find((r) => r.id === id)),
  saveHandleRecord: (record: RepoHandleRecord) => {
    store.records = [...store.records.filter((r) => r.id !== record.id), record];
    return Promise.resolve();
  },
  removeHandleRecord: (id: string) => {
    store.records = store.records.filter((r) => r.id !== id);
    return Promise.resolve();
  },
  clearHandleRecords: () => {
    store.records = [];
    return Promise.resolve();
  },
  requestPersistentStorage: () => Promise.resolve(false),
}));

type Handler = (params: unknown) => unknown;

/** A `CoreClient` stand-in: the worker contract is one `call(method, params)`. */
function fakeClient(handlers: Partial<Record<CoreMethodName, Handler>>) {
  const call = vi.fn((method: string, params: unknown) => {
    const handler = handlers[method as CoreMethodName];
    if (!handler) {
      return Promise.reject(coreError('unknown_method', `no handler for ${method}`));
    }
    return Promise.resolve(handler(params));
  });
  // The typed helpers the snapshot cache uses are thin wrappers over `call`,
  // exactly as `CoreClient` implements them.
  const client = {
    call,
    loadVault: (files: unknown, options: { rootLabel?: string } = {}) =>
      call('vault.load', { files, ...options }),
    loadSnapshot: (blob: unknown) => call('snapshot.load', blob),
    exportSnapshot: () => call('snapshot.export', undefined),
  } as unknown as CoreClient;
  return { client, call };
}

/** The error envelope shape the worker rejects with (`{ code, message }`). */
function coreError(code: string, message: string): Error {
  return Object.assign(new Error(message), { code });
}

const stats = {
  projects: 1,
  items: 1,
  pages: 1,
  comments: 0,
  durationMs: 3,
  fingerprint: 'fp-1',
  diagnostics: [],
};

const project: ProjectSummary = {
  key: 'ACME',
  name: 'Acme Platform',
  docsPath: 'docs',
  statuses: [{ id: 'todo', name: 'To Do', category: 'todo' }],
  labels: [],
  priorities: ['high'],
  itemCounts: { epic: 0, story: 1, task: 0, milestone: 0, comment: 0 },
};

const story: Item = {
  id: 'ACME-US-0001',
  type: 'story',
  title: 'Login with SSO',
  status: 'todo',
  body: '## Description\n',
  path: 'docs/.pmngr/stories/ACME-US-0001-login-with-sso.md',
  rev: 'sha256:0000000000000001',
};

function vaultFiles(): Record<string, string> {
  return {
    'docs/.pmngr/project.yaml': 'key: ACME\nname: Acme Platform\n',
    'docs/.pmngr/stories/ACME-US-0001-login-with-sso.md': '---\nid: ACME-US-0001\n---\n',
    'docs/index.md': '# Docs\n',
  };
}

/** Mounts a memory vault and returns everything the test needs to assert on. */
async function mount(handlers: Partial<Record<CoreMethodName, Handler>> = {}, writable = true) {
  const vault = new MemoryVault(vaultFiles(), { name: 'acme-repo', writable });
  const { client, call } = fakeClient({
    'vault.load': () => stats,
    'snapshot.export': () => ({ fingerprint: stats.fingerprint, json: '{}' }),
    'vault.apply': () => stats,
    'vault.stats': () => stats,
    'project.list': () => [project],
    ...handlers,
  });
  const provider = new BrowserProvider({ client });
  const id = registerVault(vault, 'repo-1');
  const events: ChangeEvent[] = [];
  provider.subscribe((event) => events.push(event));
  const repo = await provider.mountRepo({ kind: 'project', location: id, docsFolder: 'docs' });
  return { provider, vault, call, events, repo };
}

describe('BrowserProvider', () => {
  beforeEach(async () => {
    clearVaultRegistry();
    await clearHandleRecords();
  });

  it('mounts a folder by pushing its files into the core', async () => {
    const { repo, call, events } = await mount();

    expect(call).toHaveBeenCalledWith('vault.load', {
      files: [
        { path: 'docs/.pmngr/project.yaml', text: 'key: ACME\nname: Acme Platform\n' },
        {
          path: 'docs/.pmngr/stories/ACME-US-0001-login-with-sso.md',
          text: '---\nid: ACME-US-0001\n---\n',
        },
        { path: 'docs/index.md', text: '# Docs\n' },
      ],
      rootLabel: 'acme-repo',
    });
    expect(repo).toMatchObject({
      id: 'repo-1',
      name: 'acme-repo',
      docsFolder: 'docs',
      state: 'ready',
      projects: ['ACME'],
    });
    expect(events.map((event) => event.kind)).toEqual(['repo', 'index']);
  });

  it('reports a mounted fallback folder in listRepos', async () => {
    const { provider } = await mount({}, false);

    await expect(provider.listRepos()).resolves.toMatchObject([
      { id: 'repo-1', name: 'acme-repo', state: 'ready', projects: ['ACME'] },
    ]);
    expect(provider.capabilities.write).toBe(false);
    expect(provider.capabilities.maxBatchWrite).toBe(0);
  });

  it('marks a persisted repository as needs-permission when the grant expired', async () => {
    const handle = {
      kind: 'directory',
      name: 'acme-repo',
      queryPermission: () => Promise.resolve('prompt' as FsPermissionState),
    } as unknown as DirectoryHandleLike;
    await saveHandleRecord({
      id: 'repo-9',
      name: 'acme-repo',
      handle,
      docsFolder: 'docs',
      kind: 'project',
      projects: ['ACME'],
    });
    const { client } = fakeClient({});

    const repos = await new BrowserProvider({ client }).listRepos();

    expect(repos).toHaveLength(1);
    expect(repos[0]?.state).toBe('needs-permission');
  });

  it('delegates reads to the core', async () => {
    const { provider, call } = await mount({
      'item.list': () => ({ items: [story], total: 1 }),
      'item.get': () => story,
      'kb.tree': () => [{ path: 'docs/index.md', name: 'index.md', kind: 'page' }],
      'kb.page': () => ({
        path: 'docs/index.md',
        title: 'Docs',
        frontMatter: {},
        body: '# Docs',
        rev: 'sha256:00000000000000a1',
        outgoing: [],
        backlinks: [],
      }),
      search: () => [
        { kind: 'item', id: story.id, path: story.path, title: story.title, snippet: '', score: 1 },
      ],
    });

    await expect(provider.listItems({ project: 'ACME' })).resolves.toMatchObject({ total: 1 });
    await expect(provider.getItem('ACME-US-0001')).resolves.toMatchObject({
      title: 'Login with SSO',
    });
    await expect(
      provider.listKbTree({ kind: 'project', projectKey: 'ACME' }),
    ).resolves.toHaveLength(1);
    await expect(
      provider.getPage({ kind: 'project', projectKey: 'ACME' }, 'docs/index.md'),
    ).resolves.toMatchObject({ title: 'Docs' });
    await expect(provider.search({ text: 'sso', limit: 5 })).resolves.toHaveLength(1);

    expect(call).toHaveBeenCalledWith('kb.tree', { project: 'ACME' });
    expect(call).toHaveBeenCalledWith('search', { q: 'sso', limit: 5 });
  });

  it('reads assets from the vault, never from the core', async () => {
    const { provider, vault } = await mount();
    vault.putBinary('docs/assets/logo.png', new Blob(['png-bytes']));

    const blob = await provider.readAsset(
      { kind: 'project', projectKey: 'ACME' },
      'docs/assets/logo.png',
    );

    await expect(blob.text()).resolves.toBe('png-bytes');
  });

  it('persists the WriteSet returned by a write and emits a change event', async () => {
    const updated: Item = {
      ...story,
      title: 'Login with SSO (v2)',
      rev: 'sha256:0000000000000002',
    };
    const { provider, vault, events } = await mount({
      'item.update': () => ({
        item: updated,
        writes: {
          written: [{ path: updated.path, text: '---\nid: ACME-US-0001\ntitle: v2\n---\n' }],
          removed: ['docs/index.md'],
        },
      }),
    });

    const result = await provider.updateItem(
      'ACME-US-0001',
      { set: { title: 'Login with SSO (v2)' } },
      story.rev,
    );

    expect(result.rev).toBe('sha256:0000000000000002');
    expect(vault.snapshot()[updated.path]).toContain('title: v2');
    expect(vault.snapshot()['docs/index.md']).toBeUndefined();
    expect(events.at(-1)).toEqual({ kind: 'items', repoId: 'repo-1', ids: ['ACME-US-0001'] });
  });

  it('maps a stale revision from the core onto a typed provider error', async () => {
    const { provider } = await mount({
      'item.update': () => {
        throw coreError('stale_revision', 'ACME-US-0001 changed on disk');
      },
    });

    await expect(
      provider.updateItem('ACME-US-0001', { set: { title: 'x' } }, 'sha256:stale'),
    ).rejects.toMatchObject({ name: 'ProviderError', code: 'stale_revision' });
  });

  it('maps validation, not-found and unknown core codes', async () => {
    const { provider } = await mount({
      'item.create': () => {
        throw coreError('validation_failed', 'title is required');
      },
      'item.get': () => {
        throw coreError('not_found', 'no such item');
      },
      'item.delete': () => {
        throw coreError('boom', 'core exploded');
      },
    });

    await expect(
      provider.createItem({ project: 'ACME', type: 'story', title: '' }),
    ).rejects.toMatchObject({ code: 'validation_failed' });
    await expect(provider.getItem('ACME-US-9999')).rejects.toMatchObject({ code: 'not_found' });
    await expect(provider.deleteItem('ACME-US-0001', story.rev)).rejects.toMatchObject({
      code: 'internal',
    });
  });

  it('refuses writes on a read-only vault before calling the core', async () => {
    const { provider, call } = await mount({}, false);

    await expect(
      provider.updateItem('ACME-US-0001', { set: { title: 'x' } }, story.rev),
    ).rejects.toMatchObject({ code: 'read_only' });
    expect(call).not.toHaveBeenCalledWith('item.update', expect.anything());
  });

  it('reindexes incrementally from the rescan diff', async () => {
    const { provider, vault, call, events } = await mount();
    vault.setExternal('docs/index.md', '# Docs, edited\n');
    vault.removeExternal('docs/.pmngr/stories/ACME-US-0001-login-with-sso.md');

    await provider.reindex('repo-1');

    expect(call).toHaveBeenCalledWith('vault.apply', {
      events: [
        { op: 'write', path: 'docs/index.md', text: '# Docs, edited\n' },
        { op: 'remove', path: 'docs/.pmngr/stories/ACME-US-0001-login-with-sso.md' },
      ],
    });
    expect(events.some((event) => event.kind === 'index')).toBe(true);
  });

  it('falls back to vault.stats when nothing changed', async () => {
    const { provider, call } = await mount();

    await provider.reindex('repo-1');

    expect(call).toHaveBeenCalledWith('vault.stats', undefined);
  });

  it('reloads the whole vault when a full reindex is requested', async () => {
    const { provider, call } = await mount();
    call.mockClear();

    await provider.reindex('repo-1', { full: true });

    expect(call).toHaveBeenCalledWith(
      'vault.load',
      expect.objectContaining({ rootLabel: 'acme-repo' }),
    );
  });

  it('unmounts a repository and then refuses reads', async () => {
    const { provider } = await mount();

    await provider.unmountRepo('repo-1');
    clearVaultRegistry();

    await expect(provider.listItems({})).rejects.toBeInstanceOf(ProviderError);
    await expect(provider.listRepos()).resolves.toEqual([]);
  });

  it('collects per-item failures in updateMany', async () => {
    let calls = 0;
    const { provider } = await mount({
      'item.update': () => {
        calls += 1;
        if (calls === 1) {
          return { item: story, writes: { written: [], removed: [] } };
        }
        throw coreError('stale_revision', 'changed on disk');
      },
    });

    const result = await provider.updateMany([
      { id: 'ACME-US-0001', patch: { set: { status: 'todo' } }, rev: story.rev },
      { id: 'ACME-US-0002', patch: { set: { status: 'todo' } }, rev: story.rev },
    ]);

    expect(result.applied).toBe(1);
    expect(result.failed).toMatchObject([{ id: 'ACME-US-0002', code: 'stale_revision' }]);
  });
});
