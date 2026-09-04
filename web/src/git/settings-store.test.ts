import { beforeEach, describe, expect, it, vi } from 'vitest';

import { CommitTemplateError, DEFAULT_COMMIT_TEMPLATE } from '@/git/message';
import {
  BROWSER_GIT_REASON,
  DEFAULT_COMMIT_DEBOUNCE_MS,
  clearGitSettings,
  readGitSettings,
  readSyncSettings,
  writeGitSettings,
  writeSyncSettings,
} from '@/git/settings-store';

describe('browser git settings', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    globalThis.localStorage.clear();
  });

  it('is off by default with the shipped template', () => {
    const settings = readGitSettings();
    expect(settings.commitOnSave).toBe(false);
    expect(settings.commitDebounceMs).toBe(DEFAULT_COMMIT_DEBOUNCE_MS);
    expect(settings.messageTemplate).toBe(DEFAULT_COMMIT_TEMPLATE);
  });

  it('explains that browser-only mode cannot commit yet', () => {
    expect(readGitSettings().supported).toBe(false);
    expect(readGitSettings().reason).toBe(BROWSER_GIT_REASON);
  });

  it('round-trips a change through storage', () => {
    const saved = writeGitSettings({ commitOnSave: true, messageTemplate: '{{action}} {{id}}' });
    expect(saved.persisted).toBe(true);
    const reloaded = readGitSettings();
    expect(reloaded.commitOnSave).toBe(true);
    expect(reloaded.messageTemplate).toBe('{{action}} {{id}}');
  });

  it('keeps one workspace out of another', () => {
    writeGitSettings({ commitOnSave: true }, 'work');
    expect(readGitSettings('oss').commitOnSave).toBe(false);
  });

  it('refuses a broken template before storing anything', () => {
    expect(() => writeGitSettings({ messageTemplate: '{{nope}}' })).toThrow(CommitTemplateError);
    expect(readGitSettings().messageTemplate).toBe(DEFAULT_COMMIT_TEMPLATE);
  });

  it('refuses a negative window', () => {
    expect(() => writeGitSettings({ commitDebounceMs: -1 })).toThrow(RangeError);
  });

  it('stores no credential of any kind', () => {
    writeGitSettings({ commitOnSave: true, authorName: 'Marta', authorEmail: 'marta@acme.dev' });
    const raw = globalThis.localStorage.getItem('gintrack.git.settings.default') ?? '';
    for (const forbidden of ['token', 'password', 'secret', 'credential']) {
      expect(raw.toLowerCase()).not.toContain(forbidden);
    }
  });

  it('falls back to the defaults when storage is unavailable', () => {
    vi.stubGlobal('localStorage', {
      getItem() {
        throw new Error('blocked');
      },
      setItem() {
        throw new Error('blocked');
      },
      removeItem() {
        throw new Error('blocked');
      },
    });
    expect(readGitSettings().commitOnSave).toBe(false);
    expect(writeGitSettings({ commitOnSave: true }).persisted).toBe(false);
  });

  it('forgets a workspace on request', () => {
    writeGitSettings({ commitOnSave: true });
    clearGitSettings();
    expect(readGitSettings().commitOnSave).toBe(false);
  });

  it('recovers from a corrupt entry', () => {
    globalThis.localStorage.setItem('gintrack.git.settings.default', 'not json');
    expect(readGitSettings().messageTemplate).toBe(DEFAULT_COMMIT_TEMPLATE);
  });
});

describe('browser sync settings (GIT-US-0021)', () => {
  it('is unsupported until a CORS proxy is configured, and says why', () => {
    const settings = readSyncSettings('proxy-test');
    expect(settings.supported).toBe(false);
    expect(settings.reason).toContain('CORS proxy');
    // isomorphic-git has no rebase, so the strategy is forced, not stored.
    expect(settings.pullStrategy).toBe('merge');
  });

  it('becomes supported once a proxy is stored', () => {
    const saved = writeSyncSettings({ corsProxy: 'https://proxy.example/' }, 'proxy-test');
    expect(saved.supported).toBe(true);
    expect(saved.corsProxy).toBe('https://proxy.example/');
    expect(readSyncSettings('proxy-test').supported).toBe(true);
  });

  it('refuses a proxy that is not an http URL', () => {
    expect(() => writeSyncSettings({ corsProxy: 'proxy.example' }, 'proxy-test')).toThrow(RangeError);
  });
});
