import { create } from 'zustand';

import type { Capabilities } from '@/api/provider';
import { readOnlyCapabilities } from '@/api/provider';

/**
 * Which runtime the app is talking to.
 *
 * - `detecting` — the health probe has not answered yet (boot state).
 * - `companion` — `gintrack serve` is reachable; native core, fsnotify, git.
 * - `browser`   — browser-only mode; File System Access + the WASM core.
 */
export type AppMode = 'browser' | 'companion' | 'detecting';

export type ModeSlice = {
  mode: AppMode;
  /** Version reported by the companion, when one answered the probe. */
  companionVersion: string | null;
  capabilities: Capabilities;
  setMode: (mode: AppMode, companionVersion?: string | null) => void;
  setCapabilities: (capabilities: Capabilities) => void;
  reset: () => void;
};

const initialState = {
  mode: 'detecting' as AppMode,
  companionVersion: null,
  capabilities: readOnlyCapabilities,
} satisfies Pick<ModeSlice, 'mode' | 'companionVersion' | 'capabilities'>;

/**
 * Client-only state. Server/provider state belongs to TanStack Query and
 * navigational state belongs to the URL; this store holds neither.
 *
 * Components read it through selectors (`useAppStore((s) => s.mode)`) so a
 * change to one field does not re-render everything.
 */
export const useAppStore = create<ModeSlice>((set) => ({
  ...initialState,
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
  reset: () => {
    set({ ...initialState });
  },
}));
