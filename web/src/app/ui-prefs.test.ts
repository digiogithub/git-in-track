import { beforeEach, describe, expect, it, vi } from 'vitest';

const STORAGE_KEY = 'gintrack:ui-prefs';

/**
 * The store reads `localStorage` once, when the module is first evaluated, so
 * a "reload" is a module reset followed by a fresh import.
 */
async function loadPrefs() {
  vi.resetModules();
  const mod = await import('./ui-prefs');
  return mod.useUiPrefs;
}

describe('ui preferences', () => {
  beforeEach(() => {
    globalThis.localStorage.clear();
  });

  it('shows semantic results until the user turns them off', async () => {
    const useUiPrefs = await loadPrefs();
    expect(useUiPrefs.getState().semanticResults).toBe(true);
  });

  it('persists the semantic results toggle across a reload', async () => {
    const useUiPrefs = await loadPrefs();
    useUiPrefs.getState().setSemanticResults(false);
    expect(useUiPrefs.getState().semanticResults).toBe(false);

    const stored = JSON.parse(globalThis.localStorage.getItem(STORAGE_KEY) ?? '{}') as {
      semanticResults?: boolean;
    };
    expect(stored.semanticResults).toBe(false);

    const reloaded = await loadPrefs();
    expect(reloaded.getState().semanticResults).toBe(false);
  });

  it('keeps the other layout settings when the toggle is written', async () => {
    const useUiPrefs = await loadPrefs();
    useUiPrefs.getState().setKbTocOpen(false);
    useUiPrefs.getState().setSemanticResults(false);

    const reloaded = await loadPrefs();
    expect(reloaded.getState().kbTocOpen).toBe(false);
    expect(reloaded.getState().semanticResults).toBe(false);
  });
});

describe('workspace layout (GIT-US-0205)', () => {
  beforeEach(() => {
    globalThis.localStorage.clear();
  });

  it('lays the repositories out as a list by default', async () => {
    const useUiPrefs = await loadPrefs();
    expect(useUiPrefs.getState().workspaceLayout).toBe('list');
  });

  it('persists the cards layout across a reload', async () => {
    const useUiPrefs = await loadPrefs();
    useUiPrefs.getState().setWorkspaceLayout('cards');

    const reloaded = await loadPrefs();
    expect(reloaded.getState().workspaceLayout).toBe('cards');
  });

  it('falls back to the list for an unknown stored value', async () => {
    globalThis.localStorage.setItem(STORAGE_KEY, JSON.stringify({ workspaceLayout: 'grid' }));
    const useUiPrefs = await loadPrefs();
    expect(useUiPrefs.getState().workspaceLayout).toBe('list');
  });
});
