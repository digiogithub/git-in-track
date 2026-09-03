import { beforeEach, describe, expect, it } from 'vitest';

import { readOnlyCapabilities } from '@/api/provider';

import { useAppStore } from './store';

describe('useAppStore', () => {
  beforeEach(() => {
    useAppStore.getState().reset();
  });

  it('boots in detecting mode with read-only capabilities', () => {
    const state = useAppStore.getState();
    expect(state.mode).toBe('detecting');
    expect(state.companionVersion).toBeNull();
    expect(state.capabilities).toEqual(readOnlyCapabilities);
  });

  it('records the companion version when switching to companion mode', () => {
    useAppStore.getState().setMode('companion', '0.4.0');

    const state = useAppStore.getState();
    expect(state.mode).toBe('companion');
    expect(state.companionVersion).toBe('0.4.0');
  });

  it('clears the companion version when falling back to browser mode', () => {
    useAppStore.getState().setMode('companion', '0.4.0');
    useAppStore.getState().setMode('browser');

    expect(useAppStore.getState().companionVersion).toBeNull();
  });

  it('replaces the capability snapshot', () => {
    useAppStore.getState().setCapabilities({ ...readOnlyCapabilities, write: true, git: true });

    const { capabilities } = useAppStore.getState();
    expect(capabilities.write).toBe(true);
    expect(capabilities.git).toBe(true);
    expect(capabilities.maxBatchWrite).toBe(0);
  });
});
