/**
 * The token prompt of browser-only mode (story GIT-US-0023, docs/06 §8.2).
 *
 * It opens only when a transport actually asks for a credential — never at
 * mount time, never "just in case" — and what the user types goes straight back
 * to the pending `onAuth` call and into the in-memory store of
 * `@/git/credentials`. Nothing is persisted, the field is a password input that
 * is never echoed anywhere, and the dialog names the CORS proxy the request
 * will travel through before the token is typed, because that proxy sees the
 * `Authorization` header (§6.3).
 */

import { useEffect, useState } from 'react';

import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  onCredentialRequests,
  rejectCredentialRequest,
  resolveCredentialRequest,
  type CredentialRequest,
} from '@/git/credentials';

/** Renders the first pending credential request, if there is one. */
export function CredentialPrompt() {
  const [requests, setRequests] = useState<CredentialRequest[]>([]);

  useEffect(() => onCredentialRequests(setRequests), []);

  const request = requests[0];
  if (!request) return null;
  return <PromptDialog key={request.id} request={request} />;
}

/** One request's dialog. Its state dies with the request. */
function PromptDialog({ request }: { request: CredentialRequest }) {
  const [username, setUsername] = useState(request.suggestedUsername);
  const [token, setToken] = useState('');

  const cancel = () => {
    rejectCredentialRequest(request.id);
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) cancel();
      }}
    >
      <DialogContent aria-describedby="credential-prompt-description">
        <DialogHeader>
          <DialogTitle>{request.host} needs a token</DialogTitle>
          <DialogDescription id="credential-prompt-description">
            {request.remoteUrl} asked for a credential. The token is kept in this tab’s memory
            only, for {request.host} and no other host, and is forgotten when you reload or sign
            out. It is never written to storage or to a file.
          </DialogDescription>
        </DialogHeader>

        <form
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            if (token.trim() === '') return;
            resolveCredentialRequest(request.id, {
              username: username.trim() === '' ? request.suggestedUsername : username.trim(),
              token,
            });
          }}
        >
          {request.corsProxy ? (
            <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm">
              This request is proxied through <code>{request.corsProxy}</code>, which will see it
              and the token it carries. Only continue if that proxy is one you or your team run.
            </p>
          ) : null}

          <div className="space-y-1">
            <Label htmlFor="credential-username">Username</Label>
            <Input
              id="credential-username"
              autoComplete="off"
              value={username}
              onChange={(event) => {
                setUsername(event.target.value);
              }}
            />
          </div>

          <div className="space-y-1">
            <Label htmlFor="credential-token">Personal access token</Label>
            <Input
              id="credential-token"
              type="password"
              autoComplete="off"
              value={token}
              onChange={(event) => {
                setToken(event.target.value);
              }}
            />
            <p className="text-xs text-muted-foreground">
              A repository-scoped token with <code>contents: read/write</code> is enough; nothing
              else is used.
            </p>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={cancel}>
              Cancel
            </Button>
            <Button type="submit" disabled={token.trim() === ''}>
              Use for this session
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
