import { beforeEach, describe, expect, it } from 'vitest';

import {
  IDENTITY_STORAGE_KEY,
  getIdentity,
  resetIdentityCache,
  setIdentity,
  toHandle,
} from './identity';

describe('the participant identity of a browser', () => {
  beforeEach(() => {
    globalThis.localStorage.clear();
    resetIdentityCache();
  });

  it('slugifies a name into the handle a retro file accepts', () => {
    expect(toHandle('Ada Lovelace')).toBe('ada-lovelace');
    expect(toHandle('José F. Rives')).toBe('jose-f-rives');
    expect(toHandle('  MARTA  ')).toBe('marta');
    // A name that reduces to nothing yields no handle rather than a made-up one.
    expect(toHandle('!!!')).toBe('');
    expect(toHandle('')).toBe('');
  });

  it('truncates to the 32 characters the handle shape allows', () => {
    const handle = toHandle('a'.repeat(40));
    expect(handle).toHaveLength(32);
  });

  it('remembers the handle across sessions and forgets it on demand', () => {
    setIdentity('Ada Lovelace');
    expect(globalThis.localStorage.getItem(IDENTITY_STORAGE_KEY)).toBe('ada-lovelace');

    resetIdentityCache();
    expect(getIdentity()).toBe('ada-lovelace');

    setIdentity('');
    expect(getIdentity()).toBe('');
    expect(globalThis.localStorage.getItem(IDENTITY_STORAGE_KEY)).toBeNull();
  });
});
