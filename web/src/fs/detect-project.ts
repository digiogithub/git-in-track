/**
 * Docs-folder detection for the add-repository wizard (docs/05-web-app.md
 * §3.1, step 3).
 *
 * A folder is a git-in-track project when it contains `.pmngr/project.yaml`.
 * The docs folder is the parent of that `.pmngr/` directory, which is `docs/`
 * by convention but may be any path, including the repository root
 * (docs/03-data-model.md §2).
 */

import type { VaultFile } from '@/core-bridge/api';

import { dirnameOf } from './scan';

export const PROJECT_FILE = '.pmngr/project.yaml';

export type DocsFolderCandidate = {
  /** Vault-relative docs folder; `''` means the repository root. */
  docsFolder: string;
  /** Path of the `project.yaml` that was found. */
  projectFile: string;
  /** `key:` read from `project.yaml`, when the line is a plain scalar. */
  projectKey?: string;
  /** `name:` read from `project.yaml`. */
  projectName?: string;
};

function scalar(text: string, field: string): string | undefined {
  const match = new RegExp(`^${field}:\\s*["']?([^"'#\\n]+?)["']?\\s*$`, 'm').exec(text);
  return match?.[1]?.trim();
}

/**
 * Finds every `.pmngr/project.yaml` in the scanned files. A monorepo can hold
 * several projects, so the wizard shows all candidates and lets the user pick.
 */
export function detectDocsFolders(files: VaultFile[]): DocsFolderCandidate[] {
  const candidates: DocsFolderCandidate[] = [];

  for (const file of files) {
    if (file.path !== PROJECT_FILE && !file.path.endsWith(`/${PROJECT_FILE}`)) continue;
    const docsFolder = dirnameOf(dirnameOf(file.path));
    const key = scalar(file.text, 'key');
    const name = scalar(file.text, 'name');
    candidates.push({
      docsFolder,
      projectFile: file.path,
      ...(key ? { projectKey: key } : {}),
      ...(name ? { projectName: name } : {}),
    });
  }

  return candidates.sort((a, b) => a.docsFolder.localeCompare(b.docsFolder));
}

/** Normalises a docs folder typed by hand: no leading, trailing or `./` parts. */
export function normalizeDocsFolder(input: string): string {
  return input
    .trim()
    .replace(/\\/g, '/')
    .replace(/^\.\//, '')
    .replace(/^\/+|\/+$/g, '');
}
