import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, type FakeYouTrack } from '@/api/fake-provider';
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
    fieldMap: { status: 'State' },
  },
};

function renderCard(youtrack?: FakeYouTrack) {
  const provider = new FakeProvider(youtrack === undefined ? {} : { youtrack });
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
    [
      'youtrack_unauthorized',
      /YouTrack rejected the token/,
      /permanent token in Profile/,
    ],
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

    const option = await screen.findByRole('option', { name: /Acme Web/ });
    await user.click(within(option).getByRole('button'));

    expect(picker).toHaveValue('WEB');
  });

  it('does not ask the instance for projects before a token is stored', async () => {
    renderCard({});
    const user = userEvent.setup();

    const picker = await screen.findByRole('combobox', { name: 'YouTrack project' });
    await user.type(picker, 'ac');

    expect(screen.queryByRole('listbox')).toBeNull();
    expect(screen.getByText(/Save a token to search the instance for it/)).toBeInTheDocument();
  });

  // ------------------------------------- push and sync policy (GIT-T-0188/0219)

  it('spells out what pushing every comment actually means before it is saved', async () => {
    renderCard(connected);
    const user = userEvent.setup();

    const push = await screen.findByLabelText('Push comments');
    expect(screen.getByText(/Nothing leaves this repository until someone uses/)).toBeInTheDocument();

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
    expect(screen.getByText(/published and pulled only from the knowledge-base toolbar/)).toBeInTheDocument();

    await user.selectOptions(sync, 'on_write');
    expect(screen.getByText(/Saving a knowledge-base page enqueues a publish/)).toBeInTheDocument();

    await user.selectOptions(screen.getByLabelText('Sync direction'), 'both');
    await user.click(screen.getByRole('button', { name: 'Save connection' }));

    await expect(provider.getYouTrackSettings()).resolves.toMatchObject({
      kbSync: 'on_write',
      kbSyncDirection: 'both',
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
