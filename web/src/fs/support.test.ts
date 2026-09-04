import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  detectFolderSupport,
  READ_ONLY_EXPLANATION,
  SUPPORT_MATRIX,
  supportsDirectoryInput,
  supportsFileSystemAccess,
  UNSUPPORTED_EXPLANATION,
} from './support';

/** jsdom implements neither API, so both are simulated explicitly. */
function withDirectoryInput(): () => void {
  Object.defineProperty(HTMLInputElement.prototype, 'webkitdirectory', {
    value: false,
    configurable: true,
  });
  return () => {
    delete (HTMLInputElement.prototype as unknown as Record<string, unknown>)['webkitdirectory'];
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('browser support detection', () => {
  it('detects nothing in an environment without either API', () => {
    expect(supportsFileSystemAccess()).toBe(false);
    expect(supportsDirectoryInput()).toBe(false);

    const support = detectFolderSupport();
    expect(support.level).toBe('none');
    expect(support.summary).toBe(UNSUPPORTED_EXPLANATION);
  });

  it('reports the read-only fallback when only the folder input exists', () => {
    const restore = withDirectoryInput();
    try {
      expect(supportsDirectoryInput()).toBe(true);

      const support = detectFolderSupport();
      expect(support.level).toBe('read-only');
      expect(support.fileSystemAccess).toBe(false);
      expect(support.directoryInput).toBe(true);
      expect(support.summary).toBe(READ_ONLY_EXPLANATION);
    } finally {
      restore();
    }
  });

  it('reports full support when the directory picker exists', () => {
    vi.stubGlobal('showDirectoryPicker', () => Promise.resolve());

    const support = detectFolderSupport();
    expect(supportsFileSystemAccess()).toBe(true);
    expect(support.level).toBe('read-write');
  });

  it('documents every browser row shown next to the pickers', () => {
    expect(SUPPORT_MATRIX.map((row) => row.browser)).toContain('Firefox 128+');
    expect(SUPPORT_MATRIX.every((row) => row.notes.length > 0)).toBe(true);
  });
});
