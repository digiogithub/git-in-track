/**
 * A small, dismissible strip saying a newer gintrack release exists
 * (story GIT-US-0195, docs/05-web-app.md).
 *
 * It asks the provider, never the network: the companion answers from a cached
 * lookup, browser-only mode answers `supported: false` and the strip stays
 * hidden. Dismissal is remembered per version in `localStorage`, so the next
 * release is announced again; every storage access is wrapped because storage
 * may be blocked, in which case the notice simply comes back next load.
 */

import { useQuery } from '@tanstack/react-query';
import { ArrowUpCircle, X } from 'lucide-react';
import { useState } from 'react';

import { useOptionalProvider } from '@/api/provider-context';
import { Button } from '@/components/ui/button';

/** The key holding the last version whose notice the user dismissed. */
export const UPDATE_DISMISSED_KEY = 'gintrack.update.dismissedVersion';

function readDismissed(): string | null {
  try {
    return window.localStorage.getItem(UPDATE_DISMISSED_KEY);
  } catch {
    return null;
  }
}

function writeDismissed(version: string): void {
  try {
    window.localStorage.setItem(UPDATE_DISMISSED_KEY, version);
  } catch {
    // Storage is a convenience: without it the notice returns on the next load.
  }
}

export function UpdateNotice() {
  const provider = useOptionalProvider();
  const [dismissed, setDismissed] = useState<string | null>(readDismissed);

  const status = useQuery({
    queryKey: ['version-status'],
    queryFn: () => provider!.getVersionStatus(),
    enabled: provider !== null,
    // An update notice is not worth retrying or refetching on focus.
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 60 * 60 * 1000,
  });

  const data = status.data;
  if (!data || !data.supported || !data.updateAvailable || data.latest === '') return null;
  if (dismissed === data.latest) return null;

  const latest = data.latest;
  return (
    <div
      role="status"
      className="flex items-start gap-3 border-b border-border bg-surface-muted/70 px-6 py-2.5 text-sm text-foreground lg:px-8"
    >
      <span className="mt-0.5 shrink-0 text-muted-foreground">
        <ArrowUpCircle aria-hidden="true" className="h-4 w-4" />
      </span>
      <p className="flex-1 leading-relaxed">
        <strong className="font-medium">gintrack {latest} is available.</strong>
        {data.current ? ` You are running ${data.current}.` : ''} Run{' '}
        <code className="rounded-sm bg-code px-1 py-0.5 font-mono text-xs">gintrack update</code> to
        upgrade.
        {data.url ? (
          <>
            {' '}
            <a
              className="font-medium underline underline-offset-4"
              href={data.url}
              target="_blank"
              rel="noreferrer"
            >
              Release notes
            </a>
            .
          </>
        ) : null}
      </p>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="Dismiss update notice"
        onClick={() => {
          writeDismissed(latest);
          setDismissed(latest);
        }}
      >
        <X aria-hidden="true" className="h-4 w-4" />
      </Button>
    </div>
  );
}
