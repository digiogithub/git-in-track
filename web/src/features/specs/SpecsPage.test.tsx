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
  type FakeData,
  type FakeSpecAnalysis,
} from '@/api/fake-provider';
import type { Item, Requirement } from '@/api/provider';
import { BROWSER_SPEC_ANALYSIS_REASON } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { validateSpecSearch } from '@/features/specs/search';
import { SpecsPage } from '@/features/specs/SpecsPage';

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

function requirement(ref: string, title: string, status: string): Requirement {
  const [specId = ''] = ref.split('.');
  return {
    ref,
    spec: specId,
    path: `docs/.pmngr/specs/${specId}.md`,
    anchor: ref.toLowerCase().replace('.', '-'),
    line: 5,
    title,
    status,
    rev: `sha256:r-${ref}`,
    blockRev: `sha256:b-${ref}`,
  };
}

const specs = [spec('ACME-SP-0001', 'Checkout'), spec('ACME-SP-0002', 'Item ID allocation')];

const requirements = [
  requirement('ACME-SP-0001.R2', 'Keep the cart', 'in_progress'),
  requirement('ACME-SP-0001.R1', 'Trim input', 'done'),
  requirement('ACME-SP-0001.R10', 'Refuse an empty cart', 'todo'),
  requirement('ACME-SP-0002.R1', 'IDs are never reused', 'done'),
];

const analysis: FakeSpecAnalysis = {
  coverage: [
    { ref: 'ACME-SP-0001.R1', status: 'passing' },
    { ref: 'ACME-SP-0001.R2', status: 'failing' },
    { ref: 'ACME-SP-0002.R1', status: 'suspect' },
  ],
};

function renderSpecs(
  path: string,
  withAnalysis = true,
  readOnly = false,
  extra: Partial<FakeData> = {},
) {
  const provider = new FakeProvider(
    {
      items: [...sampleItems, ...specs],
      requirements,
      ...(withAnalysis ? { specAnalysis: analysis } : {}),
      ...extra,
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
          path: 'specs',
          validateSearch: validateSpecSearch,
          component: SpecsPage,
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
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'items/new',
          component: () => <div data-testid="new-item" />,
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

function refsOf(specId: string): string[] {
  const list = screen.getByRole('list', { name: `Requirements of ${specId}` });
  return within(list)
    .getAllByRole('listitem')
    .map((row) => row.getAttribute('data-ref') ?? '');
}

describe('SpecsPage', () => {
  it('groups requirements under their spec, one row each in ref order', async () => {
    renderSpecs('/p/ACME/specs');

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0001' });
    expect(refsOf('ACME-SP-0001')).toEqual([
      'ACME-SP-0001.R1',
      'ACME-SP-0001.R2',
      'ACME-SP-0001.R10',
    ]);
    expect(refsOf('ACME-SP-0002')).toEqual(['ACME-SP-0002.R1']);

    const row = screen
      .getByRole('list', { name: 'Requirements of ACME-SP-0001' })
      .querySelector('[data-ref="ACME-SP-0001.R2"]') as HTMLElement;
    expect(within(row).getByText('Keep the cart')).toBeInTheDocument();
    expect(within(row).getByText('In Progress')).toBeInTheDocument();
    expect(await within(row).findByText('failing')).toBeInTheDocument();
    // A requirement the coverage answer does not name is untested.
    const r10 = screen
      .getByRole('list', { name: 'Requirements of ACME-SP-0001' })
      .querySelector('[data-ref="ACME-SP-0001.R10"]') as HTMLElement;
    expect(within(r10).getByText('untested')).toBeInTheDocument();

    // The ref opens the requirement detail; the block anchor stays a secondary link.
    expect(within(row).getByRole('link', { name: 'ACME-SP-0001.R2' })).toHaveAttribute(
      'href',
      '/p/ACME/specs/ACME-SP-0001/R2',
    );
    expect(within(row).getByRole('link', { name: 'Open ACME-SP-0001.R2 in spec' })).toHaveAttribute(
      'href',
      '/p/ACME/items/ACME-SP-0001#acme-sp-0001-r2',
    );
  });

  it('collapses a spec group', async () => {
    const user = userEvent.setup();
    renderSpecs('/p/ACME/specs');

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0002' });
    await user.click(screen.getByRole('button', { name: 'Item ID allocation' }));
    expect(screen.queryByRole('list', { name: 'Requirements of ACME-SP-0002' })).toBeNull();
    expect(screen.getByRole('list', { name: 'Requirements of ACME-SP-0001' })).toBeInTheDocument();
  });

  it('filters by status and coverage from the URL', async () => {
    renderSpecs('/p/ACME/specs?status=done&coverage=suspect');

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0002' });
    await waitFor(() => {
      expect(screen.queryByRole('list', { name: 'Requirements of ACME-SP-0001' })).toBeNull();
    });
    expect(refsOf('ACME-SP-0002')).toEqual(['ACME-SP-0002.R1']);
    expect(screen.getByText('1 of 1 requirement')).toBeInTheDocument();
  });

  it('writes a status filter into the URL', async () => {
    const user = userEvent.setup();
    const { router } = renderSpecs('/p/ACME/specs');

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0001' });
    const statusGroup = screen.getByRole('group', { name: 'Filter by status' });
    await user.click(within(statusGroup).getByRole('button', { name: 'To Do' }));

    await waitFor(() => {
      expect(router.state.location.search).toEqual({ status: ['todo'] });
    });
    expect(refsOf('ACME-SP-0001')).toEqual(['ACME-SP-0001.R10']);
    expect(screen.queryByRole('list', { name: 'Requirements of ACME-SP-0002' })).toBeNull();

    const coverageGroup = screen.getByRole('group', { name: 'Filter by coverage' });
    await user.click(within(coverageGroup).getByRole('button', { name: /failing/ }));
    await screen.findByText('No requirement matches');
  });

  it('shows unavailable coverage in browser-only mode and keeps the rows', async () => {
    renderSpecs('/p/ACME/specs?coverage=failing', false);

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0001' });
    expect(await screen.findByText(BROWSER_SPEC_ANALYSIS_REASON)).toBeInTheDocument();
    // Every row is still listed: a coverage filter cannot apply without coverage.
    expect(refsOf('ACME-SP-0001')).toHaveLength(3);
    const badges = document.querySelectorAll('[data-coverage="unavailable"]');
    expect(badges).toHaveLength(4);
    const coverageGroup = screen.getByRole('group', { name: 'Filter by coverage' });
    for (const chip of within(coverageGroup).getAllByRole('button')) {
      expect(chip).toBeDisabled();
    }
  });

  it('adds a requirement through createRequirement with the EARS template', async () => {
    const user = userEvent.setup();
    const { provider } = renderSpecs('/p/ACME/specs');

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0002' });
    expect(screen.getByRole('link', { name: 'New spec' }).getAttribute('href')).toContain(
      'type=spec',
    );

    await user.click(screen.getByRole('button', { name: 'Add requirement to ACME-SP-0002' }));
    const dialog = await screen.findByRole('dialog');
    const text = within(dialog).getByLabelText<HTMLTextAreaElement>('Statement and scenarios');
    expect(text.value).toContain('SHALL');
    expect(text.value).toContain('#### Scenario:');
    await user.type(within(dialog).getByLabelText('Title'), 'Allocate by index scan');
    await user.click(within(dialog).getByRole('button', { name: 'Add requirement' }));

    await waitFor(() => {
      expect(refsOf('ACME-SP-0002')).toEqual(['ACME-SP-0002.R1', 'ACME-SP-0002.R2']);
    });
    const { requirement: created } = await provider.getRequirement('ACME', 'ACME-SP-0002.R2');
    expect(created.title).toBe('Allocate by index scan');
  });

  it("prefills the dialog with the project's requirement template override", async () => {
    const user = userEvent.setup();
    const custom = 'The <system> SHALL <response>.\n\n#### Scenario: <name>\n- **GIVEN** <context>\n';
    renderSpecs('/p/ACME/specs', true, false, {
      specTemplates: {
        requirement: custom,
        requirementSource: 'docs/.pmngr/templates/requirement.md',
      },
    });

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0002' });
    await user.click(screen.getByRole('button', { name: 'Add requirement to ACME-SP-0002' }));
    const dialog = await screen.findByRole('dialog');
    const text = within(dialog).getByLabelText<HTMLTextAreaElement>('Statement and scenarios');
    await waitFor(() => {
      expect(text.value).toBe(custom);
    });
  });

  it('shows similar requirements after a create as a non-blocking hint', async () => {
    const user = userEvent.setup();
    const { provider } = renderSpecs('/p/ACME/specs');
    const create = provider.createRequirement.bind(provider);
    vi.spyOn(provider, 'createRequirement').mockImplementation(async (project, draft) => ({
      ...(await create(project, draft)),
      similar: [
        { ref: 'ACME-SP-0002.R1', title: 'IDs are never reused', spec: 'ACME-SP-0002', score: 0.8 },
      ],
    }));

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0002' });
    await user.click(screen.getByRole('button', { name: 'Add requirement to ACME-SP-0002' }));
    const dialog = await screen.findByRole('dialog');
    await user.type(within(dialog).getByLabelText('Title'), 'Never reuse an id');
    await user.click(within(dialog).getByRole('button', { name: 'Add requirement' }));

    expect(await screen.findByText('Similar requirements exist')).toBeInTheDocument();
    expect(screen.getByText('ACME-SP-0002.R1 — IDs are never reused')).toBeInTheDocument();
    await waitFor(() => {
      expect(refsOf('ACME-SP-0002')).toEqual(['ACME-SP-0002.R1', 'ACME-SP-0002.R2']);
    });
  });

  it('offers no create actions in a read-only workspace', async () => {
    renderSpecs('/p/ACME/specs', true, true);

    await screen.findByRole('list', { name: 'Requirements of ACME-SP-0001' });
    expect(screen.queryByRole('link', { name: 'New spec' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Add requirement/ })).toBeNull();
  });
});
