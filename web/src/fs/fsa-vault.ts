/**
 * `VaultFS` over the File System Access API (docs/05-web-app.md §6).
 *
 * The handle is obtained once with `showDirectoryPicker({ mode: 'readwrite' })`
 * and then persisted in IndexedDB (`handle-store.ts`). Permission does not
 * always survive a reload, so the provider re-checks it on boot and the
 * workspace re-requests it inside a single user gesture.
 */

import type { FileEvent, VaultFile } from '@/core-bridge/api';

import {
  diffScans,
  isIgnoredDirectory,
  isIgnoredFile,
  isTextPath,
  joinPath,
  MAX_TEXT_FILE_BYTES,
  type VaultScan,
} from './scan';
import {
  VaultError,
  type DirectoryHandleLike,
  type FileHandleLike,
  type FsPermissionMode,
  type FsPermissionState,
  type VaultCapabilities,
  type VaultEntry,
  type VaultFS,
} from './types';

export type DirectoryPickerOptions = {
  id?: string;
  mode?: FsPermissionMode;
  startIn?: string;
};

type DirectoryPicker = (options?: DirectoryPickerOptions) => Promise<DirectoryHandleLike>;

/** The picker, or `null` in browsers without the File System Access API. */
export function getDirectoryPicker(): DirectoryPicker | null {
  const host = globalThis as { showDirectoryPicker?: DirectoryPicker };
  return typeof host.showDirectoryPicker === 'function'
    ? host.showDirectoryPicker.bind(globalThis)
    : null;
}

/**
 * Current permission for a handle. Handles without the (non-standard)
 * permission methods — older Chromium, and the plain objects used in tests —
 * are treated as granted, because reading them is the only way to find out.
 */
export async function queryPermission(
  handle: DirectoryHandleLike,
  mode: FsPermissionMode = 'readwrite',
): Promise<FsPermissionState> {
  if (typeof handle.queryPermission !== 'function') return 'granted';
  return handle.queryPermission({ mode });
}

/**
 * Re-requests permission. Chromium requires transient user activation, so this
 * is only ever called from a click handler, never during boot.
 */
export async function requestPermission(
  handle: DirectoryHandleLike,
  mode: FsPermissionMode = 'readwrite',
): Promise<FsPermissionState> {
  if (typeof handle.requestPermission !== 'function') return 'granted';
  return handle.requestPermission({ mode });
}

/** Asks the user for a folder. Rejects with `VaultError` when unsupported. */
export async function pickDirectory(options: DirectoryPickerOptions = {}): Promise<FsaVault> {
  const picker = getDirectoryPicker();
  if (!picker) {
    throw new VaultError(
      'permission_denied',
      'This browser does not support the File System Access API',
    );
  }
  const handle = await picker({ id: 'gintrack-repo', mode: 'readwrite', ...options });
  return new FsaVault(handle);
}

export class FsaVault implements VaultFS {
  readonly kind = 'fsa' as const;

  readonly #handle: DirectoryHandleLike;
  #entries = new Map<string, VaultEntry>();
  #files: VaultFile[] | null = null;
  #hasGit = false;
  #writable: boolean;

  constructor(handle: DirectoryHandleLike, options: { writable?: boolean } = {}) {
    this.#handle = handle;
    this.#writable = options.writable ?? true;
  }

  get name(): string {
    return this.#handle.name;
  }

  get handle(): DirectoryHandleLike {
    return this.#handle;
  }

  get capabilities(): VaultCapabilities {
    return { write: this.#writable };
  }

  /** True when the mounted folder contains a `.git` directory. */
  get hasGit(): boolean {
    return this.#hasGit;
  }

  /** Marks the vault read-only, for a handle that only got `read` permission. */
  setWritable(writable: boolean): void {
    this.#writable = writable;
  }

  queryPermission(mode: FsPermissionMode = 'readwrite'): Promise<FsPermissionState> {
    return queryPermission(this.#handle, mode);
  }

  requestPermission(mode: FsPermissionMode = 'readwrite'): Promise<FsPermissionState> {
    return requestPermission(this.#handle, mode);
  }

  async readTextFiles(): Promise<VaultFile[]> {
    const scan = await this.#scan();
    this.#entries = scan.entries;
    this.#files = scan.files;
    this.#hasGit = scan.hasGit;
    return scan.files;
  }

  cachedFiles(): VaultFile[] | null {
    return this.#files;
  }

  async rescan(): Promise<FileEvent[]> {
    const scan = await this.#scan();
    const events = diffScans(this.#entries, scan);
    this.#entries = scan.entries;
    this.#files = scan.files;
    this.#hasGit = scan.hasGit;
    return events;
  }

  async readBinary(path: string): Promise<Blob> {
    const handle = await this.#fileHandle(path, false);
    return handle.getFile();
  }

  async writeFile(path: string, text: string): Promise<void> {
    this.#assertWritable(path);
    const handle = await this.#fileHandle(path, true);
    if (typeof handle.createWritable !== 'function') {
      throw new VaultError('read_only', `Cannot write ${path}: writable streams unavailable`, path);
    }
    const writable = await handle.createWritable();
    await writable.write(text);
    await writable.close();

    const file = await handle.getFile();
    this.#entries.set(path, { path, size: file.size, lastModified: file.lastModified });
    this.#replaceCachedFile(path, text);
  }

  async removeFile(path: string): Promise<void> {
    this.#assertWritable(path);
    const { directory, name } = await this.#resolve(path, false);
    await directory.removeEntry(name);
    this.#entries.delete(path);
    this.#files = (this.#files ?? []).filter((file) => file.path !== path);
  }

  /** Write-then-remove: `move()` is not available on every Chromium version. */
  async rename(from: string, to: string): Promise<void> {
    this.#assertWritable(from);
    const handle = await this.#fileHandle(from, false);
    const text = await (await handle.getFile()).text();
    await this.writeFile(to, text);
    await this.removeFile(from);
  }

  #assertWritable(path: string): void {
    if (!this.#writable) {
      throw new VaultError('read_only', `This folder is mounted read-only: ${path}`, path);
    }
  }

  #replaceCachedFile(path: string, text: string): void {
    if (!this.#files) return;
    const index = this.#files.findIndex((file) => file.path === path);
    if (index === -1) this.#files.push({ path, text });
    else this.#files[index] = { path, text };
  }

  async #scan(): Promise<VaultScan> {
    const scan: VaultScan = { files: [], entries: new Map(), hasGit: false };
    await this.#walk(this.#handle, '', scan, 0);
    scan.files.sort((a, b) => a.path.localeCompare(b.path));
    return scan;
  }

  async #walk(
    directory: DirectoryHandleLike,
    prefix: string,
    scan: VaultScan,
    depth: number,
  ): Promise<void> {
    // Guards against a pathological tree; real repositories are far shallower.
    if (depth > 32) return;

    for await (const [name, entry] of directory.entries()) {
      if (entry.kind === 'directory') {
        if (name === '.git' && prefix === '') scan.hasGit = true;
        if (isIgnoredDirectory(name)) continue;
        await this.#walk(entry, joinPath(prefix, name), scan, depth + 1);
        continue;
      }
      if (isIgnoredFile(name)) continue;

      const path = joinPath(prefix, name);
      const file = await entry.getFile();
      scan.entries.set(path, { path, size: file.size, lastModified: file.lastModified });
      if (!isTextPath(path) || file.size > MAX_TEXT_FILE_BYTES) continue;
      scan.files.push({ path, text: await file.text() });
    }
  }

  async #resolve(
    path: string,
    create: boolean,
  ): Promise<{ directory: DirectoryHandleLike; name: string }> {
    const parts = path.split('/').filter(Boolean);
    const name = parts.pop();
    if (!name) throw new VaultError('not_found', `Invalid vault path: ${path}`, path);

    let directory = this.#handle;
    for (const part of parts) {
      directory = await this.#step(directory, part, create, path);
    }
    return { directory, name };
  }

  async #step(
    directory: DirectoryHandleLike,
    name: string,
    create: boolean,
    path: string,
  ): Promise<DirectoryHandleLike> {
    try {
      return await directory.getDirectoryHandle(name, { create });
    } catch (error) {
      throw toVaultError(error, path);
    }
  }

  async #fileHandle(path: string, create: boolean): Promise<FileHandleLike> {
    const { directory, name } = await this.#resolve(path, create);
    try {
      return await directory.getFileHandle(name, { create });
    } catch (error) {
      throw toVaultError(error, path);
    }
  }
}

/** Maps a DOM exception raised by the FS API onto a typed `VaultError`. */
export function toVaultError(error: unknown, path?: string): VaultError {
  if (error instanceof VaultError) return error;
  const name = error instanceof Error ? error.name : '';
  const message = error instanceof Error ? error.message : String(error);
  if (name === 'NotFoundError') return new VaultError('not_found', message, path);
  if (name === 'NotAllowedError' || name === 'SecurityError') {
    return new VaultError('permission_denied', message, path);
  }
  return new VaultError('io', message, path);
}
