import { FolderOpen, FolderSearch } from 'lucide-react';
import { useMemo, useRef, useState } from 'react';

import { Button } from '@/components/ui/button';
import {
  detectFolderSupport,
  pickDirectory,
  registerVault,
  WebkitDirectoryVault,
  type FolderSupport,
} from '@/fs';

/**
 * The two ways to open a local folder (docs/05-web-app.md §6.3).
 *
 * Picking a folder needs transient user activation, so it happens here, in a
 * click handler, and the resulting vault is handed to the provider by id. This
 * is why the workspace feature — and only the workspace feature — talks to the
 * filesystem layer directly.
 */
export type FolderPickersProps = {
  onPicked: (vaultId: string, name: string, writable: boolean) => void;
  /** Rendered by the caller next to its own errors. */
  onError?: (message: string) => void;
};

export function FolderPickers({ onPicked, onError }: FolderPickersProps) {
  const support = useMemo<FolderSupport>(() => detectFolderSupport(), []);
  const inputRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);

  async function openWritableFolder() {
    setBusy(true);
    try {
      const vault = await pickDirectory();
      onPicked(registerVault(vault), vault.name, true);
    } catch (error) {
      // A cancelled picker is not a failure: the user changed their mind.
      const name = error instanceof Error ? error.name : '';
      if (name !== 'AbortError') {
        onError?.(error instanceof Error ? error.message : 'Could not open that folder');
      }
    } finally {
      setBusy(false);
    }
  }

  function handleFallbackFiles(files: FileList | null) {
    if (!files || files.length === 0) return;
    const vault = new WebkitDirectoryVault(Array.from(files));
    onPicked(registerVault(vault), vault.name, false);
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-2">
        <Button
          onClick={() => void openWritableFolder()}
          disabled={!support.fileSystemAccess || busy}
          title={
            support.fileSystemAccess ? 'Open a folder with read and write access' : support.summary
          }
        >
          <FolderOpen aria-hidden="true" className="h-4 w-4" />
          Open folder
        </Button>

        <Button
          variant="outline"
          onClick={() => inputRef.current?.click()}
          disabled={!support.directoryInput}
          title={
            support.directoryInput
              ? 'Load a folder into memory; changes cannot be saved back'
              : support.summary
          }
        >
          <FolderSearch aria-hidden="true" className="h-4 w-4" />
          Choose folder (read-only)
        </Button>

        <input
          ref={inputRef}
          type="file"
          multiple
          aria-label="Choose a folder to open read-only"
          tabIndex={-1}
          className="sr-only"
          onChange={(event) => {
            handleFallbackFiles(event.target.files);
            event.target.value = '';
          }}
          {...({ webkitdirectory: '', directory: '' } as Record<string, string>)}
        />
      </div>

      <p className="text-xs text-muted-foreground">{support.summary}</p>
    </div>
  );
}
