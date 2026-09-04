/**
 * In-memory `VaultFS`, used by provider tests and by any preview that needs a
 * vault without touching the disk. It mirrors the semantics that matter:
 * text-only reads, write/remove bookkeeping and rescan diffing.
 */

import type { FileEvent, VaultFile } from '@/core-bridge/api';

import { diffScans, isTextPath, type VaultScan } from './scan';
import { VaultError, type VaultCapabilities, type VaultEntry, type VaultFS } from './types';

export class MemoryVault implements VaultFS {
  readonly kind = 'fsa' as const;
  readonly name: string;

  #files: Map<string, string>;
  #binary = new Map<string, Blob>();
  #stamps = new Map<string, number>();
  #entries = new Map<string, VaultEntry>();
  #cache: VaultFile[] | null = null;
  #writable: boolean;
  #clock = 1;

  constructor(
    files: Record<string, string> = {},
    options: { name?: string; writable?: boolean } = {},
  ) {
    this.name = options.name ?? 'memory-vault';
    this.#writable = options.writable ?? true;
    this.#files = new Map(Object.entries(files));
    for (const path of this.#files.keys()) this.#stamps.set(path, this.#clock);
  }

  get capabilities(): VaultCapabilities {
    return { write: this.#writable };
  }

  /** Snapshot of the current contents, for assertions. */
  snapshot(): Record<string, string> {
    return Object.fromEntries(this.#files);
  }

  /** Simulates an external edit (a git pull, another editor). */
  setExternal(path: string, text: string): void {
    this.#clock += 1;
    this.#files.set(path, text);
    this.#stamps.set(path, this.#clock);
  }

  removeExternal(path: string): void {
    this.#files.delete(path);
    this.#stamps.delete(path);
  }

  putBinary(path: string, blob: Blob): void {
    this.#binary.set(path, blob);
  }

  readTextFiles(): Promise<VaultFile[]> {
    const scan = this.#scan();
    this.#entries = scan.entries;
    this.#cache = scan.files;
    return Promise.resolve(scan.files);
  }

  cachedFiles(): VaultFile[] | null {
    return this.#cache;
  }

  rescan(): Promise<FileEvent[]> {
    const scan = this.#scan();
    const events = diffScans(this.#entries, scan);
    this.#entries = scan.entries;
    this.#cache = scan.files;
    return Promise.resolve(events);
  }

  readBinary(path: string): Promise<Blob> {
    const blob = this.#binary.get(path);
    if (blob) return Promise.resolve(blob);
    const text = this.#files.get(path);
    if (text === undefined) {
      return Promise.reject(new VaultError('not_found', `File ${path} is not in the vault`, path));
    }
    return Promise.resolve(new Blob([text]));
  }

  writeFile(path: string, text: string): Promise<void> {
    if (!this.#writable) {
      return Promise.reject(new VaultError('read_only', `Cannot write ${path}`, path));
    }
    this.#clock += 1;
    this.#files.set(path, text);
    this.#stamps.set(path, this.#clock);
    this.#entries.set(path, { path, size: text.length, lastModified: this.#clock });
    return Promise.resolve();
  }

  removeFile(path: string): Promise<void> {
    if (!this.#writable) {
      return Promise.reject(new VaultError('read_only', `Cannot remove ${path}`, path));
    }
    this.#files.delete(path);
    this.#stamps.delete(path);
    this.#entries.delete(path);
    return Promise.resolve();
  }

  async rename(from: string, to: string): Promise<void> {
    const text = this.#files.get(from);
    if (text === undefined) {
      throw new VaultError('not_found', `File ${from} is not in the vault`, from);
    }
    await this.writeFile(to, text);
    await this.removeFile(from);
  }

  #scan(): VaultScan {
    const scan: VaultScan = { files: [], entries: new Map(), hasGit: false };
    for (const [path, text] of this.#files) {
      scan.entries.set(path, {
        path,
        size: text.length,
        lastModified: this.#stamps.get(path) ?? 0,
      });
      if (isTextPath(path)) scan.files.push({ path, text });
    }
    scan.files.sort((a, b) => a.path.localeCompare(b.path));
    return scan;
  }
}
