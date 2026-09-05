/**
 * Docs-folder detection for the add-repository wizard (docs/05-web-app.md
 * §3.1, step 3).
 *
 * A folder is a git-in-track project when it contains `.pmngr/project.yaml`.
 * The docs folder is the parent of that `.pmngr/` directory, which is `docs/`
 * by convention but may be any path, including the repository root
 * (docs/03-data-model.md §2).
 *
 * Detection is deliberately deeper than discovery. Discovery — what the core
 * does on every mount and every reload — probes the repository root and its
 * first-level directories only, so a working tree carrying test fixtures does
 * not report them as projects (ADR-018). Detection looks further, up to
 * `DETECT_DEPTH`, and marks each candidate `declarationNeeded` when discovery
 * cannot reach it on its own: the wizard then shows it as a folder the user
 * must choose deliberately, and mounting it records the declaration.
 */

import type { VaultFile } from '@/core-bridge/api';

import { dirnameOf } from './scan';

export const PROJECT_FILE = '.pmngr/project.yaml';

/**
 * How deep the wizard looks for a `project.yaml`. It mirrors `detectDepth` in
 * internal/config/repo.go: deep enough for `packages/<x>/docs`, shallow enough
 * to stay instant on a large working tree.
 */
export const DETECT_DEPTH = 4;

/**
 * How deep discovery reaches on its own: the repository root and its
 * first-level directories (`core.DiscoveryDepth`, ADR-018).
 */
export const DISCOVERY_DEPTH = 1;

/** Depth of a vault-relative folder; the repository root is 0. */
function depthOf(folder: string): number {
  return folder === '' ? 0 : folder.split('/').length;
}

export type DocsFolderCandidate = {
  /** Vault-relative docs folder; `''` means the repository root. */
  docsFolder: string;
  /** Path of the `project.yaml` that was found. */
  projectFile: string;
  /** `key:` read from `project.yaml`, when the line is a plain scalar. */
  projectKey?: string;
  /** `name:` read from `project.yaml`. */
  projectName?: string;
  /**
   * True when discovery cannot reach this folder on its own, so mounting it
   * has to declare it (ADR-018). The wizard says so rather than silently
   * indexing a fixture.
   */
  declarationNeeded: boolean;
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
    if (depthOf(docsFolder) > DETECT_DEPTH) continue;
    const key = scalar(file.text, 'key');
    const name = scalar(file.text, 'name');
    candidates.push({
      docsFolder,
      projectFile: file.path,
      ...(key ? { projectKey: key } : {}),
      ...(name ? { projectName: name } : {}),
      declarationNeeded: depthOf(docsFolder) > DISCOVERY_DEPTH,
    });
  }

  // The same preference order the CLI detector uses: `docs` first, then the
  // shallowest, then alphabetically (internal/config/repo.go DocsCandidates).
  return candidates.sort((a, b) => {
    if ((a.docsFolder === 'docs') !== (b.docsFolder === 'docs')) {
      return a.docsFolder === 'docs' ? -1 : 1;
    }
    const depth = depthOf(a.docsFolder) - depthOf(b.docsFolder);
    if (depth !== 0) return depth;
    return a.docsFolder.localeCompare(b.docsFolder);
  });
}

/** Normalises a docs folder typed by hand: no leading, trailing or `./` parts. */
export function normalizeDocsFolder(input: string): string {
  return input
    .trim()
    .replace(/\\/g, '/')
    .replace(/^\.\//, '')
    .replace(/^\/+|\/+$/g, '');
}
