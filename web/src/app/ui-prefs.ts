import { create } from 'zustand';

/**
 * Layout preferences that outlive the tab: whether the main sidebar is folded
 * away, how wide the docs tree is, which docs panels show, and which
 * repositories the sidebar keeps out of sight. They are per-browser
 * conveniences, so they live in `localStorage` and never in the repository.
 */

const STORAGE_KEY = 'gintrack:ui-prefs';

/** Narrowest the docs tree may be dragged, in pixels. */
export const KB_TREE_MIN_WIDTH = 160;
/** Widest the docs tree may be dragged, in pixels. */
export const KB_TREE_MAX_WIDTH = 560;
export const KB_TREE_DEFAULT_WIDTH = 240;

type StoredPrefs = {
  sidebarCollapsed: boolean;
  kbTreeWidth: number;
  kbTreeOpen: boolean;
  kbTocOpen: boolean;
  /** The docs page fills the whole view: tree, outline and sidebar all fold. */
  kbMaximized: boolean;
  /**
   * The sidebar state to restore when the docs page leaves maximized mode, or
   * `null` when the user has touched the sidebar since and their choice wins.
   */
  sidebarBeforeMaximize: boolean | null;
  hiddenRepos: string[];
};

export type UiPrefsState = StoredPrefs & {
  setSidebarCollapsed: (collapsed: boolean) => void;
  setKbTreeWidth: (width: number) => void;
  setKbTreeOpen: (open: boolean) => void;
  setKbTocOpen: (open: boolean) => void;
  setKbMaximized: (maximized: boolean) => void;
  toggleRepoHidden: (repoId: string) => void;
};

export function clampKbTreeWidth(width: number): number {
  if (!Number.isFinite(width)) return KB_TREE_DEFAULT_WIDTH;
  return Math.round(Math.min(KB_TREE_MAX_WIDTH, Math.max(KB_TREE_MIN_WIDTH, width)));
}

function defaults(): StoredPrefs {
  // A phone-sized first visit starts with the sidebar folded: it would
  // otherwise cover the whole screen.
  const narrow = typeof globalThis.innerWidth === 'number' && globalThis.innerWidth < 768;
  return {
    sidebarCollapsed: narrow,
    kbTreeWidth: KB_TREE_DEFAULT_WIDTH,
    kbTreeOpen: true,
    kbTocOpen: true,
    kbMaximized: false,
    sidebarBeforeMaximize: null,
    hiddenRepos: [],
  };
}

function bool(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback;
}

function readPrefs(): StoredPrefs {
  const base = defaults();
  try {
    const raw = globalThis.localStorage?.getItem(STORAGE_KEY);
    if (!raw) return base;
    const parsed = JSON.parse(raw) as Partial<Record<keyof StoredPrefs, unknown>>;
    return {
      sidebarCollapsed: bool(parsed.sidebarCollapsed, base.sidebarCollapsed),
      kbTreeWidth:
        typeof parsed.kbTreeWidth === 'number'
          ? clampKbTreeWidth(parsed.kbTreeWidth)
          : base.kbTreeWidth,
      kbTreeOpen: bool(parsed.kbTreeOpen, base.kbTreeOpen),
      kbTocOpen: bool(parsed.kbTocOpen, base.kbTocOpen),
      kbMaximized: bool(parsed.kbMaximized, base.kbMaximized),
      sidebarBeforeMaximize:
        typeof parsed.sidebarBeforeMaximize === 'boolean' ? parsed.sidebarBeforeMaximize : null,
      hiddenRepos: Array.isArray(parsed.hiddenRepos)
        ? parsed.hiddenRepos.filter((id): id is string => typeof id === 'string')
        : base.hiddenRepos,
    };
  } catch {
    // Private modes and sandboxes can throw, and old values can be malformed:
    // the defaults are always a valid layout.
    return base;
  }
}

function writePrefs(prefs: StoredPrefs): void {
  try {
    globalThis.localStorage?.setItem(STORAGE_KEY, JSON.stringify(prefs));
  } catch {
    // The preference still holds for this tab; only its persistence is lost.
  }
}

export const useUiPrefs = create<UiPrefsState>((set, get) => {
  const update = (patch: Partial<StoredPrefs>) => {
    set(patch);
    const state = get();
    writePrefs({
      sidebarCollapsed: state.sidebarCollapsed,
      kbTreeWidth: state.kbTreeWidth,
      kbTreeOpen: state.kbTreeOpen,
      kbTocOpen: state.kbTocOpen,
      kbMaximized: state.kbMaximized,
      sidebarBeforeMaximize: state.sidebarBeforeMaximize,
      hiddenRepos: state.hiddenRepos,
    });
  };

  return {
    ...readPrefs(),
    setSidebarCollapsed: (sidebarCollapsed) =>
      update({ sidebarCollapsed, sidebarBeforeMaximize: null }),
    setKbTreeWidth: (width) => update({ kbTreeWidth: clampKbTreeWidth(width) }),
    setKbTreeOpen: (kbTreeOpen) => update({ kbTreeOpen }),
    setKbTocOpen: (kbTocOpen) => update({ kbTocOpen }),
    setKbMaximized: (maximized) => {
      const state = get();
      if (maximized === state.kbMaximized) return;
      if (maximized) {
        update({
          kbMaximized: true,
          sidebarBeforeMaximize: state.sidebarCollapsed,
          sidebarCollapsed: true,
        });
      } else {
        update({
          kbMaximized: false,
          sidebarCollapsed: state.sidebarBeforeMaximize ?? state.sidebarCollapsed,
          sidebarBeforeMaximize: null,
        });
      }
    },
    toggleRepoHidden: (repoId) => {
      const hidden = get().hiddenRepos;
      update({
        hiddenRepos: hidden.includes(repoId)
          ? hidden.filter((id) => id !== repoId)
          : [...hidden, repoId],
      });
    },
  };
});
