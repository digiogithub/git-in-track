/**
 * Browser-mode storage of the commit-on-save settings (docs/06-git-sync.md
 * §3.3: "the browser stores the same keys per workspace").
 *
 * These are settings, not secrets: a template and two booleans. No credential
 * ever reaches this module — token handling is GIT-US-0023 and lives in the git
 * worker's memory, never in `localStorage` (docs/06 §8.2).
 *
 * `localStorage` can be absent or throw (private mode, blocked site data), so
 * every access is guarded and a failure falls back to the defaults rather than
 * breaking the settings page.
 */

import type { GitSettings, GitSettingsPatch } from '@/api/provider';
import { DEFAULT_COMMIT_TEMPLATE, validateCommitTemplate } from '@/git/message';

/** Key prefix; one entry per workspace. */
const STORAGE_PREFIX = 'gintrack.git.settings';

/** The window rapid saves of one item are coalesced over. */
export const DEFAULT_COMMIT_DEBOUNCE_MS = 2000;

/**
 * Why browser-only mode cannot commit yet. Git in the browser is
 * isomorphic-git, which arrives with GIT-US-0021; until then the settings are
 * stored and rendered but nothing is committed, and the UI says so.
 */
export const BROWSER_GIT_REASON =
  'Browser-only mode cannot commit yet: git in the browser is isomorphic-git, which arrives with the sync story (GIT-US-0021). Run the companion for commit-on-save today.';

/** The settings of a workspace that has never been configured. */
export function defaultGitSettings(): GitSettings {
  return {
    commitOnSave: false,
    commitDebounceMs: DEFAULT_COMMIT_DEBOUNCE_MS,
    messageTemplate: DEFAULT_COMMIT_TEMPLATE,
    backend: 'isomorphic-git',
    resolvedBackend: 'isomorphic-git',
    signCommits: false,
    pending: 0,
    supported: false,
    reason: BROWSER_GIT_REASON,
  };
}

/** Reads the stored settings of a workspace, falling back to the defaults. */
export function readGitSettings(workspace = 'default'): GitSettings {
  const base = defaultGitSettings();
  const raw = safeRead(storageKey(workspace));
  if (raw === null) return base;
  try {
    const stored = JSON.parse(raw) as Partial<GitSettings>;
    return {
      ...base,
      commitOnSave: stored.commitOnSave === true,
      commitDebounceMs:
        typeof stored.commitDebounceMs === 'number' && stored.commitDebounceMs >= 0
          ? stored.commitDebounceMs
          : base.commitDebounceMs,
      messageTemplate:
        typeof stored.messageTemplate === 'string' && stored.messageTemplate !== ''
          ? stored.messageTemplate
          : base.messageTemplate,
      ...(typeof stored.authorName === 'string' ? { authorName: stored.authorName } : {}),
      ...(typeof stored.authorEmail === 'string' ? { authorEmail: stored.authorEmail } : {}),
    };
  } catch {
    // A corrupt entry is a settings entry, not data: the defaults are correct.
    return base;
  }
}

/**
 * Applies a patch and stores the result. An invalid template is refused before
 * anything is written, so a broken template can never reach a commit.
 */
export function writeGitSettings(patch: GitSettingsPatch, workspace = 'default'): GitSettings {
  const current = readGitSettings(workspace);
  const next: GitSettings = {
    ...current,
    ...(patch.commitOnSave === undefined ? {} : { commitOnSave: patch.commitOnSave }),
    ...(patch.commitDebounceMs === undefined ? {} : { commitDebounceMs: patch.commitDebounceMs }),
    ...(patch.messageTemplate === undefined ? {} : { messageTemplate: patch.messageTemplate }),
    ...(patch.authorName === undefined ? {} : { authorName: patch.authorName }),
    ...(patch.authorEmail === undefined ? {} : { authorEmail: patch.authorEmail }),
    ...(patch.signCommits === undefined ? {} : { signCommits: patch.signCommits }),
  };
  if (next.commitDebounceMs < 0) {
    throw new RangeError('commitDebounceMs must not be negative');
  }
  validateCommitTemplate(next.messageTemplate);

  const persisted = safeWrite(
    storageKey(workspace),
    JSON.stringify({
      commitOnSave: next.commitOnSave,
      commitDebounceMs: next.commitDebounceMs,
      messageTemplate: next.messageTemplate,
      ...(next.authorName === undefined ? {} : { authorName: next.authorName }),
      ...(next.authorEmail === undefined ? {} : { authorEmail: next.authorEmail }),
    }),
  );
  return { ...next, persisted };
}

/** Forgets the stored settings of a workspace. */
export function clearGitSettings(workspace = 'default'): void {
  try {
    globalThis.localStorage?.removeItem(storageKey(workspace));
  } catch {
    // Nothing to clear when storage is unavailable.
  }
}

function storageKey(workspace: string): string {
  return `${STORAGE_PREFIX}.${workspace}`;
}

function safeRead(key: string): string | null {
  try {
    return globalThis.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function safeWrite(key: string, value: string): boolean {
  try {
    globalThis.localStorage?.setItem(key, value);
    return true;
  } catch {
    return false;
  }
}
