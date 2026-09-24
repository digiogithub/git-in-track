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
import { FakeProvider, sampleItems } from '@/api/fake-provider';
import type { ImpactResult } from '@/api/provider';
import { BROWSER_SPEC_ANALYSIS_REASON } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { validateImpactSearch } from '@/features/specs/impact';
import { ImpactView } from '@/features/specs/ImpactView';

configure({ asyncUtilTimeout: 5_000 });

const impact: ImpactResult = {
  base: 'main',
  files: 3,
  symbols: 4,
  tiers: [
    { tier: 1, status: 'ok', hits: 1 },
    { tier: 2, status: 'ok', hits: 1 },
    { tier: 3, status: 'ok', hits: 1 },
  ],
  hits: [
    {
      ref: 'ACME-SP-0001.R1',
      title: 'Trim input',
      tier: 1,
      status: 'passing',
      suspect: true,
      reasons: ['symbol:internal/cart.go#Trim'],
      pending: ['ACME-US-0007'],
    },
    {
      ref: 'ACME-SP-0001.R2',
      title: 'Keep the cart',
      tier: 2,
      status: 'failing',
      reasons: ['call:internal/cart.go#Keep calls Trim d2'],
    },
    {
      ref: 'ACME-SP-0002.R1',
      title: 'IDs are never reused',
      tier: 3,
      candidate: true,
      score: 0.812,
      status: 'untested',
      reasons: ['semantic'],
    },
  ],
};

function renderImpact(path: string, analysis: { impact?: ImpactResult } | null = { impact }) {
  const provider = new FakeProvider({
    items: sampleItems,
    ...(analysis ? { specAnalysis: analysis } : {}),
  });
  const spy = vi.spyOn(provider, 'queryImpact');
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
          path: 'specs/impact',
          validateSearch: validateImpactSearch,
          component: ImpactView,
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
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'specs/$spec/$req',
          component: () => <div data-testid="requirement" />,
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
  return { router, spy };
}

function tier(n: 1 | 2 | 3): HTMLElement {
  const section = document.querySelector(`section[data-tier="${n}"]`);
  if (!(section instanceof HTMLElement)) throw new Error(`no tier ${n}`);
  return section;
}

function hit(ref: string): HTMLElement {
  const row = document.querySelector(`li[data-ref="${ref}"]`);
  if (!(row instanceof HTMLElement)) throw new Error(`no hit ${ref}`);
  return row;
}

describe('ImpactView', () => {
  it('groups hits by tier with reasons, coverage, suspect and pending items', async () => {
    const { spy } = renderImpact('/p/ACME/specs/impact');

    await screen.findByText('main..worktree');
    // The default range: base main, head the working tree (no head sent).
    expect(spy).toHaveBeenCalledWith('ACME', { base: 'main' });

    expect(within(tier(1)).getByText('Trim input')).toBeInTheDocument();
    expect(within(tier(2)).getByText('Keep the cart')).toBeInTheDocument();
    expect(within(tier(3)).getByText('IDs are never reused')).toBeInTheDocument();

    const direct = hit('ACME-SP-0001.R1');
    expect(within(direct).getByText('passing')).toBeInTheDocument();
    expect(within(direct).getByText('suspect')).toBeInTheDocument();
    expect(within(direct).getByText('internal/cart.go#Trim')).toBeInTheDocument();
    expect(within(direct).getByRole('link', { name: /ACME-US-0007/ })).toHaveAttribute(
      'href',
      '/p/ACME/items/ACME-US-0007',
    );

    // The call reason of a transitive hit, read aloud with its depth.
    const transitive = hit('ACME-SP-0001.R2');
    expect(within(transitive).getByText('called from')).toBeInTheDocument();
    expect(
      within(transitive).getByText('internal/cart.go#Keep → Trim, depth 2').closest('li'),
    ).toHaveAttribute('title', 'call:internal/cart.go#Keep calls Trim d2');
    expect(within(transitive).getByText('failing')).toBeInTheDocument();
    expect(within(transitive).queryByText('suspect')).not.toBeInTheDocument();
  });

  it('draws candidates apart from certain hits, with their score', async () => {
    renderImpact('/p/ACME/specs/impact');
    await screen.findByText('IDs are never reused');

    const candidate = hit('ACME-SP-0002.R1');
    expect(candidate).toHaveAttribute('data-candidate', 'true');
    expect(candidate.className).toContain('border-dashed');
    expect(within(candidate).getByText(/candidate · score 0\.812/)).toBeInTheDocument();

    for (const ref of ['ACME-SP-0001.R1', 'ACME-SP-0001.R2']) {
      expect(hit(ref)).not.toHaveAttribute('data-candidate');
      expect(hit(ref).className).not.toContain('border-dashed');
      expect(within(hit(ref)).queryByText(/candidate/)).not.toBeInTheDocument();
    }
    expect(within(tier(1)).queryByText('IDs are never reused')).not.toBeInTheDocument();
    expect(within(tier(2)).queryByText('IDs are never reused')).not.toBeInTheDocument();
  });

  it('links every hit to its requirement detail', async () => {
    renderImpact('/p/ACME/specs/impact');
    await screen.findByText('Trim input');

    expect(screen.getByRole('link', { name: 'ACME-SP-0001.R1' })).toHaveAttribute(
      'href',
      '/p/ACME/specs/ACME-SP-0001/R1',
    );
    expect(screen.getByRole('link', { name: 'ACME-SP-0002.R1' })).toHaveAttribute(
      'href',
      '/p/ACME/specs/ACME-SP-0002/R1',
    );
  });

  it('reads the range from the URL and writes a new one back', async () => {
    const { router, spy } = renderImpact('/p/ACME/specs/impact?base=v1.0&head=HEAD~1');
    await screen.findByText('v1.0..HEAD~1');
    expect(spy).toHaveBeenCalledWith('ACME', { base: 'v1.0', head: 'HEAD~1' });

    const user = userEvent.setup();
    const base = screen.getByLabelText('Base');
    const head = screen.getByLabelText('Head');
    expect(base).toHaveValue('v1.0');
    expect(head).toHaveValue('HEAD~1');
    await user.clear(base);
    await user.type(base, 'release');
    await user.clear(head);
    await user.type(head, 'worktree');
    await user.click(screen.getByRole('button', { name: 'Show impact' }));

    await waitFor(() => expect(router.state.location.search).toEqual({ base: 'release' }));
    await screen.findByText('release..worktree');
    expect(spy).toHaveBeenLastCalledWith('ACME', { base: 'release' });
  });

  it('reports tiers 2 and 3 unavailable without Pando, keeping tier 1', async () => {
    renderImpact('/p/ACME/specs/impact', {
      impact: {
        ...impact,
        tiers: [
          { tier: 1, status: 'ok', hits: 1 },
          { tier: 2, status: 'unavailable', hits: 0, message: 'Pando is not configured' },
          { tier: 3, status: 'error', hits: 0, message: 'project not indexed' },
        ],
        hits: impact.hits.filter((h) => h.tier === 1),
      },
    });
    await screen.findByText('Trim input');

    expect(within(tier(1)).queryByRole('status')).not.toBeInTheDocument();
    const t2 = within(tier(2)).getByRole('status');
    expect(t2).toHaveAttribute('data-tier-status', 'unavailable');
    expect(t2).toHaveTextContent('Pando is not configured');
    const t3 = within(tier(3)).getByRole('status');
    expect(t3).toHaveAttribute('data-tier-status', 'error');
    expect(t3).toHaveTextContent('project not indexed');
    expect(within(tier(2)).queryByRole('list')).not.toBeInTheDocument();
  });

  it('renders the whole view unavailable in browser-only mode', async () => {
    renderImpact('/p/ACME/specs/impact', null);

    await screen.findByText('Impact unavailable');
    expect(screen.getByText(BROWSER_SPEC_ANALYSIS_REASON)).toBeInTheDocument();
    expect(screen.getByText('gintrack serve')).toBeInTheDocument();
    expect(screen.queryByRole('form', { name: 'Diff range' })).not.toBeInTheDocument();
    expect(document.querySelector('section[data-tier]')).toBeNull();
  });
});
