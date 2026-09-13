/**
 * The import dialog, end to end (story GIT-US-0059, tasks GIT-T-0099,
 * GIT-T-0104 and GIT-T-0108).
 *
 * Seven states are worth pinning down because each is a different promise:
 * searching (one request per settled keystroke), empty (a search that matched
 * nothing says so), selected (a running count, and an already-imported issue
 * still pickable), previewing (create versus update, with warnings that do not
 * block), running (live progress from the events, not a spinner), cancelled
 * (a job that stopped without failing) and failed (the message the engine
 * recorded, which is all a queued import can report).
 */

import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, type FakeYouTrack } from '@/api/fake-provider';
import type { SyncJobEvent, YouTrackImportOptions } from '@/api/provider';
import { renderImportDialog } from '@/features/youtrack/test-utils';

/** A connected project, which is the only state the dialog is reachable in. */
const connected: FakeYouTrack = {
  settings: {
    projectKey: 'GIT',
    configured: true,
    url: 'https://yt.example.com/youtrack',
    project: 'ACME',
    hasToken: true,
    tokenSource: 'file',
  },
};

function providerWith(over: Partial<FakeYouTrack> = {}, syncEngine = false) {
  return new FakeProvider({
    youtrack: { ...connected, ...over },
    ...(syncEngine ? { syncEngine: { jobs: [] } } : {}),
  });
}

/** One `sync.job.*` frame for the import the dialog is watching. */
function jobFrame(over: Partial<SyncJobEvent> = {}): SyncJobEvent {
  return {
    phase: 'progress',
    id: 'job_000021',
    kind: 'youtrack.import',
    key: 'GIT',
    state: 'running',
    attempt: 1,
    processed: 0,
    total: 3,
    error: '',
    errorClass: '',
    ...over,
  };
}

/** The router mounts a tick after render, so the box is awaited, never grabbed. */
const searchBox = () => screen.findByRole('combobox', { name: 'Search YouTrack issues' });

/** Picks one issue out of the suggestion list by its readable id. */
async function pick(user: ReturnType<typeof userEvent.setup>, idReadable: string) {
  await user.click(await searchBox());
  const option = await screen.findByRole('option', { name: new RegExp(idReadable) });
  await user.click(within(option).getByRole('button'));
}

describe('ImportDialog', () => {
  it('searches as the user types and lists the matches in an accessible listbox', async () => {
    const user = userEvent.setup();
    renderImportDialog(providerWith());

    await user.click(await searchBox());
    expect(await screen.findByRole('listbox', { name: /suggestions/ })).toBeInTheDocument();

    await user.type(await searchBox(), 'webhook');

    // One settled search, one list: the debounce is in the query hook, so the
    // intermediate lists never land.
    await waitFor(() => {
      expect(screen.getAllByRole('option')).toHaveLength(1);
    });
    expect(screen.getByRole('option', { name: /ACME-43/ })).toBeInTheDocument();
  });

  it('says so when a search matches nothing', async () => {
    const user = userEvent.setup();
    renderImportDialog(providerWith());

    await user.click(await searchBox());
    await user.type(await searchBox(), 'nothing matches this');

    expect(await screen.findByText('No issue matches this search.')).toBeInTheDocument();
  });

  it('narrows the list by preset and starts the query again', async () => {
    const user = userEvent.setup();
    renderImportDialog(providerWith());

    await user.click(await searchBox());
    await screen.findByRole('option', { name: /ACME-42/ });

    await user.click(screen.getByRole('button', { name: 'Stories' }));
    await user.click(await searchBox());

    await waitFor(() => {
      // ACME-42 and ACME-44 are Tasks, so the Stories preset excludes them.
      expect(screen.getAllByRole('option')).toHaveLength(1);
    });
    expect(screen.getByRole('option', { name: /ACME-43/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Stories' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('keeps a running count and marks an issue a previous import already created', async () => {
    const user = userEvent.setup();
    renderImportDialog(providerWith());

    expect(await screen.findByTestId('import-selected-count')).toHaveTextContent(
      'No issue selected',
    );

    await user.click(await searchBox());
    const linked = await screen.findByRole('option', { name: /ACME-43/ });
    // Already imported, and still selectable: re-selecting it is how an issue
    // is refreshed.
    expect(within(linked).getByText('Imported as GIT-US-0007')).toBeInTheDocument();
    await user.click(within(linked).getByRole('button'));

    expect(screen.getByTestId('import-selected-count')).toHaveTextContent('1 issue selected');

    await pick(user, 'ACME-42');
    expect(screen.getByTestId('import-selected-count')).toHaveTextContent('2 issues selected');
  });

  it('navigates the list with the keyboard and selects with Enter', async () => {
    const user = userEvent.setup();
    renderImportDialog(providerWith());

    await user.click(await searchBox());
    await screen.findByRole('option', { name: /ACME-42/ });

    await user.keyboard('{ArrowDown}{Enter}');

    expect(screen.getByTestId('import-selected-count')).toHaveTextContent('1 issue selected');
  });

  it('offers the Inbox option as visibly unavailable, with the reason', async () => {
    renderImportDialog(providerWith());

    const inbox = await screen.findByLabelText('Land in Inbox (not available yet)');
    expect(inbox).toBeDisabled();
    expect(screen.getByText(/arrives with the Inbox epic/)).toBeInTheDocument();
  });

  it('previews create versus update and shows warnings without blocking the run', async () => {
    const user = userEvent.setup();
    renderImportDialog(
      providerWith({
        preview: {
          project: 'GIT',
          issues: [
            {
              youtrackId: 'ACME-42',
              title: 'Rate limit the public API',
              mappedType: 'task',
              action: 'create',
              depth: 0,
              comments: 0,
              warnings: [
                {
                  field: 'State',
                  value: 'Wontfix',
                  fallback: 'cancelled',
                  reason: 'The state is not in this project’s workflow.',
                },
              ],
            },
            {
              youtrackId: 'ACME-43',
              title: 'Retry the webhook delivery',
              mappedType: 'story',
              action: 'update',
              targetId: 'GIT-US-0007',
              depth: 0,
              comments: 0,
              warnings: [],
            },
          ],
        },
      }),
    );

    await pick(user, 'ACME-42');
    await user.click(screen.getByRole('button', { name: 'Preview' }));

    const table = await screen.findByRole('table');
    expect(within(table).getByText('Create')).toBeInTheDocument();
    expect(within(table).getByText('Update GIT-US-0007')).toBeInTheDocument();
    expect(screen.getByTestId('import-preview-summary')).toHaveTextContent(
      '1 to create, 1 to update.',
    );
    expect(within(table).getByText(/not in this project’s workflow/)).toBeInTheDocument();

    // A warning is information, not a refusal.
    expect(screen.getByRole('button', { name: 'Run import' })).toBeEnabled();
  });

  it('sends the options it was given to preview and to run', async () => {
    const user = userEvent.setup();
    const provider = providerWith();
    renderImportDialog(provider);

    await pick(user, 'ACME-42');

    const depth = screen.getByLabelText('Subtask depth');
    await user.clear(depth);
    await user.type(depth, '2');
    await user.click(screen.getByLabelText('Include comments'));

    const seen: YouTrackImportOptions[] = [];
    const original = provider.previewYouTrackImport.bind(provider);
    provider.previewYouTrackImport = (options: YouTrackImportOptions) => {
      seen.push(options);
      return original(options);
    };

    await user.click(screen.getByRole('button', { name: 'Preview' }));
    await screen.findByRole('table');

    expect(seen).toEqual([
      {
        project: 'GIT',
        ids: ['ACME-42'],
        depth: 2,
        includeLinks: false,
        includeComments: true,
        includeAttachments: false,
      },
    ]);
  });

  it('shows live progress from the job events and ends in a summary', async () => {
    const user = userEvent.setup();
    const provider = providerWith({ importJobId: 'job_000021' }, true);
    renderImportDialog(provider);

    await pick(user, 'ACME-42');
    await user.click(screen.getByRole('button', { name: 'Preview' }));
    await screen.findByRole('table');
    await user.click(screen.getByRole('button', { name: 'Run import' }));

    // A coalesced frame: the count jumps, which is normal and not an error.
    provider.emitEvent({ kind: 'syncJob', job: jobFrame({ processed: 2, total: 3 }) });
    expect(await screen.findByText('Importing 2 of 3 issues…')).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: 'Import progress' })).toHaveAttribute(
      'aria-valuenow',
      '2',
    );

    provider.emitEvent({
      kind: 'syncJob',
      job: jobFrame({ phase: 'done', state: 'done', processed: 3, total: 3 }),
    });

    expect(await screen.findByText(/The import finished/)).toBeInTheDocument();
  });

  it('ignores the frames of another job', async () => {
    const user = userEvent.setup();
    const provider = providerWith({ importJobId: 'job_000021' }, true);
    renderImportDialog(provider);

    await pick(user, 'ACME-42');
    await user.click(screen.getByRole('button', { name: 'Preview' }));
    await screen.findByRole('table');
    await user.click(screen.getByRole('button', { name: 'Run import' }));

    provider.emitEvent({
      kind: 'syncJob',
      job: jobFrame({ id: 'job_999', phase: 'done', state: 'done', processed: 9, total: 9 }),
    });

    expect(await screen.findByText('Importing 0 of 1 issues…')).toBeInTheDocument();
  });

  it('says a cancelled import stopped without failing', async () => {
    const user = userEvent.setup();
    const provider = providerWith({ importJobId: 'job_000021' }, true);
    renderImportDialog(provider);

    await pick(user, 'ACME-42');
    await user.click(screen.getByRole('button', { name: 'Preview' }));
    await screen.findByRole('table');
    await user.click(screen.getByRole('button', { name: 'Run import' }));

    provider.emitEvent({
      kind: 'syncJob',
      job: jobFrame({ phase: 'done', state: 'cancelled', processed: 1, total: 3 }),
    });

    expect(await screen.findByText(/The import was cancelled/)).toBeInTheDocument();
  });

  it('reports a job that failed outright with the message the engine recorded', async () => {
    const user = userEvent.setup();
    const provider = providerWith({ importJobId: 'job_000021' }, true);
    renderImportDialog(provider);

    await pick(user, 'ACME-42');
    await user.click(screen.getByRole('button', { name: 'Preview' }));
    await screen.findByRole('table');
    await user.click(screen.getByRole('button', { name: 'Run import' }));

    provider.emitEvent({
      kind: 'syncJob',
      job: jobFrame({
        phase: 'failed',
        state: 'failed',
        error: '403 Forbidden: the token may not read ACME-44.',
      }),
    });

    expect(await screen.findByText('The import failed.')).toBeInTheDocument();
    expect(
      screen.getByText('403 Forbidden: the token may not read ACME-44.'),
    ).toBeInTheDocument();
  });

  it('explains a YouTrack failure in its own words rather than as a network error', async () => {
    const user = userEvent.setup();
    renderImportDialog(
      providerWith({
        previewError: {
          code: 'youtrack_forbidden',
          message: 'YouTrack answered 403.',
        },
      }),
    );

    await pick(user, 'ACME-42');
    await user.click(screen.getByRole('button', { name: 'Preview' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/at least “Read Project”/);
  });
});
