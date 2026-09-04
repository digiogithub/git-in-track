import { create } from 'zustand';

import type { Capabilities } from '@/api/provider';
import { readOnlyCapabilities } from '@/api/provider';

/** Key used to keep the read-only notice dismissed for the browsing session. */
const READ_ONLY_NOTICE_KEY = 'gintrack:read-only-notice-dismissed';

function readSessionFlag(key: string): boolean {
  try {
    return globalThis.sessionStorage?.getItem(key) === '1';
  } catch {
    // Private modes and sandboxes can throw on access; the notice simply shows.
    return false;
  }
}

function writeSessionFlag(key: string, value: boolean): void {
  try {
    if (value) globalThis.sessionStorage?.setItem(key, '1');
    else globalThis.sessionStorage?.removeItem(key);
  } catch {
    // Nothing to do: the flag is a convenience, not state we depend on.
  }
}

/**
 * Which runtime the app is talking to.
 *
 * - `detecting` — the health probe has not answered yet (boot state).
 * - `companion` — `gintrack serve` is reachable; native core, fsnotify, git.
 * - `browser`   — browser-only mode; File System Access + the WASM core.
 */
export type AppMode = 'browser' | 'companion' | 'detecting';

/**
 * Workspace slice: the folder the user picked but has not mounted yet, the id
 * of the repository the UI is looking at, and whether the read-only notice was
 * dismissed. Repository *data* is provider state and lives in TanStack Query.
 */
export type WorkspaceSlice = {
  /** Id returned by `registerVault`, handed to the add-repository wizard. */
  pendingVaultId: string | null;
  pendingVaultName: string | null;
  activeRepoId: string | null;
  readOnlyNoticeDismissed: boolean;
  setPendingVault: (id: string | null, name?: string | null) => void;
  setActiveRepo: (repoId: string | null) => void;
  dismissReadOnlyNotice: () => void;
};

export type ModeSlice = {
  mode: AppMode;
  /** Version reported by the companion, when one answered the probe. */
  companionVersion: string | null;
  capabilities: Capabilities;
  setMode: (mode: AppMode, companionVersion?: string | null) => void;
  setCapabilities: (capabilities: Capabilities) => void;
  reset: () => void;
};

export type AppState = ModeSlice & WorkspaceSlice;

const initialState = {
  mode: 'detecting' as AppMode,
  companionVersion: null,
  capabilities: readOnlyCapabilities,
  pendingVaultId: null,
  pendingVaultName: null,
  activeRepoId: null,
  readOnlyNoticeDismissed: false,
} satisfies Pick<
  AppState,
  | 'mode'
  | 'companionVersion'
  | 'capabilities'
  | 'pendingVaultId'
  | 'pendingVaultName'
  | 'activeRepoId'
  | 'readOnlyNoticeDismissed'
>;

/**
 * Client-only state. Server/provider state belongs to TanStack Query and
 * navigational state belongs to the URL; this store holds neither.
 *
 * Components read it through selectors (`useAppStore((s) => s.mode)`) so a
 * change to one field does not re-render everything.
 */
export const useAppStore = create<AppState>((set) => ({
  ...initialState,
  readOnlyNoticeDismissed: readSessionFlag(READ_ONLY_NOTICE_KEY),
  setMode: (mode, companionVersion) => {
    set((state) => ({
      mode,
      companionVersion:
        companionVersion === undefined
          ? mode === 'companion'
            ? state.companionVersion
            : null
          : companionVersion,
    }));
  },
  setCapabilities: (capabilities) => {
    set({ capabilities });
  },
  setPendingVault: (pendingVaultId, pendingVaultName = null) => {
    set({ pendingVaultId, pendingVaultName });
  },
  setActiveRepo: (activeRepoId) => {
    set({ activeRepoId });
  },
  dismissReadOnlyNotice: () => {
    writeSessionFlag(READ_ONLY_NOTICE_KEY, true);
    set({ readOnlyNoticeDismissed: true });
  },
  reset: () => {
    writeSessionFlag(READ_ONLY_NOTICE_KEY, false);
    set({ ...initialState });
  },
}));
