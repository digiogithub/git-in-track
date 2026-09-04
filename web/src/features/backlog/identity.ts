/**
 * "Mine" needs to know who you are. Until the workspace carries a git identity
 * (Phase 4 settings), the last handle typed into the assignee filter is
 * remembered per browser — a convenience, never a source of truth.
 */

import { useCallback, useState } from 'react';

const STORAGE_KEY = 'gintrack.identity';

export function readIdentity(): string | null {
  try {
    const value = window.localStorage.getItem(STORAGE_KEY);
    return value && value.length > 0 ? value : null;
  } catch {
    return null;
  }
}

export function writeIdentity(handle: string): void {
  try {
    if (handle.length > 0) window.localStorage.setItem(STORAGE_KEY, handle);
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // A private window with storage disabled simply has no remembered handle.
  }
}

export function useIdentity(): { identity: string | null; remember: (handle: string) => void } {
  const [identity, setIdentity] = useState<string | null>(() => readIdentity());

  const remember = useCallback((handle: string) => {
    writeIdentity(handle);
    setIdentity(handle.length > 0 ? handle : null);
  }, []);

  return { identity, remember };
}
