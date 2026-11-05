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

  it('shows team views until the user turns them off (GIT-US-0204)', async () => {
    const useUiPrefs = await loadPrefs();
    expect(useUiPrefs.getState().showTeamViews).toBe(true);
  });

  it('persists the team views toggle across a reload (GIT-US-0204)', async () => {
    const useUiPrefs = await loadPrefs();
    useUiPrefs.getState().setShowTeamViews(false);

    const stored = JSON.parse(globalThis.localStorage.getItem(STORAGE_KEY) ?? '{}') as {
      showTeamViews?: boolean;
    };
    expect(stored.showTeamViews).toBe(false);

    const reloaded = await loadPrefs();
    expect(reloaded.getState().showTeamViews).toBe(false);
  });
});
