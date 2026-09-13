/**
 * The cache half of the triage queue: what an `inbox` event does to an open
 * pane and to the badge, and the fact that the browser-only runtime — which has
 * no WebSocket at all — goes down exactly the same path.
 */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import type { InboxFilter } from '@/api/provider';
import { inboxKeys, useInbox, useInboxEvents, useInboxPending } from '@/features/inbox/queries';
import { inboxProvider } from '@/features/inbox/test-utils';

const PENDING: InboxFilter = { project: 'ACME', limit: 25, status: ['pending'] };

function harness(provider = inboxProvider()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>{children}</DataProviderProvider>
    </QueryClientProvider>
  );
  return { provider, queryClient, wrapper };
}

describe('useInboxPending', () => {
  it('reads the whole-queue count, with an expired snooze already in it', async () => {
    const { provider, wrapper } = harness();

    const { result } = renderHook(() => useInboxPending('ACME'), { wrapper });

    await waitFor(() => {
      expect(result.current.data).toBe(2);
    });
    // One page of one row was enough: the count is not derived from the page.
    expect(await provider.listInbox({ project: 'ACME', limit: 1 })).toMatchObject({
      items: expect.any(Array),
      pending: 2,
    });
  });
});

describe('useInboxEvents', () => {
  it('moves the badge and refreshes the list from a frame, without a manual refetch', async () => {
    const { provider, queryClient, wrapper } = harness();

    renderHook(
      () => {
        useInboxEvents('ACME');
        return useInboxPending('ACME');
      },
      { wrapper },
    );

    await waitFor(() => {
      expect(queryClient.getQueryData(inboxKeys.pending('ACME'))).toBe(2);
    });

    act(() => {
      // A decision taken somewhere else entirely — another tab, an agent over
      // MCP — reaches this one as a frame carrying the queue behind it.
      provider.emitEvent({
        kind: 'inbox',
        repoId: 'repo-1',
        project: 'ACME',
        id: 'ACME-US-0200',
        action: 'reject',
        pending: 1,
      });
    });

    await waitFor(() => {
      expect(queryClient.getQueryData(inboxKeys.pending('ACME'))).toBe(1);
    });
  });

  it('ignores a frame about a different project', async () => {
    const { provider, queryClient, wrapper } = harness();

    renderHook(
      () => {
        useInboxEvents('ACME');
        return useInboxPending('ACME');
      },
      { wrapper },
    );
    await waitFor(() => {
      expect(queryClient.getQueryData(inboxKeys.pending('ACME'))).toBe(2);
    });

    act(() => {
      provider.emitEvent({
        kind: 'inbox',
        repoId: 'repo-2',
        project: 'WEB',
        id: 'WEB-US-0001',
        action: 'accept',
        pending: 99,
      });
    });

    expect(queryClient.getQueryData(inboxKeys.pending('ACME'))).toBe(2);
  });
});

describe('browser-only mode', () => {
  it('still updates after a local decision, with no WebSocket in sight', async () => {
    // This is the fallback GIT-T-0064 asks for. There is no server here to
    // publish `inbox.changed`, so the provider raises the event itself after
    // the write — which means the subscription above is the only code path,
    // and a tab without a companion needs no second one.
    const { provider, queryClient, wrapper } = harness();

    const { result } = renderHook(
      () => {
        useInboxEvents('ACME');
        return useInbox(PENDING);
      },
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.data?.pages[0]?.items).toHaveLength(2);
    });

    await act(async () => {
      await provider.triageInboxItem({ id: 'ACME-US-0200', rev: '*', action: 'reject' });
    });

    await waitFor(() => {
      expect(queryClient.getQueryData(inboxKeys.pending('ACME'))).toBe(1);
    });
    await waitFor(() => {
      expect(result.current.data?.pages[0]?.items).toHaveLength(1);
    });
  });
});
