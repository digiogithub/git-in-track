/**
 * Read-only `VaultFS` over `<input type="file" webkitdirectory>`
 * (docs/05-web-app.md §6.3, story GIT-US-0011).
 *
 * Firefox and Safari have no File System Access API. The user picks a folder
 * with a plain file input, the browser hands us a `FileList` whose entries
 * carry a `webkitRelativePath`, and we keep them in memory. The same WASM core
 * indexes them; every write affordance is disabled because there is no way to
 * write the files back.
 */

import type { FileEvent, VaultFile } from '@/core-bridge/api';

import {
  isIgnoredDirectory,
  isIgnoredFile,
  isTextPath,
  MAX_TEXT_FILE_BYTES,
  type VaultScan,
} from './scan';
import { VaultError, type VaultCapabilities, type VaultFS } from './types';

type RelativeFile = File & { webkitRelativePath?: string };

/** Strips the leading folder name the browser prepends to every entry. */
function relativePath(file: RelativeFile, rootName: string): string {
  const raw = file.webkitRelativePath ?? file.name;
  const prefix = `${rootName}/`;
  return raw.startsWith(prefix) ? raw.slice(prefix.length) : raw;
}

function rootNameOf(files: RelativeFile[]): string {
  for (const file of files) {
    const raw = file.webkitRelativePath ?? '';
    const slash = raw.indexOf('/');
    if (slash > 0) return raw.slice(0, slash);
  }
  return 'folder';
}

function isIgnoredPath(path: string): boolean {
  const parts = path.split('/');
  const name = parts.pop() ?? '';
  if (parts.some(isIgnoredDirectory)) return true;
  return isIgnoredFile(name);
}

export class WebkitDirectoryVault implements VaultFS {
  readonly kind = 'webkitdirectory' as const;
  readonly capabilities: VaultCapabilities = { write: false };

  readonly #name: string;
  readonly #files = new Map<string, File>();
  #texts: VaultFile[] | null = null;
  #hasGit = false;

  constructor(files: Iterable<File>, options: { name?: string } = {}) {
    const list = [...files] as RelativeFile[];
    this.#name = options.name ?? rootNameOf(list);
    for (const file of list) {
      const path = relativePath(file, this.#name);
      if (path.split('/').includes('.git')) this.#hasGit = true;
      if (isIgnoredPath(path)) continue;
      this.#files.set(path, file);
    }
  }

  /** Builds a vault from the `files` of a `webkitdirectory` input. */
  static fromInput(input: HTMLInputElement): WebkitDirectoryVault {
    return new WebkitDirectoryVault(input.files ? Array.from(input.files) : []);
  }

  get name(): string {
    return this.#name;
  }

  get hasGit(): boolean {
    return this.#hasGit;
  }

  async readTextFiles(): Promise<VaultFile[]> {
    const scan = await this.#scan();
    this.#texts = scan.files;
    return scan.files;
  }

  cachedFiles(): VaultFile[] | null {
    return this.#texts;
  }

  /**
   * The in-memory snapshot cannot change without the user picking the folder
   * again, so a rescan never produces events.
   */
  async rescan(): Promise<FileEvent[]> {
    if (!this.#texts) await this.readTextFiles();
    return [];
  }

  readBinary(path: string): Promise<Blob> {
    const file = this.#files.get(path);
    if (!file) {
      return Promise.reject(new VaultError('not_found', `File ${path} is not in the vault`, path));
    }
    return Promise.resolve(file);
  }

  writeFile(path: string, _text: string): Promise<void> {
    return Promise.reject(this.#readOnly(path));
  }

  removeFile(path: string): Promise<void> {
    return Promise.reject(this.#readOnly(path));
  }

  rename(from: string, _to: string): Promise<void> {
    return Promise.reject(this.#readOnly(from));
  }

  #readOnly(path: string): VaultError {
    return new VaultError(
      'read_only',
      'This folder was opened read-only. Use a Chromium browser or install the companion to make changes.',
      path,
    );
  }

  async #scan(): Promise<VaultScan> {
    const scan: VaultScan = { files: [], entries: new Map(), hasGit: this.#hasGit };
    for (const [path, file] of this.#files) {
      scan.entries.set(path, { path, size: file.size, lastModified: file.lastModified });
      if (!isTextPath(path) || file.size > MAX_TEXT_FILE_BYTES) continue;
      scan.files.push({ path, text: await file.text() });
    }
    scan.files.sort((a, b) => a.path.localeCompare(b.path));
    return scan;
  }
}
