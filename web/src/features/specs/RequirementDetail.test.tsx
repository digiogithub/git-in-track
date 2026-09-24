import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { configure, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import {
  FakeProvider,
  sampleItems,
  sampleProject,
  type FakeSpecAnalysis,
} from '@/api/fake-provider';
import type { Item, Requirement } from '@/api/provider';
import { BROWSER_SPEC_ANALYSIS_REASON } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { RequirementDetail } from '@/features/specs/RequirementDetail';

configure({ asyncUtilTimeout: 5_000 });

const spec: Item = {
  id: 'ACME-SP-0001',
  type: 'spec',
  title: 'Checkout',
  status: 'todo',
  body: '## Requirements\n',
  path: 'docs/.pmngr/specs/ACME-SP-0001-checkout.md',
  rev: 'sha256:spec',
};

const requirement: Requirement = {
  ref: 'ACME-SP-0001.R2',
  spec: 'ACME-SP-0001',
  path: spec.path,
  anchor: 'acme-sp-0001-r2',
  line: 7,
  title: 'Keep the cart',
  status: 'in_progress',
  text: 'WHEN the session expires, the cart SHALL be kept.\n\n#### Scenario: expired session\n\n- **WHEN** the session expires\n- **THEN** the cart is kept\n',
  verified: {
    rev: 'sha256:oldblock',
    commit: '0123456789abcdef0123456789abcdef01234567',
    at: '2026-09-20T10:00:00Z',
    by: 'ci',
  },
  rev: 'sha256:req-rev',
  blockRev: 'sha256:block-rev',
};

const analysis: FakeSpecAnalysis = {
  coverage: [
    {
      ref: 'ACME-SP-0001.R2',
      status: 'suspect',
      reasons: ['text', 'stamp'],
      tests: [
        { test: 'web/src/cart.test.ts#keeps the cart', result: 'pass' },
        { test: 'internal/cart/cart_test.go#TestKeep', result: 'fail' },
      ],
    },
  ],
  traces: {
    'ACME-SP-0001.R2': {
      ref: 'ACME-SP-0001.R2',
      code: [
        {
          ref: 'ACME-SP-0001.R2',
          role: 'code',
          path: 'internal/cart/cart.go',
          symbol: 'Keep',
          sources: ['marker'],
          lines: [40, 12],
        },
        {
          ref: 'ACME-SP-0001.R2',
          role: 'code',
          path: 'internal/cart/store.go',
          sources: ['trace'],
        },
        {
          ref: 'ACME-SP-0001.R2',
          role: 'code',
          path: 'internal/cart/session.go',
          symbol: 'Expire',
          sources: ['marker', 'trace'],
          lines: [8],
        },
      ],
      tests: [
        {
          ref: 'ACME-SP-0001.R2',
          role: 'tests',
          path: 'web/src/cart.test.ts',
          symbol: 'keeps the cart',
          sources: ['marker'],
          lines: [3],
        },
        {
          ref: 'ACME-SP-0001.R2',
          role: 'tests',
          path: 'internal/cart/cart_test.go',
          symbol: 'TestKeep',
          sources: ['trace'],
        },
      ],
      work: [
        { id: 'ACME-US-0042', kind: 'modifies' },
        { id: 'ACME-US-0041', kind: 'implements' },
      ],
    },
  },
};

function renderDetail({
  withAnalysis = true,
  readOnly = false,
  path = '/p/ACME/specs/ACME-SP-0001/R2',
  project = sampleProject,
}: {
  withAnalysis?: boolean;
  readOnly?: boolean;
  path?: string;
  project?: typeof sampleProject;
} = {}) {
  const provider = new FakeProvider(
    {
      projects: [project],
      items: [...sampleItems, spec],
      requirements: [structuredClone(requirement)],
      ...(withAnalysis ? { specAnalysis: analysis } : {}),
    },
    { readOnly },
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const projectRoute = createRoute({ getParentRoute: () => rootRoute, path: '/p/$project' });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      projectRoute.addChildren([
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'specs/$spec/$req',
          component: RequirementDetail,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'specs',
          component: () => <div data-testid="specs" />,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'items/$id',
          component: () => <div data-testid="item" />,
        }),
      ]),
    ]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </DataProviderProvider>
    </QueryClientProvider>,
  );
  return { provider, router };
}

async function openEditor(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: 'Edit block' }));
  return screen.getByRole('form', { name: 'Edit requirement' });
}

describe('RequirementDetail', () => {
  it('renders the block, status, verified stamp and suspect reasons', async () => {
    renderDetail();

    expect(await screen.findByRole('heading', { level: 1, name: 'Keep the cart' })).toBeVisible();
    expect(await screen.findByText(/the cart SHALL be kept/)).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /Scenario: expired session/ })).toBeInTheDocument();

    expect(screen.getByLabelText<HTMLSelectElement>('Status').value).toBe('in_progress');

    const stampList = screen.getByLabelText('Verification stamp');
    expect(within(stampList).getByText('sha256:oldblock')).toBeInTheDocument();
    // The stamp records another text than the current block rev.
    expect(within(stampList).getByText('older text')).toBeInTheDocument();
    expect(within(stampList).getByText('0123456789ab')).toHaveAttribute(
      'title',
      '0123456789abcdef0123456789abcdef01234567',
    );
    expect(within(stampList).getByText('2026-09-20T10:00:00Z')).toBeInTheDocument();
    expect(within(stampList).getByText('ci')).toBeInTheDocument();

    expect(await screen.findByText('suspect')).toBeInTheDocument();
    const reasons = screen.getByRole('list', { name: 'Coverage reasons' });
    expect(
      within(reasons)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['text', 'stamp']);

    expect(screen.getByRole('link', { name: 'Open in spec' })).toHaveAttribute(
      'href',
      '/p/ACME/items/ACME-SP-0001#acme-sp-0001-r2',
    );
  });

  it('groups code and tests by origin and links the implementing work', async () => {
    renderDetail();

    const code = await screen.findByRole('region', { name: 'Code' });
    expect(
      within(code)
        .getAllByRole('list')
        .map((list) => list.getAttribute('aria-label')),
    ).toEqual(['Code by Marker and trace:', 'Code by Marker', 'Code by trace: entry']);
    const marker = within(code).getByRole('list', { name: 'Code by Marker' });
    const keep = within(marker).getByRole('listitem');
    expect(keep).toHaveAttribute('data-trace-ref', 'internal/cart/cart.go#Keep');
    expect(within(keep).getByText('L12, L40')).toBeInTheDocument();
    const traceOnly = within(code).getByRole('list', { name: 'Code by trace: entry' });
    expect(within(traceOnly).getByText('(file)', { exact: false })).toBeInTheDocument();

    const tests = screen.getByRole('region', { name: 'Tests' });
    const byMarker = within(tests).getByRole('list', { name: 'Tests by Marker' });
    expect(within(byMarker).getByText('pass')).toHaveAttribute('data-result', 'pass');
    const byTrace = within(tests).getByRole('list', { name: 'Tests by trace: entry' });
    expect(within(byTrace).getByText('fail')).toHaveAttribute('data-result', 'fail');

    const work = screen.getByRole('region', { name: 'Work' });
    expect(within(work).getByRole('link', { name: 'ACME-US-0041' })).toHaveAttribute(
      'href',
      '/p/ACME/items/ACME-US-0041',
    );
    const rows = within(work).getAllByRole('listitem');
    expect(rows.map((row) => row.getAttribute('data-work'))).toEqual([
      'ACME-US-0041',
      'ACME-US-0042',
    ]);
    expect(within(rows[0]!).getByText('implemented by')).toBeInTheDocument();
    expect(within(rows[1]!).getByText('modified by')).toBeInTheDocument();
  });

  it('saves only the changed fields under the requirement rev, never the block rev', async () => {
    const user = userEvent.setup();
    const { provider } = renderDetail();
    const update = vi.spyOn(provider, 'updateRequirement');

    const form = await openEditor(user);
    const title = within(form).getByLabelText('Title');
    await user.clear(title);
    await user.type(title, 'Keep the cart across sessions');
    await user.click(within(form).getByRole('button', { name: 'Save block' }));

    await screen.findByRole('heading', { level: 1, name: 'Keep the cart across sessions' });
    expect(update).toHaveBeenCalledTimes(1);
    expect(update).toHaveBeenCalledWith(
      'ACME',
      'ACME-SP-0001.R2',
      { title: 'Keep the cart across sessions' },
      'sha256:req-rev',
    );
    expect(screen.queryByRole('form', { name: 'Edit requirement' })).toBeNull();
  });

  it('shows the per-field diff on a stale revision and reloads theirs', async () => {
    const user = userEvent.setup();
    const { provider } = renderDetail();

    const form = await openEditor(user);
    // Someone else renames the requirement while the edit is open.
    await provider.updateRequirement(
      'ACME',
      'ACME-SP-0001.R2',
      { title: 'Keep the basket' },
      'sha256:req-rev',
    );
    const title = within(form).getByLabelText('Title');
    await user.clear(title);
    await user.type(title, 'Keep the cart forever');
    await user.click(within(form).getByRole('button', { name: 'Save block' }));

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('ACME-SP-0001.R2 changed on disk')).toBeInTheDocument();
    const field = alert.querySelector('[data-conflict-field="title"]') as HTMLElement;
    expect(within(field).getByText('Keep the basket')).toBeInTheDocument();
    expect(within(field).getByText('Keep the cart forever')).toBeInTheDocument();

    await user.click(within(alert).getByRole('button', { name: 'Reload theirs' }));
    await screen.findByRole('heading', { level: 1, name: 'Keep the basket' });
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('form', { name: 'Edit requirement' })).toBeNull();
  });

  it('retries a conflicted save quoting the rev now on disk', async () => {
    const user = userEvent.setup();
    const { provider } = renderDetail();

    const form = await openEditor(user);
    const other = await provider.updateRequirement(
      'ACME',
      'ACME-SP-0001.R2',
      { text: 'The cart SHALL be kept for a day.\n' },
      'sha256:req-rev',
    );
    const text = within(form).getByLabelText('Statement and scenarios');
    await user.clear(text);
    await user.type(text, 'The cart SHALL be kept for a week.');
    const update = vi.spyOn(provider, 'updateRequirement');
    await user.click(within(form).getByRole('button', { name: 'Save block' }));

    const alert = await screen.findByRole('alert');
    const field = alert.querySelector('[data-conflict-field="text"]') as HTMLElement;
    // The core names the text without quoting it; the reload fills in theirs.
    expect(await within(field).findByText('The cart SHALL be kept for a day.')).toBeInTheDocument();
    expect(within(field).getByText('The cart SHALL be kept for a week.')).toBeInTheDocument();

    await user.click(within(alert).getByRole('button', { name: 'Save mine over theirs' }));
    await waitFor(() => {
      expect(screen.queryByRole('alert')).toBeNull();
    });
    expect(update).toHaveBeenLastCalledWith(
      'ACME',
      'ACME-SP-0001.R2',
      { text: 'The cart SHALL be kept for a week.' },
      other.requirement.rev,
    );
    expect(await screen.findByText('The cart SHALL be kept for a week.')).toBeInTheDocument();
  });

  it('moves the status through the declared transitions under the requirement rev', async () => {
    const user = userEvent.setup();
    const { provider } = renderDetail({
      project: {
        ...sampleProject,
        workflow: { transitions: { in_progress: ['in_review', 'cancelled'] } },
      },
    });
    const update = vi.spyOn(provider, 'updateRequirement');

    const select = await screen.findByLabelText<HTMLSelectElement>('Status');
    await waitFor(() => {
      expect([...select.options].map((o) => o.value)).toEqual([
        'in_progress',
        'in_review',
        'cancelled',
      ]);
    });
    await user.selectOptions(select, 'in_review');
    await waitFor(() => {
      expect(update).toHaveBeenCalledWith(
        'ACME',
        'ACME-SP-0001.R2',
        { status: 'in_review' },
        'sha256:req-rev',
      );
    });
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLSelectElement>('Status').value).toBe('in_review');
    });
  });

  it('shows trace and coverage as unavailable in browser-only mode and still edits', async () => {
    const user = userEvent.setup();
    renderDetail({ withAnalysis: false });

    await screen.findByRole('heading', { level: 1, name: 'Keep the cart' });
    expect(await screen.findByText('Trace unavailable.')).toBeInTheDocument();
    expect(await screen.findByText('unavailable')).toHaveAttribute('data-coverage', 'unavailable');
    expect(screen.getAllByText(BROWSER_SPEC_ANALYSIS_REASON)).toHaveLength(2);

    const form = await openEditor(user);
    const title = within(form).getByLabelText('Title');
    await user.clear(title);
    await user.type(title, 'Keep the cart offline');
    await user.click(within(form).getByRole('button', { name: 'Save block' }));
    await screen.findByRole('heading', { level: 1, name: 'Keep the cart offline' });
  });

  it('offers no edit in a read-only workspace', async () => {
    renderDetail({ readOnly: true });

    await screen.findByRole('heading', { level: 1, name: 'Keep the cart' });
    expect(screen.queryByRole('button', { name: 'Edit block' })).toBeNull();
    expect(screen.getByLabelText('Status')).toBeDisabled();
  });

  it('reports a requirement that does not exist', async () => {
    renderDetail({ path: '/p/ACME/specs/ACME-SP-0001/R9' });

    expect(await screen.findByText('ACME-SP-0001.R9 was not found')).toBeInTheDocument();
  });
});
