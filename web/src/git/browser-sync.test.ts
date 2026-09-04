import git from 'isomorphic-git';
import { beforeEach, describe, expect, it } from 'vitest';

import {
  CORS_PROXY_REASON,
  isSshRemote,
  readSyncStatus,
  redactUrl,
  runSync,
  SSH_REMOTE_REASON,
} from './browser-sync';
import { FakeDirectory } from './fake-handles';
import { createGitFs } from './fsa-fs';

/**
 * Browser git runs against a real isomorphic-git repository built through the
 * File System Access adapter — no mock of the library, so what is exercised is
 * the same code path a tab takes (docs/06 §6.1).
 */

/** Builds a repository with one commit and returns its folder handle. */
async function newRepo(): Promise<FakeDirectory> {
  const root = new FakeDirectory();
  const fs = createGitFs(root);
  await git.init({ fs, dir: '/', defaultBranch: 'main' });
  await fs.promises.mkdir('docs');
  await fs.promises.writeFile('docs/index.md', '# fixture\n');
  await git.add({ fs, dir: '/', filepath: 'docs/index.md' });
  await git.commit({
    fs,
    dir: '/',
    message: 'chore: seed the fixture',
    author: { name: 'Test User', email: 'test@example.com' },
  });
  return root;
}

describe('readSyncStatus', () => {
  let root: FakeDirectory;

  beforeEach(async () => {
    root = await newRepo();
  });

  it('reports the branch and that there is no remote yet', async () => {
    const status = await readSyncStatus(root);
    expect(status.branch).toBe('main');
    expect(status.clean).toBe(true);
    expect(status.state).toBe('no_remote');
  });

  it('reports a remote with no tracking branch as no_upstream', async () => {
    const fs = createGitFs(root);
    await git.addRemote({
      fs,
      dir: '/',
      remote: 'origin',
      url: 'https://example.test/acme/web.git',
    });

    const status = await readSyncStatus(root);
    expect(status.remote).toBe('origin');
    expect(status.remoteUrl).toBe('https://example.test/acme/web.git');
    expect(status.upstream).toBeUndefined();
    expect(status.state).toBe('no_upstream');
  });

  it('separates a tracked edit from an untracked file', async () => {
    const fs = createGitFs(root);
    // A repository with no remote reads as `no_remote` whatever else is true,
    // so give it one to see the dirty state itself.
    await git.addRemote({
      fs,
      dir: '/',
      remote: 'origin',
      url: 'https://example.test/acme/web.git',
    });
    const head = await git.resolveRef({ fs, dir: '/', ref: 'main' });
    await git.writeRef({ fs, dir: '/', ref: 'refs/remotes/origin/main', value: head, force: true });

    await fs.promises.writeFile('scratch.txt', 'notes\n');
    let status = await readSyncStatus(root);
    expect(status.clean).toBe(false);
    expect(status.trackedChanges).toBe(false);
    expect(status.dirty).toContain('scratch.txt');

    await fs.promises.writeFile('docs/index.md', '# edited\n');
    status = await readSyncStatus(root);
    expect(status.trackedChanges).toBe(true);
    expect(status.state).toBe('dirty');
  });

  it('counts ahead of a tracking branch that was left behind', async () => {
    const fs = createGitFs(root);
    await git.addRemote({
      fs,
      dir: '/',
      remote: 'origin',
      url: 'https://example.test/acme/web.git',
    });
    // Pin origin/main at the first commit, then commit again locally: exactly
    // the state a user is in after working offline.
    const head = await git.resolveRef({ fs, dir: '/', ref: 'main' });
    await git.writeRef({ fs, dir: '/', ref: 'refs/remotes/origin/main', value: head, force: true });
    await fs.promises.writeFile('docs/next.md', 'next\n');
    await git.add({ fs, dir: '/', filepath: 'docs/next.md' });
    await git.commit({
      fs,
      dir: '/',
      message: 'docs: add next',
      author: { name: 'Test User', email: 'test@example.com' },
    });

    const status = await readSyncStatus(root);
    expect(status.upstream).toBe('origin/main');
    expect(status.ahead).toBe(1);
    expect(status.behind).toBe(0);
    expect(status.state).toBe('ahead');
  });
});

describe('runSync', () => {
  it('refuses to touch the network without a CORS proxy, and says why', async () => {
    const root = await newRepo();
    const fs = createGitFs(root);
    await git.addRemote({
      fs,
      dir: '/',
      remote: 'origin',
      url: 'https://example.test/acme/web.git',
    });

    const result = await runSync(root, 'demo');
    expect(result.phase).toBe('failed');
    expect(result.code).toBe('git_cors_proxy_required');
    expect(result.message).toBe(CORS_PROXY_REASON);
    expect(result.strategy).toBe('merge');
    expect(result.pulled).toBe(0);
    expect(result.pushed).toBe(0);
  });

  it('refuses an SSH remote instead of failing obscurely', async () => {
    const root = await newRepo();
    const fs = createGitFs(root);
    await git.addRemote({ fs, dir: '/', remote: 'origin', url: 'git@example.test:acme/web.git' });

    const result = await runSync(root, 'demo', { corsProxy: 'https://proxy.test' });
    expect(result.code).toBe('git_unsupported');
    expect(result.message).toBe(SSH_REMOTE_REASON);
  });

  it('refuses a repository with no remote', async () => {
    const result = await runSync(await newRepo(), 'demo', { corsProxy: 'https://proxy.test' });
    expect(result.code).toBe('git_no_remote');
  });

  it('explains a branch that tracks nothing yet', async () => {
    const root = await newRepo();
    const fs = createGitFs(root);
    await git.addRemote({
      fs,
      dir: '/',
      remote: 'origin',
      url: 'https://example.test/acme/web.git',
    });

    const result = await runSync(root, 'demo', { corsProxy: 'https://proxy.test' });
    expect(result.code).toBe('git_no_upstream');
    expect(result.message).toContain('main');
  });

  it('always reports the merge strategy, because there is no rebase here', async () => {
    const result = await runSync(await newRepo(), 'demo', { strategy: 'rebase' });
    expect(result.strategy).toBe('merge');
  });
});

describe('remote URL handling', () => {
  it('never shows a credential', () => {
    expect(redactUrl('https://x-access-token:ghp_secret@example.test/acme/web.git')).toBe(
      'https://***@example.test/acme/web.git',
    );
    expect(redactUrl('https://example.test/acme/web.git')).toBe(
      'https://example.test/acme/web.git',
    );
  });

  it('recognises the remotes a tab cannot speak', () => {
    expect(isSshRemote('git@example.test:acme/web.git')).toBe(true);
    expect(isSshRemote('ssh://git@example.test/acme/web.git')).toBe(true);
    expect(isSshRemote('https://example.test/acme/web.git')).toBe(false);
    expect(isSshRemote(undefined)).toBe(false);
  });
});
