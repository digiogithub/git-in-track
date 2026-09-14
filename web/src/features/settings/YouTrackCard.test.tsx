import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleProject, type FakeYouTrack } from '@/api/fake-provider';
import type { ProjectSummary } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';
import { ToastProvider } from '@/components/ui/toast';
import { YouTrackCard } from '@/features/settings/YouTrackCard';

/**
 * Connecting a project to YouTrack from settings (story GIT-US-0055).
 *
 * The behaviour worth pinning down is what the card promises the user: it only
 * exists where the runtime can reach YouTrack at all, it never renders the
 * stored token (only that one is stored and where it came from), it refuses to
 * pretend it can clear a token that came from the environment, and each remote
 * failure reads as a different thing to do.
 */

/** A connected project, as the companion reports it. */
const connected: FakeYouTrack = {
  settings: {
    configured: true,
    url: 'https://yt.example.com/youtrack',
    project: 'ACME',
    hasToken: true,
    tokenSource: 'file',
    fieldMap: { status: { field: 'State' } },
  },
};

function renderCard(youtrack?: FakeYouTrack, projects?: ProjectSummary[]) {
  const provider = new FakeProvider({
    ...(youtrack === undefined ? {} : { youtrack }),
    ...(projects === undefined ? {} : { projects }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <ProviderContext.Provider value={provider}>
        <ToastProvider>
          <YouTrackCard />
        </ToastProvider>
      </ProviderContext.Provider>
    </QueryClientProvider>,
  );
  return provider;
}

const tokenField = () => screen.getByLabelText('Permanent token');
const urlField = () => screen.getByLabelText('Instance URL');

describe('YouTrackCard', () => {
  it('is absent on a runtime that cannot reach YouTrack, browser-only mode included', () => {
    renderCard();

    expect(screen.queryByText('YouTrack')).toBeNull();
    expect(screen.queryByLabelText('Instance URL')).toBeNull();
  });

  it('offers an empty form on a project that is not connected yet', async () => {
    renderCard({});

    expect(await screen.findByText('Not connected')).toBeInTheDocument();
    expect(urlField()).toHaveValue('');
    expect(tokenField()).toHaveValue('');
    expect(screen.getByTestId('youtrack-token-state')).toHaveTextContent('No token is stored.');
    expect(screen.queryByRole('button', { name: 'Forget the stored token' })).toBeNull();
  });

  it('loads a connected project without ever rendering the token', async () => {
    renderCard(connected);

    expect(await screen.findByText('Connected')).toBeInTheDocument();
    expect(urlField()).toHaveValue('https://yt.example.com/youtrack');
    // The API never returns the token, so the field stays empty: a masked
    // placeholder *value* would be saved back as a literal string.
    expect(tokenField()).toHaveValue('');
    expect(tokenField()).toHaveAttribute('placeholder', 'Stored — type a new one to replace it');
    expect(screen.getByTestId('youtrack-token-state')).toHaveTextContent(
      'A token is stored in the companion’s configuration file.',
    );
  });

  it('reports an environment token as one this screen may not clear', async () => {
    renderCard({ settings: { ...connected.settings, tokenSource: 'env' } });

    await waitFor(() => {
      expect(screen.getByTestId('youtrack-token-state')).toHaveTextContent(
        'A token comes from the environment of the running companion.',
      );
    });
    expect(tokenField()).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Forget the stored token' })).toBeNull();
  });

  it('forgets a stored token on request, and only then', async () => {
    const provider = renderCard(connected);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Forget the stored token' }));

    await waitFor(() => {
      expect(screen.getByTestId('youtrack-token-state')).toHaveTextContent('No token is stored.');
    });
    await expect(provider.getYouTrackSettings()).resolves.toMatchObject({ hasToken: false });
  });

  it('names the YouTrack user a successful test resolved', async () => {
    renderCard(connected);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Test connection' }));

    const status = await screen.findByRole('status');
    expect(status).toHaveTextContent('Jane Doe');
    expect(status).toHaveTextContent('jdoe');
    expect(status).toHaveTextContent('https://yt.example.com/youtrack');
  });

  it.each([
    ['youtrack_unauthorized', /YouTrack rejected the token/, /permanent token in Profile/],
    ['youtrack_forbidden', /may not read this project/, /Read Project/],
    ['youtrack_not_found', /this project does not exist there/, /context path/],
    ['youtrack_unreachable', /could not be reached/, /firewall/],
    ['youtrack_not_configured', /not connected to YouTrack yet/, /test the connection/],
  ] as const)('explains %s as its own problem', async (code, headline, advice) => {
    renderCard({
      ...connected,
      testError: { code, message: 'the raw companion detail' },
    });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Test connection' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(headline);
    expect(alert).toHaveTextContent(advice);
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('saves the connection and says the change reached the project file', async () => {
    const provider = renderCard({ persisted: true });
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Instance URL'), 'https://yt.example.com');
    await user.type(tokenField(), 'perm:typed-by-hand');
    await user.type(screen.getByLabelText('YouTrack project'), 'ACME');
    await user.click(screen.getByRole('button', { name: 'Save connection' }));

    expect(await screen.findByText('YouTrack connection saved')).toBeInTheDocument();
    expect(screen.getByText('Written to docs/.pmngr/project.yaml.')).toBeInTheDocument();
    await expect(provider.getYouTrackSettings()).resolves.toMatchObject({
      url: 'https://yt.example.com',
      project: 'ACME',
      hasToken: true,
      configured: true,
    });
    // The field is cleared after the save, so the typed token is not left in
    // the DOM for the rest of the session.
    expect(tokenField()).toHaveValue('');
  });

  it('says when a change only reached the running process', async () => {
    renderCard({ ...connected, persisted: false });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Save connection' }));

    expect(await screen.findByText(/Applied to the running companion only/)).toBeInTheDocument();
  });

  it('suggests projects from the instance once a token is stored', async () => {
    renderCard(connected);
    const user = userEvent.setup();

    const picker = await screen.findByRole('combobox', { name: 'YouTrack project' });
    await user.clear(picker);
    await user.type(picker, 'web');

    // Re-query the option immediately before the click. The list is backed by a
    // query whose key is the debounced search text, so holding a node across an
    // await is holding a node the next answer may already have replaced.
    await screen.findByRole('option', { name: /Acme Web/ });
    const option = screen.getByRole('option', { name: /Acme Web/ });
    await user.click(within(option).getByRole('button'));

    expect(picker).toHaveValue('WEB');
  });

  it('does not ask the instance for projects with no credential at all', async () => {
    renderCard({});
    const user = userEvent.setup();

    const picker = await screen.findByRole('combobox', { name: 'YouTrack project' });
    await user.type(picker, 'ac');

    expect(screen.queryByRole('listbox')).toBeNull();
    expect(
      screen.getByText(/Fill in the URL and a token to search the instance for it/),
    ).toBeInTheDocument();
  });

  it('searches the instance with a URL and token that are typed but not saved', async () => {
    // Nothing is stored: the picker has to work anyway, because choosing the
    // remote project is part of connecting to it.
    const provider = renderCard({});
    const user = userEvent.setup();

    await user.type(
      await screen.findByLabelText('Instance URL'),
      'https://yt.example.com/youtrack',
    );
    await user.type(tokenField(), 'perm:typed');

    const picker = screen.getByRole('combobox', { name: 'YouTrack project' });
    await user.type(picker, 'web');
    await screen.findByRole('option', { name: /Acme Web/ });
    await user.click(within(screen.getByRole('option', { name: /Acme Web/ })).getByRole('button'));

    expect(picker).toHaveValue('WEB');
    expect(provider.youtrackProbe).toEqual({
      url: 'https://yt.example.com/youtrack',
      token: 'perm:typed',
    });

    // What the picker is for: the entity id every YouTrack write addresses a
    // project by, which nobody can type and a short name cannot stand in for.
    await user.click(screen.getByRole('button', { name: 'Save connection' }));
    await expect(provider.getYouTrackSettings()).resolves.toMatchObject({
      project: 'WEB',
      projectId: '0-2',
    });
  });

  it('drops a recorded id when the short name is typed over', async () => {
    const provider = renderCard({
      ...connected,
      settings: { ...connected.settings, projectId: '0-1' },
    });
    const user = userEvent.setup();

    const picker = await screen.findByRole('combobox', { name: 'YouTrack project' });
    expect(screen.getByText(/Linked to 0-1/)).toBeInTheDocument();

    await user.clear(picker);
    await user.type(picker, 'OTHER');
    await user.click(screen.getByRole('button', { name: 'Save connection' }));

    // The id belonged to the previous project: keeping it would publish this
    // project's pages into that one.
    await expect(provider.getYouTrackSettings()).resolves.toMatchObject({
      project: 'OTHER',
      projectId: '',
    });
  });

  // ------------------------------------- push and sync policy (GIT-T-0188/0219)

  it('spells out what pushing every comment actually means before it is saved', async () => {
    renderCard(connected);
    const user = userEvent.setup();

    const push = await screen.findByLabelText('Push comments');
    expect(
      screen.getByText(/Nothing leaves this repository until someone uses/),
    ).toBeInTheDocument();

    await user.selectOptions(push, 'auto');

    // The consequence is named, and so is its limit: nothing is sent backwards.
    expect(screen.getByText(/Every comment written here from now on/)).toBeInTheDocument();
    expect(screen.getByText(/not sent retroactively/)).toBeInTheDocument();
  });

  it('persists the comment policy through the settings patch', async () => {
    const provider = renderCard({ ...connected, persisted: true });
    const user = userEvent.setup();

    await user.selectOptions(await screen.findByLabelText('Push comments'), 'auto');
    await user.click(screen.getByRole('button', { name: 'Save connection' }));

    await expect(provider.getYouTrackSettings()).resolves.toMatchObject({
      pushComments: 'auto',
    });
  });

  it('spells out what on_write costs, and persists it with its direction', async () => {
    const provider = renderCard({ ...connected, persisted: true });
    const user = userEvent.setup();

    const sync = await screen.findByLabelText('Knowledge base sync');
    expect(
      screen.getByText(/published and pulled only from the knowledge-base toolbar/),
    ).toBeInTheDocument();

    await user.selectOptions(sync, 'on_write');
    expect(screen.getByText(/Saving a knowledge-base page enqueues a publish/)).toBeInTheDocument();

    await user.selectOptions(screen.getByLabelText('Sync direction'), 'both');
    await user.click(screen.getByRole('button', { name: 'Save connection' }));

    await expect(provider.getYouTrackSettings()).resolves.toMatchObject({
      kbSync: 'on_write',
      kbSyncDirection: 'both',
    });
  });

  it('scopes every call to one project, and offers the choice only where there is one', async () => {
    // A companion serving a single project resolves `?key=` on its own, so the
    // picker would be a question with one answer.
    const single = renderCard(connected);
    expect(await screen.findByText('Connected')).toBeInTheDocument();
    expect(screen.queryByLabelText('git-in-track project')).toBeNull();
    expect(single.youtrackScope).toEqual({ projectKey: 'ACME' });
  });

  it('reads and writes the project the picker names, not whichever one is first', async () => {
    const other: ProjectSummary = { ...sampleProject, key: 'WEB', name: 'Web' };
    const provider = renderCard(connected, [sampleProject, other]);
    const user = userEvent.setup();

    // The first project is connected; its connection is what loads.
    const picker = await screen.findByLabelText('git-in-track project');
    expect(await screen.findByText('Connected')).toBeInTheDocument();
    expect(urlField()).toHaveValue('https://yt.example.com/youtrack');

    // The second is a project of its own, with a connection of its own — the
    // bug this replaces sent every call unscoped, which the companion refuses.
    await user.selectOptions(picker, 'WEB');
    await waitFor(() => {
      expect(urlField()).toHaveValue('');
    });
    expect(screen.getByText('Not connected')).toBeInTheDocument();

    await user.type(urlField(), 'https://yt.example.com/youtrack');
    await user.click(screen.getByRole('button', { name: 'Save connection' }));

    await expect(provider.getYouTrackSettings({ projectKey: 'WEB' })).resolves.toMatchObject({
      url: 'https://yt.example.com/youtrack',
    });
    // …and saving one project must not have touched the other.
    await expect(provider.getYouTrackSettings({ projectKey: 'ACME' })).resolves.toMatchObject({
      project: 'ACME',
      url: 'https://yt.example.com/youtrack',
    });
  });

  it('never suggests the feedback block will be published', async () => {
    renderCard(connected);

    // The `## Feedback` block stays in the repository whichever way pages
    // travel (ADR-030); the card has to say so rather than leave it implied.
    expect(await screen.findByText(/is never published/)).toBeInTheDocument();
    expect(screen.getByText(/reader notes stay in\s+this repository/)).toBeInTheDocument();
  });
});
