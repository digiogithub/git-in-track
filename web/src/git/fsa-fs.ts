/**
 * The `fs` adapter `isomorphic-git` runs on in browser-only mode
 * (docs/06-git-sync.md §6.1).
 *
 * isomorphic-git needs a small `promises` subset of Node's `fs`; the File
 * System Access API gives us directory and file handles instead. This module
 * bridges the two, over the same `DirectoryHandleLike` the vault already holds,
 * so git reads and writes the user's real repository folder — objects included
 * — and no copy of it lives anywhere else.
 *
 * What the library actually calls, and what it is given here: `readFile`,
 * `writeFile`, `unlink`, `readdir`, `mkdir`, `rmdir`, `stat`, `lstat`,
 * `readlink` and `symlink`. Symlinks do not exist in this API, so the last two
 * fail the way they do on a filesystem without them, which is what
 * isomorphic-git expects.
 */

import type { DirectoryHandleLike, FileHandleLike } from '@/fs/types';

/** The error shape isomorphic-git branches on: `err.code === 'ENOENT'`. */
export class FsError extends Error {
  readonly code: string;

  constructor(code: string, path: string, message?: string) {
    super(message ?? `${code}: ${path}`);
    this.name = 'FsError';
    this.code = code;
  }
}

/** The stat shape isomorphic-git reads. */
export type FsStat = {
  type: 'file' | 'dir';
  mode: number;
  size: number;
  ino: number;
  mtimeMs: number;
  ctimeMs: number;
  uid: number;
  gid: number;
  dev: number;
  isFile(): boolean;
  isDirectory(): boolean;
  isSymbolicLink(): boolean;
};

/** Options isomorphic-git passes to `readFile`. */
type ReadFileOptions = { encoding?: string } | string | undefined;

/** The `fs` object isomorphic-git is handed. */
export type GitFs = { promises: GitFsPromises };

export type GitFsPromises = {
  readFile(path: string, options?: ReadFileOptions): Promise<Uint8Array | string>;
  writeFile(path: string, data: Uint8Array | string, options?: ReadFileOptions): Promise<void>;
  unlink(path: string): Promise<void>;
  readdir(path: string): Promise<string[]>;
  mkdir(path: string): Promise<void>;
  rmdir(path: string): Promise<void>;
  stat(path: string): Promise<FsStat>;
  lstat(path: string): Promise<FsStat>;
  readlink(path: string): Promise<string>;
  symlink(): Promise<void>;
};

/** Splits a path into its segments, tolerating leading slashes and `.`. */
function segmentsOf(path: string): string[] {
  return path
    .split('/')
    .filter((part) => part !== '' && part !== '.')
    .filter((part) => part !== '..' || raiseTraversal(path));
}

/** A `..` would let git escape the mounted folder, so it is refused. */
function raiseTraversal(path: string): never {
  throw new FsError('EINVAL', path, `path escapes the mounted folder: ${path}`);
}

/** Builds a stat record; the numbers isomorphic-git ignores are stable zeros. */
function makeStat(type: 'file' | 'dir', size: number, mtimeMs: number): FsStat {
  return {
    type,
    // 0o040000 marks a directory and 0o100644 a regular file, which is what
    // isomorphic-git writes into the index.
    mode: type === 'dir' ? 0o040000 : 0o100644,
    size,
    ino: 0,
    mtimeMs,
    ctimeMs: mtimeMs,
    uid: 0,
    gid: 0,
    dev: 0,
    isFile: () => type === 'file',
    isDirectory: () => type === 'dir',
    isSymbolicLink: () => false,
  };
}

/**
 * Builds the `fs` object for one mounted folder.
 *
 * Every operation resolves from the root handle each time. Handles are cheap to
 * re-resolve and caching them across a `git checkout` that deletes directories
 * is how stale-handle bugs happen.
 */
export function createGitFs(root: DirectoryHandleLike): GitFs {
  async function directoryOf(
    segments: string[],
    options: { create?: boolean } = {},
  ): Promise<DirectoryHandleLike> {
    let handle = root;
    for (const segment of segments) {
      try {
        handle = await handle.getDirectoryHandle(segment, { create: options.create === true });
      } catch (error) {
        throw asFsError(error, segments.join('/'));
      }
    }
    return handle;
  }

  async function fileOf(path: string, options: { create?: boolean } = {}): Promise<FileHandleLike> {
    const segments = segmentsOf(path);
    const name = segments.pop();
    if (name === undefined) throw new FsError('EISDIR', path);
    const parent = await directoryOf(segments, { create: options.create === true });
    try {
      return await parent.getFileHandle(name, { create: options.create === true });
    } catch (error) {
      throw asFsError(error, path);
    }
  }

  const promises: GitFsPromises = {
    async readFile(path, options) {
      const handle = await fileOf(path);
      const file = await handle.getFile();
      const bytes = new Uint8Array(await file.arrayBuffer());
      return encodingOf(options) === 'utf8' ? new TextDecoder().decode(bytes) : bytes;
    },

    async writeFile(path, data) {
      const handle = await fileOf(path, { create: true });
      if (typeof handle.createWritable !== 'function') {
        throw new FsError('EACCES', path, 'this folder was mounted read-only');
      }
      const writable = await handle.createWritable();
      await writable.write(typeof data === 'string' ? data : bufferOf(data));
      await writable.close();
    },

    async unlink(path) {
      const segments = segmentsOf(path);
      const name = segments.pop();
      if (name === undefined) throw new FsError('EISDIR', path);
      const parent = await directoryOf(segments);
      try {
        await parent.removeEntry(name);
      } catch (error) {
        throw asFsError(error, path);
      }
    },

    async readdir(path) {
      const dir = await directoryOf(segmentsOf(path));
      const names: string[] = [];
      for await (const [name] of dir.entries()) names.push(name);
      return names.sort();
    },

    async mkdir(path) {
      await directoryOf(segmentsOf(path), { create: true });
    },

    async rmdir(path) {
      const segments = segmentsOf(path);
      const name = segments.pop();
      if (name === undefined) return;
      const parent = await directoryOf(segments);
      try {
        await parent.removeEntry(name, { recursive: true });
      } catch (error) {
        throw asFsError(error, path);
      }
    },

    async stat(path) {
      const segments = segmentsOf(path);
      if (segments.length === 0) return makeStat('dir', 0, 0);
      const name = segments[segments.length - 1] as string;
      const parent = await directoryOf(segments.slice(0, -1));
      for await (const [entryName, handle] of parent.entries()) {
        if (entryName !== name) continue;
        if (handle.kind === 'directory') return makeStat('dir', 0, 0);
        const file = await handle.getFile();
        return makeStat('file', file.size, file.lastModified);
      }
      throw new FsError('ENOENT', path);
    },

    lstat(path) {
      return promises.stat(path);
    },

    readlink(path) {
      return Promise.reject(
        new FsError('EINVAL', path, 'symlinks are not supported in the browser'),
      );
    },

    symlink() {
      return Promise.reject(new FsError('EPERM', '', 'symlinks are not supported in the browser'));
    },
  };

  return { promises };
}

/** Reads the encoding out of the options isomorphic-git passes. */
function encodingOf(options: ReadFileOptions): string | undefined {
  if (typeof options === 'string') return options;
  return options?.encoding;
}

/** Copies a view into a standalone buffer, which `write` requires. */
function bufferOf(data: Uint8Array): ArrayBuffer {
  return data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength) as ArrayBuffer;
}

/**
 * Maps a File System Access failure onto the `code` isomorphic-git expects.
 * `NotFoundError` is by far the most important: the library uses a failed
 * `ENOENT` read as a normal control-flow answer.
 */
function asFsError(error: unknown, path: string): FsError {
  if (error instanceof FsError) return error;
  const name = error instanceof Error ? error.name : '';
  switch (name) {
    case 'NotFoundError':
      return new FsError('ENOENT', path);
    case 'TypeMismatchError':
      return new FsError('ENOTDIR', path);
    case 'NotAllowedError':
    case 'SecurityError':
      return new FsError('EACCES', path, 'permission to this folder was not granted');
    case 'InvalidModificationError':
      return new FsError('ENOTEMPTY', path);
    default:
      return new FsError('ENOENT', path, error instanceof Error ? error.message : String(error));
  }
}
