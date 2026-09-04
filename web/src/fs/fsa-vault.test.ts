import { describe, expect, it, vi } from 'vitest';

import {
  FsaVault,
  getDirectoryPicker,
  pickDirectory,
  queryPermission,
  requestPermission,
} from './fsa-vault';
import type {
  DirectoryHandleLike,
  FileHandleLike,
  FsPermissionState,
  WritableFileStreamLike,
} from './types';
import { VaultError } from './types';

/** Minimal in-memory stand-in for a `FileSystemFileHandle`. */
class FakeFileHandle implements FileHandleLike {
  readonly kind = 'file' as const;

  constructor(
    readonly name: string,
    public text: string,
    public lastModified = 1,
  ) {}

  getFile(): Promise<File> {
    return Promise.resolve(new File([this.text], this.name, { lastModified: this.lastModified }));
  }

  createWritable(): Promise<WritableFileStreamLike> {
    let buffer = '';
    return Promise.resolve({
      write: (data: string | BufferSource | Blob) => {
        // The vault only ever writes strings; anything else is a test mistake.
        buffer += typeof data === 'string' ? data : '';
        return Promise.resolve();
      },
      close: () => {
        this.text = buffer;
        this.lastModified += 1;
        return Promise.resolve();
      },
    });
  }
}

/** Minimal in-memory stand-in for a `FileSystemDirectoryHandle`. */
class FakeDirectoryHandle implements DirectoryHandleLike {
  readonly kind = 'directory' as const;
  readonly children = new Map<string, FakeDirectoryHandle | FakeFileHandle>();

  constructor(
    readonly name: string,
    public permission: FsPermissionState = 'granted',
  ) {}

  async *entries(): AsyncIterableIterator<[string, DirectoryHandleLike | FileHandleLike]> {
    await Promise.resolve();
    for (const entry of this.children.entries()) yield entry;
  }

  getDirectoryHandle(name: string, options?: { create?: boolean }): Promise<DirectoryHandleLike> {
    const existing = this.children.get(name);
    if (existing instanceof FakeDirectoryHandle) return Promise.resolve(existing);
    if (!options?.create) return Promise.reject(notFound(name));
    const created = new FakeDirectoryHandle(name);
    this.children.set(name, created);
    return Promise.resolve(created);
  }

  getFileHandle(name: string, options?: { create?: boolean }): Promise<FileHandleLike> {
    const existing = this.children.get(name);
    if (existing instanceof FakeFileHandle) return Promise.resolve(existing);
    if (!options?.create) return Promise.reject(notFound(name));
    const created = new FakeFileHandle(name, '');
    this.children.set(name, created);
    return Promise.resolve(created);
  }

  removeEntry(name: string): Promise<void> {
    this.children.delete(name);
    return Promise.resolve();
  }

  queryPermission(): Promise<FsPermissionState> {
    return Promise.resolve(this.permission);
  }

  requestPermission(): Promise<FsPermissionState> {
    this.permission = 'granted';
    return Promise.resolve(this.permission);
  }

  /** Builds a whole tree from `path → contents`. */
  static from(files: Record<string, string>, name = 'acme-repo'): FakeDirectoryHandle {
    const root = new FakeDirectoryHandle(name);
    for (const [path, text] of Object.entries(files)) {
      const parts = path.split('/');
      const file = parts.pop() as string;
      let directory = root;
      for (const part of parts) {
        const next = directory.children.get(part);
        if (next instanceof FakeDirectoryHandle) {
          directory = next;
        } else {
          const created = new FakeDirectoryHandle(part);
          directory.children.set(part, created);
          directory = created;
        }
      }
      directory.children.set(file, new FakeFileHandle(file, text));
    }
    return root;
  }
}

function notFound(name: string): Error {
  const error = new Error(`${name} not found`);
  error.name = 'NotFoundError';
  return error;
}

const tree = {
  'README.md': '# Acme',
  'docs/index.md': '# Docs',
  'docs/.pmngr/project.yaml': 'key: ACME\nname: Acme Platform\n',
  'docs/.pmngr/stories/ACME-US-0001-login.md': '---\nid: ACME-US-0001\n---\n',
  'docs/assets/logo.png': 'binary-bytes',
  '.git/config': '[core]',
  '.github/workflows/ci.yml': 'name: ci',
  'node_modules/left-pad/index.js': 'module.exports = 1;',
  '.DS_Store': 'noise',
};

describe('FsaVault', () => {
  it('walks the tree and reads text files, skipping ignored entries', async () => {
    const vault = new FsaVault(FakeDirectoryHandle.from(tree));

    const files = await vault.readTextFiles();

    expect(files.map((file) => file.path)).toEqual([
      'docs/.pmngr/project.yaml',
      'docs/.pmngr/stories/ACME-US-0001-login.md',
      'docs/index.md',
      'README.md',
    ]);
    expect(vault.hasGit).toBe(true);
    expect(vault.name).toBe('acme-repo');
    expect(vault.capabilities.write).toBe(true);
  });

  it('caches the last walk and reads binary assets on demand', async () => {
    const vault = new FsaVault(FakeDirectoryHandle.from(tree));
    expect(vault.cachedFiles()).toBeNull();

    await vault.readTextFiles();
    expect(vault.cachedFiles()?.length).toBe(4);

    const blob = await vault.readBinary('docs/assets/logo.png');
    await expect(blob.text()).resolves.toBe('binary-bytes');
  });

  it('writes a file, creating the intermediate directories', async () => {
    const root = FakeDirectoryHandle.from(tree);
    const vault = new FsaVault(root);
    await vault.readTextFiles();

    await vault.writeFile('docs/.pmngr/tasks/ACME-T-0001-new.md', 'body');

    const tasks = await root.getDirectoryHandle('docs');
    const pmngr = await tasks.getDirectoryHandle('.pmngr');
    const folder = await pmngr.getDirectoryHandle('tasks');
    const handle = await folder.getFileHandle('ACME-T-0001-new.md');
    await expect((await handle.getFile()).text()).resolves.toBe('body');
    expect(vault.cachedFiles()?.some((file) => file.text === 'body')).toBe(true);
  });

  it('removes and renames files', async () => {
    const root = FakeDirectoryHandle.from(tree);
    const vault = new FsaVault(root);
    await vault.readTextFiles();

    await vault.rename('README.md', 'docs/README.md');
    expect(root.children.has('README.md')).toBe(false);

    await vault.removeFile('docs/index.md');
    const files = await vault.readTextFiles();
    expect(files.map((file) => file.path)).not.toContain('docs/index.md');
    expect(files.map((file) => file.path)).toContain('docs/README.md');
  });

  it('refuses to write when the vault is mounted read-only', async () => {
    const vault = new FsaVault(FakeDirectoryHandle.from(tree), { writable: false });

    await expect(vault.writeFile('docs/index.md', 'nope')).rejects.toBeInstanceOf(VaultError);
    expect(vault.capabilities.write).toBe(false);
  });

  it('diffs a rescan into create, write and remove events', async () => {
    const root = FakeDirectoryHandle.from(tree);
    const vault = new FsaVault(root);
    await vault.readTextFiles();

    const docs = root.children.get('docs') as FakeDirectoryHandle;
    const index = docs.children.get('index.md') as FakeFileHandle;
    index.text = '# Docs, edited';
    index.lastModified = 99;
    docs.children.set('new-page.md', new FakeFileHandle('new-page.md', '# New'));
    root.children.delete('README.md');

    const events = await vault.rescan();

    expect(events).toEqual(
      expect.arrayContaining([
        { op: 'write', path: 'docs/index.md', text: '# Docs, edited' },
        { op: 'create', path: 'docs/new-page.md', text: '# New' },
        { op: 'remove', path: 'README.md' },
      ]),
    );
    expect(events).toHaveLength(3);
    await expect(vault.rescan()).resolves.toEqual([]);
  });

  it('reports and re-requests permission through the handle', async () => {
    const handle = new FakeDirectoryHandle('acme-repo', 'prompt');
    const vault = new FsaVault(handle);

    await expect(vault.queryPermission()).resolves.toBe('prompt');
    await expect(vault.requestPermission()).resolves.toBe('granted');
    await expect(queryPermission(handle)).resolves.toBe('granted');
  });

  it('treats a handle without permission methods as granted', async () => {
    const handle = FakeDirectoryHandle.from({});
    const bare = { ...handle, queryPermission: undefined, requestPermission: undefined };

    await expect(queryPermission(bare as unknown as DirectoryHandleLike)).resolves.toBe('granted');
    await expect(requestPermission(bare as unknown as DirectoryHandleLike)).resolves.toBe(
      'granted',
    );
  });
});

describe('pickDirectory', () => {
  it('rejects with a typed error when the API is missing', async () => {
    expect(getDirectoryPicker()).toBeNull();
    await expect(pickDirectory()).rejects.toBeInstanceOf(VaultError);
  });

  it('wraps the handle returned by the picker', async () => {
    const handle = FakeDirectoryHandle.from({ 'docs/index.md': '# Docs' });
    const picker = vi.fn().mockResolvedValue(handle);
    vi.stubGlobal('showDirectoryPicker', picker);

    const vault = await pickDirectory();

    expect(picker).toHaveBeenCalledWith(expect.objectContaining({ mode: 'readwrite' }));
    await expect(vault.readTextFiles()).resolves.toEqual([
      { path: 'docs/index.md', text: '# Docs' },
    ]);
    vi.unstubAllGlobals();
  });
});
