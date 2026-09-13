/**
 * "Send to YouTrack" on a comment (story GIT-US-0076, task GIT-T-0183).
 *
 * The three things worth pinning down are the gate, the three states, and who
 * owns the truth. The action exists only where all three conditions hold; the
 * badge says pending, sent or failed; and every one of those comes from the
 * comment file plus the job stream, never from the fact that a button was
 * clicked — which is what makes the state survive a reload.
 */

import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import {
  FakeProvider,
  sampleComments,
  sampleItems,
  type FakeYouTrack,
} from '@/api/fake-provider';
import type { Comment, Item, SyncJobEvent } from '@/api/provider';

import { resetCommentPushes, COMMENT_PUSH_JOB_KIND } from './comment-sync';
import { renderBacklog } from './test-utils';

const COMMENT_PATH = sampleComments[0]!.path;

/** A connected project, the way the companion reports one. */
const connected: FakeYouTrack = {
  settings: {
    configured: true,
    url: 'https://yt.example.com/youtrack',
    project: 'ACME',
    hasToken: true,
    tokenSource: 'file',
    pushComments: 'manual',
  },
};

/** The story, mirroring a YouTrack issue. Without this there is nowhere to post. */
function linkedItems(): Item[] {
  return sampleItems.map((item) =>
    item.id === 'ACME-US-0042'
      ? {
          ...item,
          external: [
            { system: 'youtrack', id: 'ACME-42', url: 'https://yt.example.com/issue/ACME-42' },
          ],
        }
      : item,
  );
}

function comments(external?: Comment['external']): Comment[] {
  const base = sampleComments[0]!;
  return [{ ...base, ...(external === undefined ? {} : { external }) }];
}

type SetupOptions = {
  youtrack?: FakeYouTrack | undefined;
  items?: Item[];
  comments?: Comment[];
};

function setup(options: SetupOptions = {}) {
  const provider = new FakeProvider({
    items: options.items ?? linkedItems(),
    comments: options.comments ?? comments(),
    ...(options.youtrack === undefined ? {} : { youtrack: options.youtrack }),
  });
  renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });
  return provider;
}

async function thread() {
  return within(await screen.findByRole('list', { name: 'Comment thread' }));
}

function jobFrame(overrides: Partial<SyncJobEvent> = {}): SyncJobEvent {
  return {
    phase: 'failed',
    id: 'job_comment_1',
    kind: COMMENT_PUSH_JOB_KIND,
    key: COMMENT_PATH,
    state: 'failed',
    attempt: 3,
    processed: 0,
    total: 1,
    error: 'YouTrack rejected the token',
    errorClass: 'terminal',
    ...overrides,
  };
}

beforeEach(() => {
  resetCommentPushes();
});

describe('the gate', () => {
  it('is absent in browser-only mode, where nothing can reach YouTrack', async () => {
    // No `youtrack` fixture at all: `capabilities.youtrack` is false, exactly
    // as it is in a tab with no companion.
    setup({ youtrack: undefined });

    expect(await (await thread()).findByText('Northwind is the pilot tenant.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Send to YouTrack' })).toBeNull();
    expect(screen.queryByText('Not sent to YouTrack')).toBeNull();
  });

  it('is absent when the project is not connected to a YouTrack project', async () => {
    setup({ youtrack: { settings: { configured: false, project: '' } } });

    expect(await (await thread()).findByText('Northwind is the pilot tenant.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Send to YouTrack' })).toBeNull();
  });

  it('is absent when the item mirrors no issue, because there is nothing to post to', async () => {
    setup({ youtrack: connected, items: sampleItems });

    expect(await (await thread()).findByText('Northwind is the pilot tenant.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Send to YouTrack' })).toBeNull();
  });

  it('appears once the runtime, the project and the item all qualify', async () => {
    setup({ youtrack: connected });

    expect(await screen.findByRole('button', { name: 'Send to YouTrack' })).toBeInTheDocument();
    expect(screen.getByText('Not sent to YouTrack')).toBeInTheDocument();
  });
});

describe('the three states', () => {
  it('shows a comment that already carries a reference as sent, with a link', async () => {
    setup({
      youtrack: connected,
      comments: comments([
        {
          system: 'youtrack',
          id: '4-19',
          url: 'https://yt.example.com/issue/ACME-42#comment=4-19',
        },
      ]),
    });

    const sent = await screen.findByRole('link', { name: '4-19' });
    expect(sent).toHaveAttribute('href', 'https://yt.example.com/issue/ACME-42#comment=4-19');
    // Nothing to do: it is already there.
    expect(screen.queryByRole('button', { name: 'Send to YouTrack' })).toBeNull();
  });

  it('goes pending on a push and reaches sent when the job writes the reference back', async () => {
    const provider = setup({ youtrack: connected });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Send to YouTrack' }));

    expect(await screen.findByText('Sending to YouTrack…')).toBeInTheDocument();

    // What the job does when the remote comment exists: it writes the id into
    // the comment file. That file — not the click — is what "sent" means.
    act(() => {
      provider.completeCommentPush(COMMENT_PATH, {
        system: 'youtrack',
        id: '4-19',
        url: 'https://yt.example.com/issue/ACME-42#comment=4-19',
      });
    });

    expect(await screen.findByRole('link', { name: '4-19' })).toBeInTheDocument();
  });

  it('shows the job error and offers a retry when the push fails', async () => {
    const provider = setup({ youtrack: connected });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Send to YouTrack' }));
    await screen.findByText('Sending to YouTrack…');

    act(() => {
      provider.emitEvent({ kind: 'syncJob', job: jobFrame() });
    });

    expect(await screen.findByText(/YouTrack rejected the token/)).toBeInTheDocument();
    const retry = await screen.findByRole('button', { name: 'Retry' });

    // Retrying queues it again: the badge goes back to pending.
    await user.click(retry);
    expect(await screen.findByText('Sending to YouTrack…')).toBeInTheDocument();
  });

  it('picks up a push started elsewhere, without this tab having clicked anything', async () => {
    const provider = setup({ youtrack: connected });
    await screen.findByRole('button', { name: 'Send to YouTrack' });

    act(() => {
      provider.emitEvent({
        kind: 'syncJob',
        job: jobFrame({ phase: 'progress', state: 'running', error: '', errorClass: '' }),
      });
    });

    expect(await screen.findByText('Sending to YouTrack…')).toBeInTheDocument();
  });
});

describe('auto mode', () => {
  it('keeps the badge and drops the action, because everything is pushed anyway', async () => {
    setup({
      youtrack: { settings: { ...connected.settings, pushComments: 'auto' } },
    });

    expect(await screen.findByText('Not sent to YouTrack')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Send to YouTrack' })).toBeNull();
    });
  });
});

describe('what is deliberately missing', () => {
  it('offers no way to delete the comment remotely', async () => {
    setup({
      youtrack: connected,
      comments: comments([{ system: 'youtrack', id: '4-19' }]),
    });

    await screen.findByText(/Sent as/);
    // Deletion is local only, by design: a repository is not the authority on
    // an issue's conversation.
    expect(screen.queryByRole('button', { name: /delete.*youtrack/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /remove.*remote/i })).toBeNull();
  });
});
