/**
 * The knowledge-base sync surface (story GIT-US-0093, tasks GIT-T-0217 and
 * GIT-T-0218): the gating, the five badge states, the folder confirmation and
 * the conflict notice — on load and on the live event.
 *
 * Everything goes through the `DataProvider` seam and the fake provider. No
 * test here knows a URL: the companion's REST routes for these three
 * operations are owned elsewhere, and a test that pinned one would break for a
 * reason that has nothing to do with this screen.
 */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider, type FakeData } from '@/api/fake-provider';
import type { KbPageSyncStatus, KbSyncState } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { KbViewer } from '@/features/kb/KbViewer';
import { clearMarkdownCache } from '@/markdown';

vi.mock('mermaid', () => ({
  default: { initialize: vi.fn(), render: vi.fn(() => new Promise(() => undefined)) },
}));

const PAGE = 'docs/architecture/overview.md';

/** A fake whose project is linked to YouTrack, which is what gates the group. */
function linked(data: FakeData = {}): FakeProvider {
  return new FakeProvider({
    youtrack: { settings: { configured: true, projectKey: 'ACME' } },
    ...data,
  });
}

function renderKb(provider: FakeProvider, path = `/p/ACME/kb/${PAGE}`) {
  clearMarkdownCache();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: '/p/$project/kb/$', component: KbViewer }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </DataProviderProvider>
    </QueryClientProvider>,
  );
  return { ...utils, provider, queryClient };
}

function row(state: KbSyncState, extra: Partial<KbPageSyncStatus> = {}): KbPageSyncStatus {
  return { path: PAGE, linked: state !== 'unlinked', state, ...extra };
}

// ------------------------------------------------------------------- gating

describe('KB sync gating', () => {
  it('renders no toolbar at all in a runtime that cannot reach YouTrack', async () => {
    renderKb(new FakeProvider());

    await screen.findByRole('heading', { name: 'Architecture overview' });
    // Browser-only mode: there is no process to hold a credential, so the group
    // is absent rather than present and failing.
    expect(screen.queryByTestId('kb-sync-toolbar')).not.toBeInTheDocument();
  });

  it('renders no toolbar when the runtime supports YouTrack but no project is linked', async () => {
    renderKb(new FakeProvider({ youtrack: { settings: { configured: false } } }));

    await screen.findByRole('heading', { name: 'Architecture overview' });
    expect(screen.queryByTestId('kb-sync-toolbar')).not.toBeInTheDocument();
  });

  it('renders the toolbar when the runtime supports YouTrack and the project is linked', async () => {
    renderKb(linked());

    expect(await screen.findByTestId('kb-sync-toolbar')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Publish to YouTrack/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Sync now/ })).toBeInTheDocument();
  });
});

// -------------------------------------------------------------------- badge

describe('KB sync badge', () => {
  it('renders a page that mirrors no article as not published', async () => {
    renderKb(linked({ kbSync: { pages: [] } }));

    const toolbar = await screen.findByTestId('kb-sync-toolbar');
    expect(await within(toolbar).findByText('Not published')).toBeInTheDocument();
  });

  it('renders an in-sync page and names the article it mirrors', async () => {
    renderKb(
      linked({
        kbSync: {
          pages: [
            row('in_sync', {
              articleId: 'ACME-A-3',
              url: 'https://youtrack.example/articles/ACME-A-3',
            }),
          ],
        },
      }),
    );

    const toolbar = await screen.findByTestId('kb-sync-toolbar');
    expect(await within(toolbar).findByText('In sync')).toBeInTheDocument();
    const link = within(toolbar).getByRole('link', { name: 'ACME-A-3' });
    expect(link).toHaveAttribute('href', 'https://youtrack.example/articles/ACME-A-3');
  });

  it('renders local drift, remote drift and a conflict, each with its own word', async () => {
    const cases: [KbSyncState, string][] = [
      ['local_ahead', 'Local changes'],
      ['remote_ahead', 'Article is newer'],
      ['conflict', 'Conflict'],
    ];
    for (const [state, label] of cases) {
      const view = renderKb(linked({ kbSync: { pages: [row(state)] } }));
      const toolbar = await screen.findByTestId('kb-sync-toolbar');
      expect(await within(toolbar).findByText(label)).toBeInTheDocument();
      view.unmount();
    }
  });

  it('reports a per-page read failure beside the page, not as a screen failure', async () => {
    renderKb(
      linked({
        kbSync: {
          pages: [row('local_ahead', { articleId: 'ACME-A-3', error: '502 Bad Gateway' })],
        },
      }),
    );

    const toolbar = await screen.findByTestId('kb-sync-toolbar');
    // The state is still reported: one unreachable article must not hide it.
    expect(await within(toolbar).findByText('Local changes')).toBeInTheDocument();
    expect(within(toolbar).getByText(/502 Bad Gateway/)).toBeInTheDocument();
  });

  it('explains a status call that failed outright, in the problem code’s own words', async () => {
    renderKb(
      linked({
        kbSync: {
          statusError: { code: 'youtrack_unauthorized', message: '401' },
        },
      }),
    );

    const toolbar = await screen.findByTestId('kb-sync-toolbar');
    expect(await within(toolbar).findByText(/YouTrack rejected the token/)).toBeInTheDocument();
  });
});

// ------------------------------------------------------------ folder publish

describe('publishing', () => {
  it('confirms a folder publish and says how many pages it will touch', async () => {
    const user = userEvent.setup();
    const provider = linked();
    const publish = vi.spyOn(provider, 'publishKbPage');
    renderKb(provider);

    await screen.findByTestId('kb-sync-toolbar');
    await user.click(screen.getByRole('button', { name: /Publish folder/ }));

    const dialog = await screen.findByRole('dialog');
    // `docs/architecture` holds exactly one page in the fixture.
    expect(within(dialog).getByText(/This will touch 1 page\./)).toBeInTheDocument();
    // Nothing has been queued yet: the confirmation is the gate.
    expect(publish).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole('button', { name: /Publish 1 page/ }));
    await waitFor(() => {
      expect(publish).toHaveBeenCalledWith(
        expect.objectContaining({ path: 'docs/architecture', recursive: true }),
      );
    });
  });

  it('never suggests the feedback block is published', async () => {
    const user = userEvent.setup();
    renderKb(linked());

    await screen.findByTestId('kb-sync-toolbar');
    await user.click(screen.getByRole('button', { name: /Publish folder/ }));

    const dialog = await screen.findByRole('dialog');
    expect(
      within(dialog).getByText(/## Feedback block stays in the repository/),
    ).toBeInTheDocument();
  });

  it('queues a page publish and says so, rather than claiming it landed', async () => {
    const user = userEvent.setup();
    renderKb(linked());

    await screen.findByTestId('kb-sync-toolbar');
    await user.click(screen.getByRole('button', { name: /Publish to YouTrack/ }));

    expect(await screen.findByText('Publishing this page to YouTrack')).toBeInTheDocument();
    expect(screen.getByText(/queued; the job reports when it lands/)).toBeInTheDocument();
  });

  it('reports a refused publish with the problem code’s message', async () => {
    const user = userEvent.setup();
    renderKb(linked({ kbSync: { jobError: { code: 'youtrack_forbidden', message: '403' } } }));

    await screen.findByTestId('kb-sync-toolbar');
    await user.click(screen.getByRole('button', { name: /Publish to YouTrack/ }));

    expect(await screen.findByText('Nothing was published')).toBeInTheDocument();
    expect(screen.getByText(/may not read this project/)).toBeInTheDocument();
  });

  it('relabels the manual publish when the project publishes on save', async () => {
    renderKb(
      new FakeProvider({
        youtrack: {
          settings: { configured: true, projectKey: 'ACME', kbSync: 'on_write' },
        },
      }),
    );

    const toolbar = await screen.findByTestId('kb-sync-toolbar');
    // The setting already publishes; the button says it brings that forward
    // rather than claiming to be how pages reach YouTrack.
    expect(await within(toolbar).findByRole('button', { name: /Publish now/ })).toBeInTheDocument();
    expect(within(toolbar).queryByRole('button', { name: /Publish to YouTrack/ })).toBeNull();
    expect(within(toolbar).getByText(/saving a page already queues a publish/)).toBeInTheDocument();
  });

  it('keeps the manual wording when on_write only pulls', async () => {
    renderKb(
      new FakeProvider({
        youtrack: {
          settings: {
            configured: true,
            projectKey: 'ACME',
            kbSync: 'on_write',
            kbSyncDirection: 'pull',
          },
        },
      }),
    );

    const toolbar = await screen.findByTestId('kb-sync-toolbar');
    // A save that only pulls publishes nothing, so this button is still the
    // only way a page reaches YouTrack.
    expect(
      await within(toolbar).findByRole('button', { name: /Publish to YouTrack/ }),
    ).toBeInTheDocument();
  });

  it('offers no folder publish for a project with no pages at all', async () => {
    renderKb(linked({ pages: [] }), '/p/ACME/kb/');

    await screen.findByTestId('kb-sync-toolbar');
    expect(screen.getByRole('button', { name: /Publish folder/ })).toBeDisabled();
  });
});

// ----------------------------------------------------------- conflict notice

describe('conflict notice', () => {
  it('appears on load for a page whose state is conflict, and links to the conflict file', async () => {
    renderKb(linked({ kbSync: { pages: [row('conflict', { articleId: 'ACME-A-3' })] } }));

    const notice = await screen.findByRole('alert');
    expect(notice).toHaveTextContent(/this page was left exactly as it was/i);
    expect(
      within(notice).getByRole('link', { name: 'docs/architecture/overview.conflict.md' }),
    ).toBeInTheDocument();
    expect(notice).toHaveTextContent('ACME-A-3');
  });

  it('appears live when the conflict arrives while the page is open, and names the newer side', async () => {
    const provider = linked();
    renderKb(provider);

    await screen.findByTestId('kb-sync-toolbar');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    act(() => {
      provider.emitEvent({
        kind: 'kbConflict',
        project: 'ACME',
        path: PAGE,
        conflictPath: 'docs/architecture/overview.conflict.md',
        articleId: 'ACME-A-9',
        direction: 'pull',
      });
    });

    const notice = await screen.findByRole('alert');
    expect(notice).toHaveTextContent(/The article in YouTrack changed most recently/);
    expect(notice).toHaveTextContent(/this page was left exactly as it was/i);
    expect(
      within(notice).getByRole('link', { name: 'docs/architecture/overview.conflict.md' }),
    ).toBeInTheDocument();
  });

  it('leaves a page alone when the conflict belongs to another page', async () => {
    const provider = linked();
    renderKb(provider);

    await screen.findByTestId('kb-sync-toolbar');
    act(() => {
      provider.emitEvent({
        kind: 'kbConflict',
        project: 'ACME',
        path: 'docs/index.md',
        conflictPath: 'docs/index.conflict.md',
        direction: 'publish',
      });
    });

    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });
  });
});

// --------------------------------------------------------------- tree badges

describe('tree badges', () => {
  it('summarises a folder with the worst state under it', async () => {
    renderKb(
      linked({
        kbSync: {
          pages: [{ path: 'docs/index.md', linked: true, state: 'in_sync' }, row('conflict')],
        },
      }),
    );

    const tree = await screen.findByRole('navigation', { name: 'Knowledge base pages' });
    // `docs` contains a conflict, so the folder row says so without expanding.
    await waitFor(() => {
      expect(within(tree).getAllByText(/Conflict/).length).toBeGreaterThan(0);
    });
  });
});

// -------------------------------------------------------------- freshness

describe('staying fresh', () => {
  it('only reads the articles when a reader asks, and says the answer was checked', async () => {
    const user = userEvent.setup();
    const provider = linked({ kbSync: { pages: [row('in_sync')] } });
    const status = vi.spyOn(provider, 'kbSyncStatus');
    renderKb(provider);

    await screen.findByTestId('kb-sync-toolbar');
    await waitFor(() => {
      expect(status).toHaveBeenCalled();
    });
    // The tree-wide answer costs no request against the instance.
    expect(status.mock.calls.every(([selector]) => selector?.remote !== true)).toBe(true);

    await user.click(screen.getByRole('button', { name: /Check the article/ }));

    await waitFor(() => {
      expect(status).toHaveBeenCalledWith(expect.objectContaining({ remote: true, path: PAGE }));
    });
  });

  it('re-reads the state when a knowledge-base job reports, with no manual refresh', async () => {
    const provider = linked({ kbSync: { pages: [row('local_ahead')] } });
    const status = vi.spyOn(provider, 'kbSyncStatus');
    renderKb(provider);

    await screen.findByTestId('kb-sync-toolbar');
    await waitFor(() => {
      expect(status).toHaveBeenCalledTimes(1);
    });

    act(() => {
      provider.emitEvent({
        kind: 'syncJob',
        job: {
          id: 'job_kb_publish',
          kind: 'youtrack.kb.publish',
          key: 'ACME',
          state: 'done',
          phase: 'done',
          attempt: 1,
          processed: 1,
          total: 1,
          error: '',
          errorClass: '',
        },
      });
    });

    await waitFor(() => {
      expect(status).toHaveBeenCalledTimes(2);
    });
  });

  it('ignores a job that has nothing to do with the knowledge base', async () => {
    const provider = linked({ kbSync: { pages: [row('local_ahead')] } });
    const status = vi.spyOn(provider, 'kbSyncStatus');
    renderKb(provider);

    await screen.findByTestId('kb-sync-toolbar');
    await waitFor(() => {
      expect(status).toHaveBeenCalledTimes(1);
    });

    act(() => {
      provider.emitEvent({
        kind: 'syncJob',
        job: {
          id: 'job_import',
          kind: 'youtrack.import',
          key: 'ACME',
          state: 'done',
          phase: 'done',
          attempt: 1,
          processed: 20,
          total: 20,
          error: '',
          errorClass: '',
        },
      });
    });

    // An import moves items, not pages: refetching the whole tree's sync state
    // on every frame of a long import would be noise, not freshness.
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(status).toHaveBeenCalledTimes(1);
  });
});

// ------------------------------------------------------------------ unlinking

describe('unlinking a page', () => {
  it('is offered only for a page that mirrors an article', async () => {
    renderKb(linked({ kbSync: { pages: [row('unlinked')] } }));

    await screen.findByTestId('kb-sync-toolbar');
    expect(screen.queryByRole('button', { name: 'Unlink' })).toBeNull();
  });

  it('asks before it forgets, and says the article is left alone', async () => {
    const provider = linked({
      kbSync: { pages: [row('in_sync', { articleId: 'ACME-A-3' })] },
    });
    const unlink = vi.spyOn(provider, 'unlinkKbPage');
    renderKb(provider);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Unlink' }));

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/ACME-A-3/)).toBeInTheDocument();
    // The promise the confirmation makes: this is local, and it is not a
    // disconnect of the project.
    expect(within(dialog).getByText(/Nothing is sent to YouTrack/)).toBeInTheDocument();

    // Cancelling writes nothing at all.
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(unlink).not.toHaveBeenCalled();

    await user.click(screen.getByRole('button', { name: 'Unlink' }));
    await user.click(
      within(await screen.findByRole('dialog')).getByRole('button', { name: 'Unlink the page' }),
    );

    await waitFor(() => {
      expect(unlink).toHaveBeenCalledWith({ path: PAGE, project: 'ACME' });
    });
    // The page is unlinked now, so the action it no longer applies to is gone.
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Unlink' })).toBeNull();
    });
    expect(await screen.findByText('The page was unlinked')).toBeInTheDocument();
  });
});
