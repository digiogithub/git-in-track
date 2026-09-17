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
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider } from '@/api/fake-provider';
import type { SearchHit } from '@/api/provider';
import { ProjectLayout } from '@/app/layout/ProjectLayout';
import { useUiPrefs } from '@/app/ui-prefs';

const hits: SearchHit[] = [
  {
    kind: 'item',
    id: 'ACME-US-0042',
    path: 'docs/.pmngr/stories/ACME-US-0042-login-with-sso.md',
    title: 'Login with SSO',
    score: 3,
    source: 'core',
  },
  {
    kind: 'page',
    path: 'docs/guides/login.md',
    title: 'Login guide',
    score: 1,
    source: 'core',
  },
];

function Passthrough() {
  return <Outlet />;
}

/** A page with something focusable, so focus restoration has a target. */
function ItemsPage() {
  return <button type="button">Backlog action</button>;
}

function ItemPage() {
  const { id } = useParams({ strict: false });
  return <p>Item page {id}</p>;
}

function KbPage() {
  const params = useParams({ strict: false });
  return <p>KB page {params._splat}</p>;
}

/** The project slice of the real route tree, with the real project layout. */
function renderApp(path: string, provider: FakeProvider) {
  const rootRoute = createRootRoute({ component: Passthrough });
  const projectRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/p/$project',
    component: ProjectLayout,
  });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: '/', component: ItemsPage }),
    projectRoute.addChildren([
      createRoute({ getParentRoute: () => projectRoute, path: 'items', component: ItemsPage }),
      createRoute({ getParentRoute: () => projectRoute, path: 'items/$id', component: ItemPage }),
      createRoute({ getParentRoute: () => projectRoute, path: 'kb/$', component: KbPage }),
    ]),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        <RouterProvider router={router} />
      </DataProviderProvider>
    </QueryClientProvider>,
  );
}

const shortcut = '{Control>}{Shift>}F{/Shift}{/Control}';

async function openOverlay(user: ReturnType<typeof userEvent.setup>) {
  await user.keyboard(shortcut);
  return screen.findByRole('dialog', { name: /search acme/i });
}

describe('ProjectSearchOverlay', () => {
  beforeEach(() => {
    globalThis.localStorage.clear();
    useUiPrefs.getState().setSemanticResults(true);
  });

  it('opens on Ctrl+Shift+F in a project route and scopes the query to it', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider({ repos: [] });
    const search = vi.spyOn(provider, 'search').mockResolvedValue({ hits });
    renderApp('/p/ACME/items', provider);
    await screen.findByRole('button', { name: 'Backlog action' });

    const dialog = await openOverlay(user);
    const input = within(dialog).getByRole('combobox', { name: /search this project/i });
    expect(input).toHaveFocus();
    await user.type(input, 'login');

    await within(dialog).findByText('Login with SSO');
    expect(search).toHaveBeenLastCalledWith(
      expect.objectContaining({ text: 'login', projectKey: 'ACME' }),
    );
  });

  it('does nothing outside a project route', async () => {
    const user = userEvent.setup();
    renderApp('/', new FakeProvider({ repos: [] }));
    await screen.findByRole('button', { name: 'Backlog action' });

    await user.keyboard(shortcut);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('filters the hits by tab', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider({ repos: [] });
    vi.spyOn(provider, 'search').mockResolvedValue({ hits });
    renderApp('/p/ACME/items', provider);
    await screen.findByRole('button', { name: 'Backlog action' });

    const dialog = await openOverlay(user);
    await user.type(within(dialog).getByRole('combobox'), 'login');
    const results = within(dialog).getByRole('listbox', { name: /search results/i });
    await waitFor(() => expect(within(results).getAllByRole('option')).toHaveLength(2));

    await user.click(within(dialog).getByRole('tab', { name: 'Items' }));
    expect(within(results).getAllByRole('option')).toHaveLength(1);
    expect(within(results).getByText('Login with SSO')).toBeInTheDocument();

    await user.click(within(dialog).getByRole('tab', { name: 'KB' }));
    expect(within(results).getAllByRole('option')).toHaveLength(1);
    expect(within(results).getByText('Login guide')).toBeInTheDocument();

    await user.click(within(dialog).getByRole('tab', { name: 'All' }));
    expect(within(results).getAllByRole('option')).toHaveLength(2);
  });

  it('moves the selection with the arrows and opens it on Enter', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider({ repos: [] });
    vi.spyOn(provider, 'search').mockResolvedValue({ hits });
    renderApp('/p/ACME/items', provider);
    await screen.findByRole('button', { name: 'Backlog action' });

    const dialog = await openOverlay(user);
    const input = within(dialog).getByRole('combobox');
    await user.type(input, 'login');
    const options = await within(dialog).findAllByRole('option');
    await waitFor(() => expect(options[0]).toHaveAttribute('aria-selected', 'true'));

    await user.keyboard('{ArrowDown}');
    const selected = within(dialog).getAllByRole('option')[1]!;
    expect(selected).toHaveAttribute('aria-selected', 'true');
    expect(input).toHaveAttribute('aria-activedescendant', selected.id);

    await user.keyboard('{Enter}');
    expect(await screen.findByText('KB page docs/guides/login.md')).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('opens an item on Enter', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider({ repos: [] });
    vi.spyOn(provider, 'search').mockResolvedValue({ hits });
    renderApp('/p/ACME/items', provider);
    await screen.findByRole('button', { name: 'Backlog action' });

    const dialog = await openOverlay(user);
    await user.type(within(dialog).getByRole('combobox'), 'login');
    await within(dialog).findByText('Login with SSO');

    await user.keyboard('{Enter}');
    expect(await screen.findByText('Item page ACME-US-0042')).toBeInTheDocument();
  });

  it('closes on Escape and gives focus back', async () => {
    const user = userEvent.setup();
    renderApp('/p/ACME/items', new FakeProvider({ repos: [] }));
    const action = await screen.findByRole('button', { name: 'Backlog action' });
    action.focus();

    await openOverlay(user);
    await user.keyboard('{Escape}');

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(action).toHaveFocus();
  });
});
