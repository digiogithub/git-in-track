/**
 * Memory proposals in the inbox (ADR-044, GIT-US-0216): a memory is accepted
 * in one click, because it has no status or parent to pick, and a
 * knowledge-only project hides its backlog links but keeps its inbox.
 */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider, sampleItems, sampleTriageProject } from '@/api/fake-provider';
import type { Item, ProjectSummary } from '@/api/provider';
import { BacklogOnly } from '@/app/layout/AppShell';
import { inboxProvider, renderInbox } from '@/features/inbox/test-utils';

const proposedMemory: Item = {
  id: 'ACME-MEM-0001',
  type: 'memory',
  title: 'Customer X is invoiced quarterly',
  status: 'triage',
  created: '2026-09-01T10:00:00Z',
  author: 'pando-ana',
  inbox: { status: 'pending', source: 'memsync' },
  memory: {
    key: 'customer-x-invoicing',
    version: 3,
    source: { kind: 'agent', ref: 'memsync/inst-1' },
  },
  body: 'Customer X receives one invoice per quarter.',
  path: 'docs/.pmngr/memory/ACME-MEM-0001-customer-x-is-invoiced-quarterly.md',
  rev: 'sha256:0000000000000001',
};

describe('a memory proposal in the inbox', () => {
  it('shows its key and source, and is accepted in one click', async () => {
    const provider = inboxProvider({ items: [...sampleItems, proposedMemory] });
    const triage = vi.spyOn(provider, 'triageInboxItem');
    const { router } = renderInbox({ provider, path: '/p/ACME/inbox?selected=ACME-MEM-0001' });

    expect(await screen.findByText('customer-x-invoicing')).toBeInTheDocument();
    expect(screen.getByText('memsync/inst-1')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /^Accept/ }));
    await waitFor(() => {
      expect(triage).toHaveBeenCalledWith(
        expect.objectContaining({ id: 'ACME-MEM-0001', action: 'accept' }),
      );
    });
    // No accept form: the location stays on the queue.
    expect(router.state.location.pathname).toBe('/p/ACME/inbox');
  });
});

function withProvider(project: ProjectSummary, ui: React.ReactNode) {
  const provider = new FakeProvider({ projects: [project], items: [] });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>{ui}</DataProviderProvider>
    </QueryClientProvider>,
  );
}

describe('BacklogOnly', () => {
  it('hides the backlog links of a knowledge-only project', async () => {
    const knowledge: ProjectSummary = { ...sampleTriageProject, kind: 'knowledge' };
    withProvider(knowledge, <BacklogOnly project={knowledge.key}>backlog link</BacklogOnly>);
    await waitFor(() => {
      expect(screen.queryByText('backlog link')).not.toBeInTheDocument();
    });
  });

  it('keeps them for a backlog project', async () => {
    withProvider(
      sampleTriageProject,
      <BacklogOnly project={sampleTriageProject.key}>backlog link</BacklogOnly>,
    );
    expect(await screen.findByText('backlog link')).toBeInTheDocument();
  });
});
