import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';

import { useOptionalProvider } from '@/api/provider-context';
import { getToken } from '@/api/token';
import { Button } from '@/components/ui/button';
import { accessLink, shareLink } from '@/features/workspace/share-link';

/**
 * Getting the room into this retro (ADR-027, ADR-028).
 *
 * A retro is the one screen several people use at once, and the way they reach
 * it is the public tunnel the companion is already able to open. This is the
 * link for it, deep into this retro rather than into the workspace root, in the
 * two shapes of docs/07 §5.1: the plain address, and the address with the
 * access token in it.
 *
 * The second one is a credential and says so. It is offered here anyway,
 * because the alternative — dictating a token in a call — is what people
 * actually do, and they paste it into the same chat window either way.
 */
export function RetroShare({ retroId }: { retroId: string }) {
  const provider = useOptionalProvider();
  const [copied, setCopied] = useState('');

  const tunnel = useQuery({
    queryKey: ['tunnel', 'status'],
    queryFn: () => provider?.getTunnel() ?? Promise.reject(new Error('no provider')),
    enabled: provider !== null,
    refetchInterval: 15_000,
  });

  const status = tunnel.data;
  if (!status?.supported || status.state !== 'connected' || status.url === '') return null;

  const token = getToken();
  const path = `retros/${retroId}`;
  const copy = (value: string, what: string) => {
    void navigator.clipboard?.writeText(value).then(() => setCopied(what));
  };

  return (
    <div className="flex flex-wrap items-center gap-2 text-xs">
      <span className="text-muted-foreground">Share this retro:</span>
      <Button size="sm" variant="outline" onClick={() => copy(shareLink(status.url, path), 'link')}>
        Copy link
      </Button>
      <Button
        size="sm"
        variant="outline"
        disabled={token === null}
        onClick={() => {
          if (token === null) return;
          copy(accessLink(status.url, token, path), 'link with access');
        }}
      >
        Copy link with access
      </Button>
      {copied === '' ? null : <span className="text-muted-foreground">Copied the {copied}.</span>}
      <p className="w-full text-destructive">
        <strong className="font-semibold">The access link is a credential.</strong> It carries this
        tab’s token, and whoever opens it can read and write every repository this companion has
        open. The plain link asks them for the token instead.
      </p>
    </div>
  );
}
