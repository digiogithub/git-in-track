/**
 * The knowledge-base sync toolbar (story GIT-US-0093, task GIT-T-0217).
 *
 * Three actions and a state. Publishing one page is a click; publishing a
 * **folder** is a click plus a confirmation that says how many pages it will
 * touch, because "publish" over a handbook is hundreds of articles and a
 * reader is entitled to know that before it starts. "Sync now" is the other
 * direction — it pulls the article's content back into the page — and the badge
 * doubles as the way to ask whether the article has moved, which is the one
 * question that costs a request.
 *
 * Nothing here happens inline: both directions queue a job and return, and the
 * job reports itself over the `sync.job.*` stream the viewer already listens
 * to. The toasts therefore say "queued", never "published" — claiming the
 * second would be a lie the moment an instance is slow.
 *
 * The manual publish reads the project's `kb_sync` setting rather than assuming
 * it. Under `on_write` a save already enqueues a publish, so a button labelled
 * "Publish to YouTrack" would be describing the setting's work as its own: it
 * relabels to "Publish now" and the toolbar says where publishing actually
 * comes from. The button is not disabled — publishing this page this instant is
 * still a thing to want, most obviously when the last automatic job failed.
 */

import { Link2Off, RefreshCw, UploadCloud } from 'lucide-react';
import { useState } from 'react';

import type { KbPageSyncStatus, KbSyncSelector } from '@/api/provider';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useToast } from '@/components/ui/toast';
import { PUBLISH_SCOPE_NOTE } from '@/features/kb/kb-sync';
import { KbSyncBadge } from '@/features/kb/KbSyncBadge';
import { useKbSyncJob, useKbUnlink } from '@/features/kb/useKbData';
import { youtrackMessage } from '@/features/settings/youtrack-messages';
import { useYouTrackSettings } from '@/features/settings/youtrack-queries';

export type KbSyncToolbarProps = {
  project: string;
  /** The page the viewer is showing; `''` when the viewer has no page open. */
  path: string;
  /** The folder that page lives in, which a folder publish selects. */
  folder: string;
  /** Every page path at or below `folder`, so the confirmation can count them. */
  folderPages: string[];
  status: KbPageSyncStatus | undefined;
  /** Whether `status` was produced by actually reading the article. */
  remote: boolean;
  statusPending: boolean;
  statusError: Error | null;
  /** Asks for a fresh answer with the article read; the viewer owns that query. */
  onCheck: () => void;
  checking: boolean;
};

export function KbSyncToolbar({
  project,
  path,
  folder,
  folderPages,
  status,
  remote,
  statusPending,
  statusError,
  onCheck,
  checking,
}: KbSyncToolbarProps) {
  const { toast } = useToast();
  const publish = useKbSyncJob(project, 'publish');
  const pull = useKbSyncJob(project, 'pull');
  const unlink = useKbUnlink(project);
  const [confirmFolder, setConfirmFolder] = useState(false);
  const [confirmUnlink, setConfirmUnlink] = useState(false);
  const settings = useYouTrackSettings(project);

  // What the project's own setting already does, which is what decides whether
  // the manual button is the way pages reach YouTrack or merely the fast one.
  // An unset direction means `push`, the same default the settings card shows.
  const direction = settings.data?.kbSyncDirection ?? '';
  const publishesOnWrite = settings.data?.kbSync === 'on_write' && direction !== 'pull';

  const busy = publish.isPending || pull.isPending || unlink.isPending;
  // Only a page that mirrors an article has anything to forget.
  const linked = status?.linked === true;
  const folderLabel = folder === '' ? 'this project' : folder;

  const run = (
    direction: 'publish' | 'pull',
    selector: KbSyncSelector,
    what: string,
    onDone?: () => void,
  ) => {
    const mutation = direction === 'publish' ? publish : pull;
    mutation.mutate(selector, {
      onSuccess: (job) => {
        onDone?.();
        toast({
          title:
            direction === 'publish'
              ? `Publishing ${what} to YouTrack`
              : `Pulling ${what} from YouTrack`,
          description:
            job.pages.length === 1
              ? 'One page was queued; the job reports when it lands.'
              : `${job.pages.length} pages were queued; the job reports when they land.`,
        });
      },
      onError: (error) => {
        toast({
          variant: 'destructive',
          title: direction === 'publish' ? 'Nothing was published' : 'Nothing was pulled',
          description: youtrackMessage(error),
        });
      },
    });
  };

  return (
    <div className="flex flex-wrap items-center gap-2" data-testid="kb-sync-toolbar">
      <Button
        variant="outline"
        size="sm"
        disabled={path === '' || busy}
        title={
          publishesOnWrite
            ? 'This project publishes on save; this queues a publish straight away'
            : undefined
        }
        onClick={() => {
          run('publish', { path }, 'this page');
        }}
      >
        <UploadCloud className="h-4 w-4" aria-hidden="true" />
        {publishesOnWrite ? 'Publish now' : 'Publish to YouTrack'}
      </Button>
      <Button
        variant="ghost"
        size="sm"
        disabled={folderPages.length === 0 || busy}
        onClick={() => {
          setConfirmFolder(true);
        }}
      >
        Publish folder…
      </Button>
      <Button
        variant="ghost"
        size="sm"
        disabled={path === '' || busy}
        title="Brings the article's content back into this page"
        onClick={() => {
          run('pull', { path }, 'this page');
        }}
      >
        <RefreshCw className="h-4 w-4" aria-hidden="true" />
        Sync now
      </Button>

      {statusError ? (
        <span className="text-xs text-destructive" role="status">
          The sync state could not be read: {youtrackMessage(statusError)}
        </span>
      ) : statusPending ? (
        <span className="text-xs text-muted-foreground">Reading the sync state…</span>
      ) : status ? (
        <KbSyncBadge status={status} remote={remote} />
      ) : null}

      <Button
        variant="ghost"
        size="sm"
        disabled={path === '' || checking}
        title="Reads the article to see whether it has changed"
        onClick={onCheck}
      >
        {checking ? 'Checking…' : 'Check the article'}
      </Button>
      {linked ? (
        <Button
          variant="ghost"
          size="sm"
          disabled={busy}
          title="Forgets the article this page mirrors; the article itself is left alone"
          onClick={() => {
            setConfirmUnlink(true);
          }}
        >
          <Link2Off className="h-4 w-4" aria-hidden="true" />
          Unlink
        </Button>
      ) : null}

      {publishesOnWrite ? (
        <span className="w-full text-xs text-muted-foreground">
          This project is set to publish on save: saving a page already queues a publish, and these
          buttons only bring it forward.
        </span>
      ) : null}

      <Dialog open={confirmUnlink} onOpenChange={setConfirmUnlink}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Unlink this page from YouTrack?</DialogTitle>
            <DialogDescription>
              The reference to{' '}
              {status?.articleId === undefined || status.articleId === ''
                ? 'the article'
                : status.articleId}{' '}
              is removed from this page. Nothing is sent to YouTrack: the article stays exactly
              where it is, and this project stays connected.
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            Publishing this page afterwards creates a new article rather than updating that one, so
            re-linking it means publishing again — or pasting the reference back by hand.
          </p>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                setConfirmUnlink(false);
              }}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => {
                unlink.mutate(
                  { path },
                  {
                    onSuccess: (result) => {
                      setConfirmUnlink(false);
                      toast({
                        title: result.unlinked
                          ? 'The page was unlinked'
                          : 'The page mirrored no article',
                        description: result.unlinked
                          ? 'Its YouTrack reference was removed. The article itself was not touched.'
                          : 'Nothing was written: there was no reference to remove.',
                      });
                    },
                    onError: (error) => {
                      toast({
                        variant: 'destructive',
                        title: 'The page was not unlinked',
                        description: youtrackMessage(error),
                      });
                    },
                  },
                );
              }}
            >
              Unlink the page
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={confirmFolder} onOpenChange={setConfirmFolder}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Publish {folderLabel} to YouTrack?</DialogTitle>
            <DialogDescription>
              {folderPages.length === 1
                ? 'This will touch 1 page.'
                : `This will touch ${folderPages.length} pages.`}{' '}
              Each one is created as an article, or updated in place when it already mirrors one.
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">{PUBLISH_SCOPE_NOTE}</p>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                setConfirmFolder(false);
              }}
            >
              Cancel
            </Button>
            <Button
              variant="accent"
              disabled={busy}
              onClick={() => {
                run('publish', { path: folder, recursive: true }, folderLabel, () => {
                  setConfirmFolder(false);
                });
              }}
            >
              Publish {folderPages.length === 1 ? '1 page' : `${folderPages.length} pages`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
