/**
 * Vault filesystem abstraction (docs/05-web-app.md §6).
 *
 * The Go core keeps the vault in memory: the host reads files and pushes them
 * in with `vault.load` / `vault.apply`, and persists back whatever the core
 * reports as written or removed. `VaultFS` is the thin browser-side half of
 * that contract, with two implementations:
 *
 * - `FsaVault` — File System Access API handles: read and write (Chromium).
 * - `WebkitDirectoryVault` — `<input type="file" webkitdirectory>`: read only
 *   (Firefox, Safari), see docs/05-web-app.md §6.3.
 *
 * The types below are structural on purpose: `lib.dom` does not declare
 * `showDirectoryPicker`, the permission methods or the directory async
 * iterator, and tests need to substitute plain objects for real handles.
 */

import type { FileEvent, VaultFile } from '@/core-bridge/api';

export type VaultKind = 'fsa' | 'webkitdirectory';

export type FsPermissionState = 'granted' | 'denied' | 'prompt';

export type FsPermissionMode = 'read' | 'readwrite';

export type FsPermissionDescriptor = { mode?: FsPermissionMode };

export interface WritableFileStreamLike {
  write(data: string | BufferSource | Blob): Promise<void>;
  close(): Promise<void>;
}

export interface FileHandleLike {
  readonly kind: 'file';
  readonly name: string;
  getFile(): Promise<File>;
  createWritable?(options?: { keepExistingData?: boolean }): Promise<WritableFileStreamLike>;
}

export interface DirectoryHandleLike {
  readonly kind: 'directory';
  readonly name: string;
  entries(): AsyncIterableIterator<[string, DirectoryHandleLike | FileHandleLike]>;
  getDirectoryHandle(name: string, options?: { create?: boolean }): Promise<DirectoryHandleLike>;
  getFileHandle(name: string, options?: { create?: boolean }): Promise<FileHandleLike>;
  removeEntry(name: string, options?: { recursive?: boolean }): Promise<void>;
  queryPermission?(descriptor?: FsPermissionDescriptor): Promise<FsPermissionState>;
  requestPermission?(descriptor?: FsPermissionDescriptor): Promise<FsPermissionState>;
}

/** Metadata kept between scans so `rescan()` can diff cheaply. */
export type VaultEntry = { path: string; size: number; lastModified: number };

export type VaultCapabilities = {
  /** false for the `webkitdirectory` fallback: every write affordance is disabled. */
  write: boolean;
};

/** A folder mounted in the browser, read as text for the core and written back. */
export interface VaultFS {
  readonly kind: VaultKind;
  /** Display name of the mounted folder. */
  readonly name: string;
  readonly capabilities: VaultCapabilities;

  /** Walks the folder and returns every text file, ignoring `.git` and friends. */
  readTextFiles(): Promise<VaultFile[]>;
  /** The result of the last walk, or `null` when the vault was never read. */
  cachedFiles(): VaultFile[] | null;
  /** Walks again and returns the `FileEvent[]` that `vault.apply` expects. */
  rescan(): Promise<FileEvent[]>;
  /** Raw bytes of one file, for images and other KB assets. */
  readBinary(path: string): Promise<Blob>;

  writeFile(path: string, text: string): Promise<void>;
  removeFile(path: string): Promise<void>;
  rename(from: string, to: string): Promise<void>;
}

/** Thrown when a vault operation cannot be performed by this implementation. */
export class VaultError extends Error {
  readonly code: 'read_only' | 'not_found' | 'permission_denied' | 'io';
  readonly path: string | undefined;

  constructor(code: VaultError['code'], message: string, path?: string) {
    super(message);
    this.name = 'VaultError';
    this.code = code;
    this.path = path;
  }
}
