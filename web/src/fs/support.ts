/**
 * Browser capability detection for the workspace UI (docs/05-web-app.md §6.3,
 * story GIT-US-0011).
 *
 * Detection runs before any picker is offered, so an unsupported browser never
 * sees a button that throws.
 */

export type FolderAccessLevel = 'read-write' | 'read-only' | 'none';

export type FolderSupport = {
  /** `showDirectoryPicker` is available: full browser-only mode. */
  fileSystemAccess: boolean;
  /** `<input webkitdirectory>` is available: read-only fallback. */
  directoryInput: boolean;
  level: FolderAccessLevel;
  /** One sentence for the banner and the empty state. */
  summary: string;
};

/** True in Chromium 108+ (Chrome, Edge, Brave, Opera, Arc). */
export function supportsFileSystemAccess(): boolean {
  return (
    typeof (globalThis as { showDirectoryPicker?: unknown }).showDirectoryPicker === 'function'
  );
}

/** True where `<input type="file" webkitdirectory>` selects a whole folder. */
export function supportsDirectoryInput(): boolean {
  if (typeof document === 'undefined') return false;
  return 'webkitdirectory' in document.createElement('input');
}

export const READ_ONLY_EXPLANATION =
  'This browser can read a folder but not write to it. Use a Chromium browser (Chrome, Edge, Brave, Opera) for full editing, or install the gintrack companion, which removes the limitation everywhere.';

export const UNSUPPORTED_EXPLANATION =
  'This browser cannot open a local folder at all. Install the gintrack companion and open http://127.0.0.1:7317, or use a Chromium browser.';

/** Everything the UI needs to explain what this browser can do. */
export function detectFolderSupport(): FolderSupport {
  const fileSystemAccess = supportsFileSystemAccess();
  const directoryInput = supportsDirectoryInput();

  if (fileSystemAccess) {
    return {
      fileSystemAccess,
      directoryInput,
      level: 'read-write',
      summary: 'This browser can open a local folder and save changes back to it.',
    };
  }
  if (directoryInput) {
    return {
      fileSystemAccess,
      directoryInput,
      level: 'read-only',
      summary: READ_ONLY_EXPLANATION,
    };
  }
  return {
    fileSystemAccess,
    directoryInput,
    level: 'none',
    summary: UNSUPPORTED_EXPLANATION,
  };
}

export type SupportRow = {
  browser: string;
  picker: string;
  read: string;
  write: string;
  notes: string;
};

/** The support matrix rendered next to the pickers (docs/05-web-app.md §6.3). */
export const SUPPORT_MATRIX: SupportRow[] = [
  {
    browser: 'Chrome / Edge 108+',
    picker: 'Yes',
    read: 'Yes',
    write: 'Yes',
    notes: 'Full browser-only mode',
  },
  {
    browser: 'Opera / Brave / Arc',
    picker: 'Yes',
    read: 'Yes',
    write: 'Yes',
    notes: 'Chromium engine',
  },
  {
    browser: 'Safari 17+',
    picker: 'No',
    read: 'Folder input',
    write: 'No',
    notes: 'Read-only knowledge base and backlog',
  },
  {
    browser: 'Firefox 128+',
    picker: 'No',
    read: 'Folder input',
    write: 'No',
    notes: 'Read-only; companion recommended',
  },
  {
    browser: 'Any browser + companion',
    picker: 'Not needed',
    read: 'Yes',
    write: 'Yes',
    notes: 'Recommended everywhere',
  },
];
