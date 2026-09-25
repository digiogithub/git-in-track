import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { configure, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider, sampleItems, type FakeSpecAnalysis } from '@/api/fake-provider';
import type { CoverageRow, Item, Requirement } from '@/api/provider';
import { BROWSER_SPEC_ANALYSIS_REASON } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { CoverageMatrixPage, MATRIX_ROW_HEIGHT } from '@/features/specs/CoverageMatrix';
import { validateMatrixSearch } from '@/features/specs/matrix';

configure({ asyncUtilTimeout: 5_000 });

function spec(id: string, title: string): Item {
  return {
    id,
    type: 'spec',
    title,
    status: 'todo',
    body: '## Requirements\n',
    path: `docs/.pmngr/specs/${id}.md`,
    rev: `sha256:${id}`,
  };
}

function requirement(ref: string, title: string): Requirement {
  const [specId = ''] = ref.split('.');
  return {
    ref,
    spec: specId,
    path: `docs/.pmngr/specs/${specId}.md`,
    anchor: ref.toLowerCase().replace('.', '-'),
    line: 5,
    title,
    status: 'todo',
    rev: `sha256:r-${ref}`,
    blockRev: `sha256:b-${ref}`,
  };
}

const specs = [spec('ACME-SP-0001', 'Checkout'), spec('ACME-SP-0002', 'Item ID allocation')];

const requirements = [
  requirement('ACME-SP-0001.R1', 'Trim input'),
  requirement('ACME-SP-0001.R2', 'Keep the cart'),
  requirement('ACME-SP-0001.R3', 'Refuse an empty cart'),
  requirement('ACME-SP-0002.R1', 'IDs are never reused'),
];

const coverage: CoverageRow[] = [
  {
    ref: 'ACME-SP-0001.R1',
    status: 'passing',
    reasons: ['results'],
    tests: [{ test: 'internal/cart_test.go#TestTrim', result: 'pass' }],
  },
  {
    ref: 'ACME-SP-0001.R2',
    status: 'failing',
    reasons: ['failed', 'results'],
    tests: [
      { test: 'internal/cart_test.go#TestKeep', result: 'fail' },
      { test: 'web/cart.test.ts#keeps the cart', result: 'skip' },
    ],
  },
  {
    ref: 'ACME-SP-0002.R1',
    status: 'suspect',
    reasons: ['code:internal/core/id.go#NextID', 'results'],
    tests: [{ test: 'internal/core/id_test.go#TestNextID', result: 'missing' }],
  },
];

function renderMatrix(
  path: string,
  analysis: FakeSpecAnalysis | null = { coverage },
  data: { requirements?: Requirement[]; items?: Item[] } = {},
) {
  const provider = new FakeProvider({
    items: [...sampleItems, ...(data.items ?? specs)],
    requirements: data.requirements ?? requirements,
    ...(analysis ? { specAnalysis: analysis } : {}),
  });
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
          path: 'specs/coverage',
          validateSearch: validateMatrixSearch,
          component: CoverageMatrixPage,
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
  return { router };
}

function bodyRow(ref: string): HTMLElement {
  const row = document.querySelector(`tr[data-ref="${ref}"]`);
  if (!(row instanceof HTMLElement)) throw new Error(`no row ${ref}`);
  return row;
}

function renderedRefs(): string[] {
  return [...document.querySelectorAll('tr[data-ref]')].map(
    (row) => row.getAttribute('data-ref') ?? '',
  );
}

describe('CoverageMatrixPage', () => {
  it('renders one row per requirement with its computed status and reasons', async () => {
    renderMatrix('/p/ACME/specs/coverage');

    await screen.findByRole('table', { name: 'Requirement coverage' });
    expect(renderedRefs()).toEqual([
      'ACME-SP-0001.R1',
      'ACME-SP-0001.R2',
      'ACME-SP-0001.R3',
      'ACME-SP-0002.R1',
    ]);

    const failing = bodyRow('ACME-SP-0001.R2');
    expect(within(failing).getByRole('rowheader')).toHaveTextContent('Keep the cart');
    expect(within(failing).getByText('failing')).toBeInTheDocument();
    expect(within(failing).getByText('failed · results')).toHaveAttribute(
      'title',
      'Reasons: failed, results',
    );
    // A requirement the coverage answer does not name is untested.
    expect(within(bodyRow('ACME-SP-0001.R3')).getByText('untested')).toBeInTheDocument();
    expect(within(bodyRow('ACME-SP-0002.R1')).getByText('suspect')).toBeInTheDocument();

    // The ref opens the requirement detail; the block anchor stays a secondary link.
    expect(within(failing).getByRole('link', { name: 'ACME-SP-0001.R2' })).toHaveAttribute(
      'href',
      '/p/ACME/specs/ACME-SP-0001/R2',
    );
    expect(
      within(failing).getByRole('link', { name: 'Open ACME-SP-0001.R2 in spec' }),
    ).toHaveAttribute('href', '/p/ACME/items/ACME-SP-0001#acme-sp-0001-r2');
  });

  it('groups the test columns by file and shows each last result in words', async () => {
    renderMatrix('/p/ACME/specs/coverage');

    const table = await screen.findByRole('table', { name: 'Requirement coverage' });
    const files = within(table)
      .getAllByRole('columnheader')
      .filter((th) => th.hasAttribute('data-file'));
    expect(files.map((th) => [th.textContent, th.getAttribute('colspan')])).toEqual([
      ['internal/cart_test.go', '2'],
      ['internal/core/id_test.go', '1'],
      ['web/cart.test.ts', '1'],
    ]);
    const tests = within(table)
      .getAllByRole('columnheader')
      .filter((th) => th.hasAttribute('data-test'))
      .map((th) => th.textContent);
    expect(tests).toEqual(['TestKeep', 'TestTrim', 'TestNextID', 'keeps the cart']);

    const cells = (ref: string) =>
      [...bodyRow(ref).querySelectorAll('td')].map(
        (td) => td.querySelector('[data-result]')?.getAttribute('data-result') ?? '',
      );
    expect(cells('ACME-SP-0001.R2')).toEqual(['fail', '', '', 'skip']);
    expect(cells('ACME-SP-0001.R1')).toEqual(['', 'pass', '', '']);
    expect(cells('ACME-SP-0002.R1')).toEqual(['', '', 'missing', '']);
    expect(within(bodyRow('ACME-SP-0001.R2')).getByText('fail')).toBeInTheDocument();
    expect(within(bodyRow('ACME-SP-0002.R1')).getByText('no result')).toBeInTheDocument();
  });

  it('summarises counts per status and per spec', async () => {
    renderMatrix('/p/ACME/specs/coverage');

    await screen.findByRole('table', { name: 'Requirement coverage' });
    const statusGroup = screen.getByRole('group', { name: 'Filter by status' });
    expect(within(statusGroup).getByRole('button', { name: 'failing (1)' })).toBeInTheDocument();
    expect(within(statusGroup).getByRole('button', { name: 'untested (1)' })).toBeInTheDocument();
    expect(within(statusGroup).getByRole('button', { name: 'passing (1)' })).toBeInTheDocument();

    const summary = screen.getByRole('table', { name: 'Coverage by spec' });
    const checkout = summary.querySelector('tr[data-spec="ACME-SP-0001"]') as HTMLElement;
    expect([...checkout.querySelectorAll('td')].map((td) => td.textContent)).toEqual([
      '3',
      '1',
      '1',
      '1',
      '0',
    ]);
  });

  it('filters by spec and status from the URL', async () => {
    renderMatrix('/p/ACME/specs/coverage?spec=ACME-SP-0001&status=failing,untested');

    await screen.findByRole('table', { name: 'Requirement coverage' });
    await waitFor(() => {
      expect(renderedRefs()).toEqual(['ACME-SP-0001.R2', 'ACME-SP-0001.R3']);
    });
    // Only the tests of the rows kept are columns.
    const tests = [...document.querySelectorAll('th[data-test]')].map((th) => th.textContent);
    expect(tests).toEqual(['TestKeep', 'keeps the cart']);
  });

  it('writes the filters into the URL', async () => {
    const user = userEvent.setup();
    const { router } = renderMatrix('/p/ACME/specs/coverage');

    await screen.findByRole('table', { name: 'Requirement coverage' });
    const specGroup = screen.getByRole('group', { name: 'Filter by spec' });
    await user.click(within(specGroup).getByRole('button', { name: 'ACME-SP-0002' }));
    await waitFor(() => {
      expect(router.state.location.search).toEqual({ spec: ['ACME-SP-0002'] });
    });
    expect(renderedRefs()).toEqual(['ACME-SP-0002.R1']);

    const statusGroup = screen.getByRole('group', { name: 'Filter by status' });
    await user.click(within(statusGroup).getByRole('button', { name: /^failing/ }));
    await screen.findByText('No requirement matches');
    await user.click(screen.getByRole('button', { name: 'Clear filters' }));
    await waitFor(() => {
      expect(renderedRefs()).toHaveLength(4);
    });
  });

  it('shows unavailable with a hint to run the companion in browser-only mode', async () => {
    renderMatrix('/p/ACME/specs/coverage', null);

    expect(await screen.findByText('Coverage unavailable')).toBeInTheDocument();
    expect(screen.getByText(BROWSER_SPEC_ANALYSIS_REASON)).toBeInTheDocument();
    expect(screen.getByText('gintrack serve')).toBeInTheDocument();
    expect(screen.queryByRole('table', { name: 'Requirement coverage' })).toBeNull();
  });

  it('windows a large matrix: a bounded number of rows, moving with the scroll', async () => {
    const many = Array.from({ length: 2_000 }, (_, i) =>
      requirement(`ACME-SP-0001.R${i + 1}`, `Requirement ${i + 1}`),
    );
    const rows: CoverageRow[] = many.map((r, i) => ({
      ref: r.ref,
      status: i % 2 === 0 ? 'passing' : 'failing',
      tests: [
        { test: `internal/big_test.go#Test${i % 50}`, result: i % 2 === 0 ? 'pass' : 'fail' },
      ],
    }));
    renderMatrix('/p/ACME/specs/coverage', { coverage: rows }, { requirements: many });

    const table = await screen.findByRole('table', { name: 'Requirement coverage' });
    expect(table).toHaveAttribute('aria-rowcount', '2002');
    await waitFor(() => {
      expect(renderedRefs().length).toBeGreaterThan(0);
    });
    expect(renderedRefs().length).toBeLessThanOrEqual(40);
    expect(renderedRefs()[0]).toBe('ACME-SP-0001.R1');

    const viewport = screen.getByTestId('coverage-matrix-viewport');
    viewport.scrollTop = 64 + MATRIX_ROW_HEIGHT * 1_000;
    fireEvent.scroll(viewport);
    await waitFor(() => {
      expect(renderedRefs()).toContain('ACME-SP-0001.R1001');
    });
    expect(renderedRefs()).not.toContain('ACME-SP-0001.R1');
    expect(renderedRefs().length).toBeLessThanOrEqual(40);
    expect(bodyRow('ACME-SP-0001.R1001')).toHaveAttribute('aria-rowindex', '1003');
  });
});
