/**
 * The permission dialog (task GIT-T-0070).
 *
 * **This is a security surface, not a convenience.** Until Pando's adapter-wide
 * tool allow-list lands (PANDO-EP-0002), `HumanInTheLoop = true` with
 * `AutoApprove = false` plus this dialog is the only thing between a prompt and
 * a destructive call: the agent is Pando's full coder agent, with bash, edit
 * and write. Everything below follows from that:
 *
 * - **Nothing defaults to yes.** Deny is the resting state; the primary button
 *   is Deny, and approval takes a deliberate click.
 * - **Dismissal is a denial, but not an accident.** Escape and the backdrop
 *   ask for confirmation rather than answering for the user — a stray keypress
 *   must not be able to end a turn — and the confirmation's own answer is a
 *   denial. Nothing here can close without an answer reaching Pando.
 * - **The arguments are agent output.** They are rendered as escaped text in a
 *   `<pre>`, never Markdown and never HTML.
 * - **"Always allow" is per thread and in memory.** It never reaches
 *   `localStorage`; a standing cross-session grant would be a security
 *   decision needing its own ADR.
 */

import { ShieldAlert, ShieldCheck, ShieldX } from 'lucide-react';
import { useState } from 'react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Switch } from '@/components/ui/switch';
import type { PermissionPrompt } from '@/features/agent/hitl';

export type PermissionDialogProps = {
  prompt: PermissionPrompt;
  /** This tab may not run the thread: show the request, offer no answer. */
  readOnly?: boolean;
  onApprove: (options: { always: boolean }) => void;
  onDeny: () => void;
};

/** Pretty-prints whatever the agent put in `params`, without ever parsing it as markup. */
function paramsText(params: unknown): string {
  if (params === undefined || params === null) return '';
  if (typeof params === 'string') return params;
  try {
    return JSON.stringify(params, null, 2);
  } catch {
    return '(the arguments could not be displayed)';
  }
}

export function PermissionDialog({
  prompt,
  readOnly = false,
  onApprove,
  onDeny,
}: PermissionDialogProps) {
  const [always, setAlways] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const { request } = prompt;
  const params = paramsText(request.params);

  /**
   * Radix reports Escape and a backdrop click as the same close request. Both
   * are answered the same way: hold the dialog open and ask, then deny.
   */
  function onOpenChange(open: boolean): void {
    if (open) return;
    setConfirming(true);
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent
        aria-labelledby="agent-permission-title"
        // The close affordance in the corner is a dismissal like any other.
        onInteractOutside={(event) => {
          event.preventDefault();
          setConfirming(true);
        }}
      >
        <DialogHeader>
          <DialogTitle id="agent-permission-title" className="flex items-center gap-2">
            <ShieldAlert aria-hidden="true" className="h-4 w-4 text-warning" />
            The agent wants to run <span className="font-mono">{request.toolName}</span>
          </DialogTitle>
          <DialogDescription>
            Nothing runs until you say so. Read the arguments below: they are the agent&rsquo;s
            words, not this app&rsquo;s.
          </DialogDescription>
        </DialogHeader>

        <dl className="space-y-2 text-sm">
          <div className="flex gap-2">
            <dt className="w-24 shrink-0 text-muted-foreground">Tool</dt>
            <dd className="min-w-0 break-words font-mono">{request.toolName}</dd>
          </div>
          {request.action === '' ? null : (
            <div className="flex gap-2">
              <dt className="w-24 shrink-0 text-muted-foreground">Action</dt>
              <dd className="min-w-0 break-words font-mono">{request.action}</dd>
            </div>
          )}
          {request.path === undefined ? null : (
            <div className="flex gap-2">
              <dt className="w-24 shrink-0 text-muted-foreground">Path</dt>
              <dd className="min-w-0 break-words font-mono">{request.path}</dd>
            </div>
          )}
          {request.description === undefined ? null : (
            <div className="flex gap-2">
              <dt className="w-24 shrink-0 text-muted-foreground">Description</dt>
              <dd className="min-w-0 whitespace-pre-wrap break-words">{request.description}</dd>
            </div>
          )}
        </dl>

        {params === '' ? null : (
          <section aria-label="Arguments" className="mt-3">
            <h4 className="section-label mb-1">Arguments</h4>
            <pre
              data-testid="permission-params"
              className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-sm bg-surface-muted p-2 font-mono text-xs leading-relaxed"
            >
              {params}
            </pre>
          </section>
        )}

        {readOnly ? (
          <p className="mt-4 text-sm text-muted-foreground">
            Another tab owns this conversation, so it has to answer there.
          </p>
        ) : confirming ? (
          <div
            role="alertdialog"
            aria-label="Confirm the denial"
            className="mt-4 rounded-md border border-warning/40 bg-warning/5 p-3 text-sm"
          >
            <p>
              Closing this refuses the request and the agent continues without it. Deny{' '}
              <span className="font-mono">{request.toolName}</span>?
            </p>
            <div className="mt-3 flex justify-end gap-2">
              <Button variant="ghost" size="sm" onClick={() => setConfirming(false)}>
                Keep deciding
              </Button>
              <Button variant="destructive" size="sm" onClick={onDeny}>
                Deny
              </Button>
            </div>
          </div>
        ) : (
          <>
            <div className="mt-4 flex items-center gap-3 rounded-md bg-surface-muted px-3 py-2 text-sm">
              <Switch
                checked={always}
                onCheckedChange={setAlways}
                aria-label="Always allow"
                aria-describedby="agent-permission-always"
              />
              <span id="agent-permission-always" className="min-w-0 flex-1">
                Always allow <span className="font-mono">{request.toolName}</span> in this
                conversation
                <span className="mt-0.5 block text-xs text-muted-foreground">
                  In memory only. It is gone when you switch conversations or reload, and you can
                  revoke it from the state panel.
                </span>
              </span>
              <Badge variant="outline">this thread</Badge>
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={() => onApprove({ always })}>
                <ShieldCheck aria-hidden="true" className="h-4 w-4" />
                {always ? 'Always allow' : 'Approve once'}
              </Button>
              <Button variant="destructive" onClick={onDeny}>
                <ShieldX aria-hidden="true" className="h-4 w-4" />
                Deny
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
