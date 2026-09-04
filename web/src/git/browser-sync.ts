/**
 * Browser-only git: sync over `isomorphic-git` and the File System Access
 * handles (docs/06-git-sync.md §6, story GIT-US-0021).
 *
 * Three things this module refuses to pretend about, because §6.2 is explicit
 * that they are surfaced honestly rather than hidden:
 *
 * - **No rebase.** isomorphic-git has none, so browser mode always integrates
 *   with a merge and the setting is forced, not silently reinterpreted.
 * - **A CORS proxy is required.** Git HTTP endpoints send no
 *   `Access-Control-Allow-Origin`, so without a configured proxy the network
 *   half of a sync cannot run at all. It is reported as unsupported with the
 *   reason, never attempted and left to fail obscurely (§6.3).
 * - **No SSH.** `git@host:org/repo.git` cannot be spoken from a tab.
 *
 * No credential is stored here or anywhere else in browser mode: a token, when
 * there is one, is supplied per call through `onAuth` and lives only in the
 * caller's closure (§8.2, and GIT-US-0023 for the storage choices).
 */

import git, {
  type AuthCallback,
  type AuthFailureCallback,
  type ReadCommitResult,
} from 'isomorphic-git';
import http from 'isomorphic-git/http/web';

import type { SyncCommit, SyncConflict, SyncOptions, SyncResult, SyncStatus } from '@/api/provider';
import type { DirectoryHandleLike } from '@/fs/types';

import { createGitFs, type GitFs } from './fsa-fs';

/** The working tree root every call operates from. */
const ROOT = '/';

/** How far back ahead/behind counting walks; a backlog never needs more. */
const WALK_DEPTH = 500;

/** How many commits a preview lists. */
const PREVIEW_LIMIT = 50;

/** Why browser-only mode cannot reach a git host without a proxy (§6.3). */
export const CORS_PROXY_REASON =
  'Git hosts do not send CORS headers, so a browser tab cannot fetch or push without a proxy. ' +
  'Set a CORS proxy in Settings → Sync (the companion serves one at ' +
  'http://127.0.0.1:7317/cors-proxy/ while it runs, or self-host @isomorphic-git/cors-proxy), ' +
  'or run the companion and let it do the networking.';

/** Why an SSH remote cannot be used from a tab (§6.2). */
export const SSH_REMOTE_REASON =
  'This repository’s remote uses SSH, which a browser tab cannot speak. ' +
  'Add an HTTPS remote URL for it, or run the companion.';

/**
 * One conflicted path with the three versions the merge driver saw. Browser
 * mode keeps them in memory for the resolver: `isomorphic-git` aborts a
 * conflicting merge instead of leaving markers on disk, so the working tree is
 * untouched and the three blobs are the only record of the conflict
 * (docs/06 §5, §6.2).
 */
export type BrowserConflict = {
  path: string;
  kind: string;
  base: string;
  ours: string;
  theirs: string;
};

/** What a browser sync needs beyond the folder itself. */
export type BrowserGitOptions = {
  /** The configured CORS proxy; empty means git over the network is off. */
  corsProxy?: string;
  /**
   * Supplies a token per call, prompting for one when a host actually asks
   * (GIT-US-0023). Nothing it returns is ever persisted: the credential lives
   * in the caller's closure for the tab's lifetime and nowhere else.
   */
  onAuth?: AuthCallback;
  /**
   * Called when the host refused what `onAuth` supplied, so the caller can drop
   * the rejected credential instead of replaying it.
   */
  onAuthFailure?: AuthFailureCallback;
  /** Author of a merge commit; falls back to the repository's git config. */
  author?: { name: string; email: string };
  /**
   * Resolutions to apply during the merge, keyed by path. A path that has one
   * merges cleanly with exactly that text, which is how a resolved conflict
   * completes the merge it belongs to.
   */
  resolutions?: Record<string, string>;
  /**
   * Called with the conflicted paths and their three versions when the merge
   * stopped. It is how the provider remembers a conflict the resolver then
   * works on; nothing is written to disk.
   */
  onConflict?: (conflicts: BrowserConflict[]) => void;
};

/** A failure with the same machine codes the companion reports. */
export class BrowserGitError extends Error {
  readonly code: string;
  readonly conflicts: SyncConflict[];

  constructor(code: string, message: string, conflicts: SyncConflict[] = []) {
    super(message);
    this.name = 'BrowserGitError';
    this.code = code;
    this.conflicts = conflicts;
  }
}

/** Reads one repository's sync state. */
export async function readSyncStatus(root: DirectoryHandleLike): Promise<SyncStatus> {
  const fs = createGitFs(root);
  const status: SyncStatus = {
    branch: '',
    detached: false,
    clean: true,
    trackedChanges: false,
    ahead: 0,
    behind: 0,
    state: 'up_to_date',
  };

  const branch = await git.currentBranch({ fs, dir: ROOT, fullname: false });
  if (!branch) {
    status.branch = 'HEAD';
    status.detached = true;
    return resolveState(status);
  }
  status.branch = branch;

  const dirty = await readDirty(fs);
  status.dirty = dirty.paths;
  status.trackedChanges = dirty.tracked;
  status.conflicted = dirty.conflicted;

  const remote = await readRemote(fs);
  if (remote) {
    status.remote = remote.remote;
    status.remoteUrl = redactUrl(remote.url);
    const upstream = `${remote.remote}/${branch}`;
    if (await refExists(fs, `refs/remotes/${upstream}`)) {
      status.upstream = upstream;
      const counters = await countAheadBehind(fs, branch, `refs/remotes/${upstream}`);
      status.ahead = counters.ahead;
      status.behind = counters.behind;
    }
  }
  if (await pathExists(fs, '.git/MERGE_HEAD')) status.operation = 'merge';
  return resolveState(status);
}

/**
 * Fetch, merge and push one repository.
 *
 * It resolves with a filled report in every case, failure included: the
 * pipeline is non-destructive at every step, so a caller renders `code` and
 * `message` rather than catching an exception.
 */
export async function runSync(
  root: DirectoryHandleLike,
  repoId: string,
  opts: SyncOptions & BrowserGitOptions = {},
): Promise<SyncResult> {
  const started = Date.now();
  const fs = createGitFs(root);
  const before = await readSyncStatus(root);
  const result: SyncResult = {
    repo: repoId,
    dryRun: opts.dryRun === true,
    // Browser mode has no rebase, so the strategy is forced (§6.2).
    strategy: 'merge',
    phase: 'preflight',
    before,
    after: before,
    pulled: 0,
    pushed: 0,
    retries: 0,
    durationMs: 0,
  };
  const done = (): SyncResult => ({ ...result, durationMs: Date.now() - started });

  try {
    preflight(before, opts);
    const remote = before.remote as string;
    const branch = before.branch;

    result.phase = 'fetch';
    await git.fetch({
      fs,
      http,
      dir: ROOT,
      remote,
      ref: branch,
      singleBranch: true,
      ...(opts.corsProxy ? { corsProxy: opts.corsProxy } : {}),
      ...(opts.onAuth ? { onAuth: opts.onAuth } : {}),
      ...(opts.onAuthFailure ? { onAuthFailure: opts.onAuthFailure } : {}),
    });

    const after = await readSyncStatus(root);
    result.after = after;
    const incoming = await preview(fs, `refs/remotes/${after.upstream}`, branch, after.behind);
    const outgoing = await preview(fs, branch, `refs/remotes/${after.upstream}`, after.ahead);
    if (incoming) result.incoming = incoming;
    if (outgoing) result.outgoing = outgoing;

    if (opts.dryRun === true) {
      result.phase = 'done';
      return done();
    }

    if (after.behind > 0) {
      result.phase = 'integrate';
      await integrate(fs, after, opts);
      result.pulled = after.behind;
    }
    if (opts.push !== false) {
      result.phase = 'push';
      result.pushed = await push(fs, after, opts);
    }
    result.after = await readSyncStatus(root);
    result.phase = 'done';
    return done();
  } catch (error) {
    const failure = asBrowserGitError(error);
    result.code = failure.code;
    result.message = failure.message;
    result.conflicts = failure.conflicts;
    result.phase = failure.code === 'git_conflict' ? 'conflicts' : 'failed';
    return done();
  }
}

/**
 * Merges the fetched upstream into the current branch, out of band of a full
 * sync. It is what the conflict resolver replays once the user has decided:
 * the resolutions in `opts` are handed to the merge driver, so the merge that
 * stopped completes with exactly the text the user accepted.
 *
 * It resolves with the conflicts that stopped it, if any; the working tree is
 * untouched in that case, because the merge is rolled back.
 */
export async function mergeUpstream(
  root: DirectoryHandleLike,
  opts: BrowserGitOptions = {},
): Promise<BrowserConflict[]> {
  const fs = createGitFs(root);
  const status = await readSyncStatus(root);
  if (!status.upstream) {
    throw new BrowserGitError(
      'git_no_upstream',
      `Branch ${status.branch} tracks no remote branch yet, so there is nothing to merge.`,
    );
  }
  const seen: BrowserConflict[] = [];
  try {
    await integrate(fs, status, {
      ...opts,
      onConflict: (conflicts) => {
        seen.push(...conflicts);
        opts.onConflict?.(conflicts);
      },
    });
  } catch (error) {
    const failure = asBrowserGitError(error);
    if (failure.code !== 'git_conflict') throw failure;
  }
  return seen;
}

/** Refuses a run the browser cannot complete, before it touches the network. */
function preflight(status: SyncStatus, opts: SyncOptions & BrowserGitOptions): void {
  if (status.detached) {
    throw new BrowserGitError(
      'git_unexpected_branch',
      'This repository has a detached HEAD: check out the branch you want to sync first.',
    );
  }
  if (!status.remote) {
    throw new BrowserGitError('git_no_remote', 'This repository has no git remote to sync with.');
  }
  if (isSshRemote(status.remoteUrl)) {
    throw new BrowserGitError('git_unsupported', SSH_REMOTE_REASON);
  }
  if (!opts.corsProxy) {
    throw new BrowserGitError('git_cors_proxy_required', CORS_PROXY_REASON);
  }
  if (!status.upstream) {
    throw new BrowserGitError(
      'git_no_upstream',
      `Branch ${status.branch} tracks no remote branch yet: push it once from a terminal, then sync.`,
    );
  }
  if (status.operation) {
    throw new BrowserGitError(
      'git_operation_in_progress',
      `A ${status.operation} is already in progress here: finish it or abort it before syncing.`,
    );
  }
  if (status.trackedChanges && opts.dryRun !== true) {
    throw new BrowserGitError(
      'git_dirty_tree',
      'There are uncommitted changes to tracked files: commit them before syncing. Nothing was fetched.',
    );
  }
}

/**
 * Merges the fetched work and brings the working tree up to it.
 *
 * The merge runs with our own driver, which is what gives browser mode a
 * conflict surface at all (docs/06 §6.2): `isomorphic-git`'s own merge would
 * only report the paths, while the driver sees the base, ours and theirs blobs
 * of every conflicting file. A path the caller already resolved merges cleanly
 * with that text, which is how a resolution finishes the merge.
 */
async function integrate(fs: GitFs, status: SyncStatus, opts: BrowserGitOptions): Promise<void> {
  const author = opts.author ?? (await readAuthor(fs));
  const seen: BrowserConflict[] = [];
  try {
    await git.merge({
      fs,
      dir: ROOT,
      ours: status.branch,
      theirs: `refs/remotes/${status.upstream}`,
      abortOnConflict: true,
      author,
      mergeDriver: ({ contents, path }) => {
        const [base = '', ours = '', theirs = ''] = contents;
        const resolved = opts.resolutions?.[path];
        if (resolved !== undefined) return { cleanMerge: true, mergedText: resolved };
        seen.push({ path, kind: 'content', base, ours, theirs });
        // Nothing is written: `abortOnConflict` rolls the merge back, and the
        // three versions above are what the resolver works on.
        return { cleanMerge: false, mergedText: ours };
      },
    });
  } catch (error) {
    const conflicts = seen.length > 0 ? seen.map(asSyncConflict) : conflictsOf(error);
    if (seen.length > 0) opts.onConflict?.(seen);
    if (conflicts.length > 0) {
      throw new BrowserGitError(
        'git_conflict',
        `The merge stopped on ${conflicts.length} conflicted file(s): ` +
          `${conflicts.map((c) => c.path).join(', ')}. Nothing was pushed and your files are untouched; ` +
          'resolve them and sync again.',
        conflicts,
      );
    }
    throw error;
  }
  // isomorphic-git moves the branch but leaves the working tree behind, so the
  // files only appear after an explicit checkout.
  await git.checkout({ fs, dir: ROOT, ref: status.branch, force: true });
}

/** Publishes the local branch, explaining a rejection precisely. */
async function push(fs: GitFs, status: SyncStatus, opts: BrowserGitOptions): Promise<number> {
  const result = await git.push({
    fs,
    http,
    dir: ROOT,
    remote: status.remote,
    ref: status.branch,
    ...(opts.corsProxy ? { corsProxy: opts.corsProxy } : {}),
    ...(opts.onAuth ? { onAuth: opts.onAuth } : {}),
    ...(opts.onAuthFailure ? { onAuthFailure: opts.onAuthFailure } : {}),
  });
  const rejection =
    result.error ?? Object.values(result.refs ?? {}).find((ref) => ref.error)?.error;
  if (rejection) {
    throw new BrowserGitError(
      'git_push_rejected',
      `The remote refused the push: ${rejection}. Your commits are safe locally; sync again to fetch, ` +
        'merge and retry.',
    );
  }
  return status.ahead;
}

/** Lists the commits `to` has and `from` does not, newest first. */
async function preview(
  fs: GitFs,
  from: string,
  to: string,
  expected: number,
): Promise<SyncCommit[] | undefined> {
  if (expected <= 0) return undefined;
  const [fromOids, commits] = await Promise.all([oidsOf(fs, from), logOf(fs, to)]);
  return commits
    .filter((commit) => !fromOids.has(commit.oid))
    .slice(0, PREVIEW_LIMIT)
    .map((commit) => ({
      sha: commit.oid,
      subject: commit.commit.message.split('\n')[0] ?? '',
      author: `${commit.commit.author.name} <${commit.commit.author.email}>`,
      date: new Date(commit.commit.author.timestamp * 1000).toISOString(),
    }));
}

/** Counts the commits each side has that the other lacks. */
async function countAheadBehind(
  fs: GitFs,
  local: string,
  remote: string,
): Promise<{ ahead: number; behind: number }> {
  const [localOids, remoteOids] = await Promise.all([oidsOf(fs, local), oidsOf(fs, remote)]);
  let ahead = 0;
  let behind = 0;
  for (const oid of localOids) if (!remoteOids.has(oid)) ahead += 1;
  for (const oid of remoteOids) if (!localOids.has(oid)) behind += 1;
  return { ahead, behind };
}

/** Reads a bounded history as a set of commit ids. */
async function oidsOf(fs: GitFs, ref: string): Promise<Set<string>> {
  const commits = await logOf(fs, ref);
  return new Set(commits.map((commit) => commit.oid));
}

/** Reads a bounded history, tolerating a ref that does not exist. */
async function logOf(fs: GitFs, ref: string): Promise<ReadCommitResult[]> {
  try {
    return await git.log({ fs, dir: ROOT, ref, depth: WALK_DEPTH });
  } catch {
    return [];
  }
}

/** Reads the uncommitted set, scoped away from the object store. */
async function readDirty(
  fs: GitFs,
): Promise<{ paths: string[]; tracked: boolean; conflicted: SyncConflict[] }> {
  const matrix = await git.statusMatrix({
    fs,
    dir: ROOT,
    filter: (filepath) => !filepath.startsWith('.git/'),
  });
  const paths: string[] = [];
  let tracked = false;
  for (const [filepath, head, workdir, stage] of matrix) {
    if (head === 1 && workdir === 1 && stage === 1) continue;
    paths.push(filepath);
    // head 0 with stage 0 is a file git has never seen: untracked, and
    // untracked files never block an integration.
    if (head !== 0 || stage !== 0) tracked = true;
  }
  // isomorphic-git aborts a conflicting merge instead of leaving markers
  // behind, so a conflicted working tree is not a state it can produce; the
  // field exists because the contract is shared with the companion.
  return { paths: paths.sort(), tracked, conflicted: [] };
}

/** Resolves the remote to sync with, preferring `origin`. */
async function readRemote(fs: GitFs): Promise<{ remote: string; url: string } | undefined> {
  const remotes = await git.listRemotes({ fs, dir: ROOT });
  if (remotes.length === 0) return undefined;
  const chosen = remotes.find((entry) => entry.remote === 'origin') ?? remotes[0];
  return chosen ? { remote: chosen.remote, url: chosen.url } : undefined;
}

/** Reads the identity a merge commit is attributed to. */
async function readAuthor(fs: GitFs): Promise<{ name: string; email: string }> {
  const name: unknown = await git.getConfig({ fs, dir: ROOT, path: 'user.name' });
  const email: unknown = await git.getConfig({ fs, dir: ROOT, path: 'user.email' });
  if (typeof name !== 'string' || typeof email !== 'string' || !name || !email) {
    throw new BrowserGitError(
      'git_no_identity',
      'No git identity is configured for this repository: set an author name and email in ' +
        'Settings → Sync before syncing.',
    );
  }
  return { name, email };
}

/** Reports whether a ref exists. */
async function refExists(fs: GitFs, ref: string): Promise<boolean> {
  try {
    await git.resolveRef({ fs, dir: ROOT, ref });
    return true;
  } catch {
    return false;
  }
}

/** Reports whether a path exists in the mounted folder. */
async function pathExists(fs: GitFs, path: string): Promise<boolean> {
  try {
    await fs.promises.stat(path);
    return true;
  } catch {
    return false;
  }
}

/** Fills the headline state, in the same precedence as the companion. */
function resolveState(status: SyncStatus): SyncStatus {
  status.clean = (status.dirty?.length ?? 0) === 0;
  if ((status.conflicted?.length ?? 0) > 0) status.state = 'conflicted';
  else if (status.operation) status.state = 'in_progress';
  else if (status.detached) status.state = 'detached';
  else if (!status.remote) status.state = 'no_remote';
  else if (!status.upstream) status.state = 'no_upstream';
  else if (status.ahead > 0 && status.behind > 0) status.state = 'diverged';
  else if (status.behind > 0) status.state = 'behind';
  else if (status.ahead > 0) status.state = 'ahead';
  else if (!status.clean) status.state = 'dirty';
  else status.state = 'up_to_date';
  return status;
}

/** Reports whether a remote URL is one only SSH can speak. */
export function isSshRemote(url: string | undefined): boolean {
  if (!url) return false;
  return url.startsWith('ssh://') || (/^[^/]+@[^/]+:/.test(url) && !url.includes('://'));
}

/** Removes any credential a remote URL carries before it is displayed. */
export function redactUrl(url: string): string {
  if (!url.includes('://')) return url;
  const [scheme, rest] = url.split('://', 2) as [string, string];
  const at = rest.indexOf('@');
  const slash = rest.indexOf('/');
  if (at === -1 || (slash !== -1 && at > slash)) return url;
  return `${scheme}://***@${rest.slice(at + 1)}`;
}

/** Narrows a recorded conflict to the shape the sync report carries. */
function asSyncConflict(conflict: BrowserConflict): SyncConflict {
  return { path: conflict.path, kind: conflict.kind };
}

/** Reads the conflicted paths out of an isomorphic-git merge failure. */
function conflictsOf(error: unknown): SyncConflict[] {
  const data = (error as { code?: string; data?: { filepaths?: string[] } } | null)?.data;
  const paths = Array.isArray(data?.filepaths) ? data.filepaths : [];
  return paths.map((path) => ({ path, kind: 'content' }));
}

/** Maps any failure onto the machine codes the UI already knows. */
function asBrowserGitError(error: unknown): BrowserGitError {
  if (error instanceof BrowserGitError) return error;
  const code = (error as { code?: string } | null)?.code ?? '';
  const message = error instanceof Error ? error.message : String(error);
  switch (code) {
    case 'HttpError': {
      const statusCode = (error as { data?: { statusCode?: number } }).data?.statusCode;
      if (statusCode === 401 || statusCode === 403) {
        return new BrowserGitError(
          'git_auth_required',
          'The git host refused the credentials for this repository. ' +
            'Nothing was changed locally; provide a token with `contents: read/write` and try again.',
        );
      }
      return new BrowserGitError(
        'git_network_unavailable',
        `The git host could not be reached (${message}). Your local work is safe; sync again later. ` +
          'A failure here often means the CORS proxy is missing or refusing this host.',
      );
    }
    case 'MergeConflictError':
      return new BrowserGitError('git_conflict', message, conflictsOf(error));
    case 'MergeNotSupportedError':
      return new BrowserGitError(
        'git_unsupported',
        'This merge is beyond what browser git can do. Run the companion for this repository.',
      );
    case 'PushRejectedError':
      return new BrowserGitError(
        'git_push_rejected',
        'The remote already moved on, so the push was rejected. Your commits are safe locally; ' +
          'sync again to fetch, merge and retry.',
      );
    default:
      return new BrowserGitError('git_sync_failed', message);
  }
}
