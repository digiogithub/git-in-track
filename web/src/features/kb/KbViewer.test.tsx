import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useParams,
} from '@tanstack/react-router';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider, samplePages } from '@/api/fake-provider';
import type { DataProvider, KbPage } from '@/api/provider';
import { KbViewer } from '@/features/kb/KbViewer';
import { clearMarkdownCache } from '@/markdown';

// Mermaid never runs in jsdom: the dynamic import resolves to a stub whose
// `render` never settles, so the block stays in its `<pre class="mermaid">`
// fallback — exactly what a user without a rendered diagram sees.
vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn(() => new Promise(() => undefined)),
  },
}));

function ItemStub() {
  const params = useParams({ strict: false });
  return <div data-testid="item-screen">Item {params.id}</div>;
}

function renderKb(path: string, provider: DataProvider = new FakeProvider()) {
  clearMarkdownCache();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });

  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: '/p/$project/kb/$', component: KbViewer }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: '/p/$project/items/$id',
      component: ItemStub,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
  });

  const utils = render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        <RouterProvider router={router} />
      </DataProviderProvider>
    </QueryClientProvider>,
  );
  return { ...utils, provider, router, queryClient };
}

const richPage: KbPage = {
  path: 'docs/guides/handbook.md',
  title: 'Handbook',
  frontMatter: { title: 'Handbook', tags: ['guide', 'onboarding'], owner: 'marta' },
  body: [
    '# Handbook',
    '',
    '## Getting started',
    '',
    '- [x] Read [[architecture/overview]]',
    '- [ ] Open [[ACME-US-0042|the SSO story]]',
    '',
    '## Reference',
    '',
    '> [!WARNING]',
    '> Handle credentials carefully.',
    '',
    '```ts',
    'const answer = 42;',
    '```',
  ].join('\n'),
  rev: 'sha256:00000000000000b1',
  outgoing: ['docs/architecture/overview.md', 'ACME-US-0042'],
  backlinks: ['docs/index.md', 'ACME-US-0042'],
};

describe('KbViewer', () => {
  it('renders the requested page and its file tree', async () => {
    renderKb('/p/ACME/kb/docs/index.md');

    expect(await screen.findByRole('heading', { name: 'ACME Platform', level: 1 })).toBeVisible();

    const tree = screen.getByRole('navigation', { name: /knowledge base pages/i });
    expect(within(tree).getByRole('link', { name: /acme platform/i })).toHaveAttribute(
      'aria-current',
      'page',
    );
    // Folders start collapsed unless they contain the current page.
    expect(within(tree).getByRole('button', { name: 'architecture' })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
    expect(within(tree).queryByRole('link', { name: /architecture overview/i })).toBeNull();
  });

  it('opens the scope index page when the route carries no path', async () => {
    renderKb('/p/ACME/kb/');
    expect(await screen.findByRole('heading', { name: 'ACME Platform', level: 1 })).toBeVisible();
  });

  it('navigates to another page from the tree', async () => {
    const user = userEvent.setup();
    renderKb('/p/ACME/kb/docs/index.md');

    const tree = await screen.findByRole('navigation', { name: /knowledge base pages/i });
    await user.click(within(tree).getByRole('button', { name: 'architecture' }));
    await user.click(within(tree).getByRole('link', { name: /architecture overview/i }));

    expect(
      await screen.findByRole('heading', { name: 'Architecture overview', level: 1 }),
    ).toBeVisible();
    // The GFM table of that page renders too.
    expect(await screen.findByRole('table')).toBeVisible();
  });

  it('resolves a wikilink to another page and follows it', async () => {
    const user = userEvent.setup();
    renderKb('/p/ACME/kb/docs/index.md');

    const link = await screen.findByRole('link', { name: 'architecture/overview' });
    expect(link).toHaveAttribute('href', '/p/ACME/kb/docs/architecture/overview.md');

    await user.click(link);
    expect(
      await screen.findByRole('heading', { name: 'Architecture overview', level: 1 }),
    ).toBeVisible();
  });

  it('resolves a wikilink to a backlog item and follows it', async () => {
    const user = userEvent.setup();
    renderKb('/p/ACME/kb/docs/index.md');

    const link = await screen.findByRole('link', { name: 'ACME-US-0042' });
    expect(link).toHaveAttribute('href', '/p/ACME/items/ACME-US-0042');

    await user.click(link);
    expect(await screen.findByTestId('item-screen')).toHaveTextContent('ACME-US-0042');
  });

  it('leaves a mermaid fence as a placeholder holding its source', async () => {
    const { container } = renderKb('/p/ACME/kb/docs/index.md');

    await waitFor(() => {
      expect(container.querySelector('pre.mermaid')).not.toBeNull();
    });
    expect(container.querySelector('pre.mermaid')).toHaveTextContent('graph TD; A-->B;');
  });

  it('shows front matter, the outline, callouts, task lists and backlinks', async () => {
    renderKb('/p/ACME/kb/docs/guides/handbook.md', new FakeProvider({ pages: [richPage] }));

    // Wait for the Markdown itself, not just the page header. The generous
    // timeout covers the first load of the Shiki chunk: this page has a fence.
    expect(
      await screen.findByRole('heading', { name: 'Getting started', level: 2 }, { timeout: 10_000 }),
    ).toBeVisible();

    // Front matter card.
    expect(screen.getByText('tags')).toBeVisible();
    expect(screen.getByText('onboarding')).toBeVisible();

    // Outline.
    const toc = screen.getByRole('navigation', { name: /on this page/i });
    expect(within(toc).getByRole('link', { name: 'Getting started' })).toHaveAttribute(
      'href',
      '#getting-started',
    );

    // GFM task list and the alias of an item wikilink.
    expect(screen.getAllByRole('checkbox')[0]).toBeChecked();
    expect(screen.getByRole('link', { name: 'the SSO story' })).toHaveAttribute(
      'href',
      '/p/ACME/items/ACME-US-0042',
    );

    // Callout.
    expect(screen.getByText('Handle credentials carefully.')).toBeVisible();

    // Backlinks panel.
    const backlinks = screen.getByRole('region', { name: /backlinks/i });
    expect(within(backlinks).getByRole('link', { name: 'docs/index.md' })).toBeVisible();
  });

  it('toggles the raw Markdown source', async () => {
    const user = userEvent.setup();
    renderKb('/p/ACME/kb/docs/architecture/overview.md');

    await screen.findByRole('heading', { name: 'Architecture overview', level: 1 });
    expect(screen.queryByText(/> \[!NOTE\]/)).toBeNull();

    await user.click(screen.getByRole('button', { name: /open raw/i }));

    expect(screen.getByText(/> \[!NOTE\]/)).toBeVisible();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('shows a 404 state with a create hint for a page that does not exist', async () => {
    renderKb('/p/ACME/kb/docs/ghost.md');

    expect(await screen.findByRole('heading', { name: /page not found/i })).toBeVisible();
    expect(screen.getByText(/create the file to start the page/i)).toBeVisible();
  });

  it('refetches the page when the provider reports a kb change', async () => {
    const provider = new FakeProvider({ pages: samplePages });
    renderKb('/p/ACME/kb/docs/index.md', provider);

    await screen.findByRole('heading', { name: 'ACME Platform', level: 1 });

    await provider.writePage(
      { kind: 'project', projectKey: 'ACME' },
      'docs/index.md',
      '# Rewritten by someone else\n',
    );

    expect(
      await screen.findByRole('heading', { name: 'Rewritten by someone else', level: 1 }),
    ).toBeVisible();
  });

  it('does not offer an Edit link while the edit route does not exist', async () => {
    renderKb('/p/ACME/kb/docs/index.md');
    await screen.findByRole('heading', { name: 'ACME Platform', level: 1 });
    expect(screen.queryByRole('link', { name: 'Edit' })).toBeNull();
  });
});
