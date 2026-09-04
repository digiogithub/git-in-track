/**
 * Shared scanning rules for every `VaultFS` implementation: which entries are
 * skipped, which files are read as text, and how two scans are diffed into the
 * `FileEvent[]` that `vault.apply` consumes (docs/05-web-app.md §6.2).
 */

import type { FileEvent, VaultFile } from '@/core-bridge/api';

import type { VaultEntry } from './types';

/** Directories that are never walked, whatever their depth. */
export const IGNORED_DIRECTORIES = new Set(['.git', 'node_modules']);

/** Dot-directories are skipped except this allowlist: the backlog lives in `.pmngr`. */
export const ALLOWED_DOT_DIRECTORIES = new Set(['.pmngr']);

/** Editor droppings and OS noise, mirrored from the watcher's ignore list. */
const IGNORED_FILE_PATTERNS = [/^\.DS_Store$/, /\.swp$/, /\.tmp$/, /^\.#/, /^4913$/, /~$/];

/** Extensions read as UTF-8 text and handed to the core. */
export const TEXT_EXTENSIONS = new Set([
  'md',
  'markdown',
  'mdx',
  'yaml',
  'yml',
  'txt',
  'json',
  'toml',
  'csv',
]);

/** Files larger than this are indexed by metadata only (docs/05-web-app.md §6.3). */
export const MAX_TEXT_FILE_BYTES = 5 * 1024 * 1024;

export function isIgnoredDirectory(name: string): boolean {
  if (IGNORED_DIRECTORIES.has(name)) return true;
  if (name.startsWith('.')) return !ALLOWED_DOT_DIRECTORIES.has(name);
  return false;
}

export function isIgnoredFile(name: string): boolean {
  return IGNORED_FILE_PATTERNS.some((pattern) => pattern.test(name));
}

export function extensionOf(path: string): string {
  const name = path.slice(path.lastIndexOf('/') + 1);
  const dot = name.lastIndexOf('.');
  return dot <= 0 ? '' : name.slice(dot + 1).toLowerCase();
}

/** True for the Markdown, YAML and plain-text files the core parses. */
export function isTextPath(path: string): boolean {
  return TEXT_EXTENSIONS.has(extensionOf(path));
}

export function joinPath(parent: string, name: string): string {
  return parent ? `${parent}/${name}` : name;
}

/** Directory part of a vault path (`''` for a file at the root). */
export function dirnameOf(path: string): string {
  const slash = path.lastIndexOf('/');
  return slash === -1 ? '' : path.slice(0, slash);
}

export type VaultScan = {
  files: VaultFile[];
  entries: Map<string, VaultEntry>;
  /** True when the folder looks like a git working tree. */
  hasGit: boolean;
};

/**
 * Diffs two scans by `(size, lastModified)`.
 *
 * Renames are reported as a `remove` plus a `create`: matching them reliably
 * would need content hashing of the whole vault, and the core treats both
 * shapes identically.
 */
export function diffScans(previous: Map<string, VaultEntry>, next: VaultScan): FileEvent[] {
  const events: FileEvent[] = [];
  const texts = new Map(next.files.map((file) => [file.path, file.text]));

  for (const [path, entry] of next.entries) {
    const before = previous.get(path);
    const text = texts.get(path);
    if (text === undefined) continue;
    if (!before) {
      events.push({ op: 'create', path, text });
    } else if (before.size !== entry.size || before.lastModified !== entry.lastModified) {
      events.push({ op: 'write', path, text });
    }
  }

  for (const path of previous.keys()) {
    if (!next.entries.has(path)) events.push({ op: 'remove', path });
  }

  return events;
}
