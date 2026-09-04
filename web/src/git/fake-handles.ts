/**
 * In-memory stand-ins for File System Access handles, used by the browser-git
 * tests. They hold bytes rather than text, because git writes packfiles and
 * loose objects, not strings.
 *
 * This file is test support that lives outside `*.test.ts` so both the fs
 * adapter and the sync tests can share it.
 */

import type {
  DirectoryHandleLike,
  FileHandleLike,
  FsPermissionState,
  WritableFileStreamLike,
} from '@/fs/types';

/** A `NotFoundError`, which is what the real API rejects a missing entry with. */
function notFound(name: string): Error {
  const error = new Error(`${name} was not found`);
  error.name = 'NotFoundError';
  return error;
}

/** An in-memory file handle. */
export class FakeFile implements FileHandleLike {
  readonly kind = 'file' as const;
  bytes: Uint8Array;
  lastModified = 1;

  constructor(
    readonly name: string,
    bytes: Uint8Array = new Uint8Array(),
  ) {
    this.bytes = bytes;
  }

  getFile(): Promise<File> {
    const copy = this.bytes.slice();
    return Promise.resolve(
      new File([copy as unknown as BlobPart], this.name, { lastModified: this.lastModified }),
    );
  }

  createWritable(): Promise<WritableFileStreamLike> {
    const chunks: Uint8Array[] = [];
    return Promise.resolve({
      write: async (data: string | BufferSource | Blob) => {
        chunks.push(await toBytes(data));
      },
      close: () => {
        this.bytes = concat(chunks);
        this.lastModified += 1;
        return Promise.resolve();
      },
    });
  }
}

/** An in-memory directory handle. */
export class FakeDirectory implements DirectoryHandleLike {
  readonly kind = 'directory' as const;
  readonly children = new Map<string, FakeDirectory | FakeFile>();

  constructor(
    readonly name = 'repo',
    public permission: FsPermissionState = 'granted',
  ) {}

  async *entries(): AsyncIterableIterator<[string, DirectoryHandleLike | FileHandleLike]> {
    await Promise.resolve();
    for (const entry of [...this.children.entries()]) yield entry;
  }

  getDirectoryHandle(name: string, options?: { create?: boolean }): Promise<DirectoryHandleLike> {
    const existing = this.children.get(name);
    if (existing instanceof FakeDirectory) return Promise.resolve(existing);
    if (existing) return Promise.reject(typeMismatch(name));
    if (!options?.create) return Promise.reject(notFound(name));
    const created = new FakeDirectory(name);
    this.children.set(name, created);
    return Promise.resolve(created);
  }

  getFileHandle(name: string, options?: { create?: boolean }): Promise<FileHandleLike> {
    const existing = this.children.get(name);
    if (existing instanceof FakeFile) return Promise.resolve(existing);
    if (existing) return Promise.reject(typeMismatch(name));
    if (!options?.create) return Promise.reject(notFound(name));
    const created = new FakeFile(name);
    this.children.set(name, created);
    return Promise.resolve(created);
  }

  removeEntry(name: string): Promise<void> {
    if (!this.children.has(name)) return Promise.reject(notFound(name));
    this.children.delete(name);
    return Promise.resolve();
  }

  queryPermission(): Promise<FsPermissionState> {
    return Promise.resolve(this.permission);
  }

  requestPermission(): Promise<FsPermissionState> {
    return Promise.resolve(this.permission);
  }
}

/** A `TypeMismatchError`, raised when a file is asked for as a directory. */
function typeMismatch(name: string): Error {
  const error = new Error(`${name} is not of the expected kind`);
  error.name = 'TypeMismatchError';
  return error;
}

/** Normalises everything `write()` accepts into bytes. */
async function toBytes(data: string | BufferSource | Blob): Promise<Uint8Array> {
  if (typeof data === 'string') return new TextEncoder().encode(data);
  if (data instanceof Blob) return new Uint8Array(await data.arrayBuffer());
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength));
  }
  return new Uint8Array(data);
}

/** Joins written chunks into one buffer. */
function concat(chunks: Uint8Array[]): Uint8Array {
  const size = chunks.reduce((total, chunk) => total + chunk.length, 0);
  const out = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}
