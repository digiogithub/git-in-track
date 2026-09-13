/**
 * The backlog's entry into the import dialog (story GIT-US-0059).
 *
 * Two capabilities gate it, and they are different questions. `youtrackSupported`
 * asks whether this *runtime* can reach YouTrack at all — false in browser-only
 * mode, where there is no process to hold a credential — and `youtrack` asks
 * whether a project is actually linked. The settings card deliberately shows on
 * the first alone, because a card that appeared only once a project was
 * connected could never connect the first one. This button is the opposite
 * case: importing from an instance nothing is linked to is meaningless, so it
 * needs both.
 */

import { Download } from 'lucide-react';
import { useState } from 'react';

import { useOptionalProvider } from '@/api/provider-context';
import { Button } from '@/components/ui/button';
import { ImportDialog } from '@/features/youtrack/ImportDialog';

export function ImportFromYouTrackButton({ projectKey }: { projectKey: string }) {
  const provider = useOptionalProvider();
  const [open, setOpen] = useState(false);

  const capabilities = provider?.capabilities;
  if (!capabilities?.youtrackSupported || !capabilities.youtrack) return null;

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        onClick={() => {
          setOpen(true);
        }}
      >
        <Download aria-hidden="true" className="h-4 w-4" />
        Import from YouTrack
      </Button>
      <ImportDialog open={open} onOpenChange={setOpen} projectKey={projectKey} />
    </>
  );
}
