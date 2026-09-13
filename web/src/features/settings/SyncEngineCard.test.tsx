/**
 * The sync engine card (story GIT-US-0081, tasks GIT-T-0162 and GIT-T-0166).
 *
 * What is worth pinning down is what the card promises: it is absent where
 * there is no engine, it refuses a knob that cannot work before it sends
 * anything, it says whether a change survived a restart, it renders every job
 * state with only the actions that state allows, and it updates from the event
 * stream rather than from a timer.
 */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, type FakeSyncEngine } from '@/api/fake-provider';
import type { SyncJob } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';
import { ToastProvider } from '@/components/ui/toast';
import { SyncEngineCard } from '@/features/settings/SyncEngineCard';

/** A queue holding one job in each state the table has to render. */
const jobs: SyncJob[] = [
  {
    id: 'job_000021',
    kind: 'youtrack.import',
    key: 'ACME',
    state: 'queued',
    attempts: 0,
    createdAt: '2026-09-13T11:00:00Z',
    updatedAt: '2026-09-13T11:00:00Z',
  },
  {
    id: 'job_000022',
    kind: 'youtrack.import',
    key: 'ACME',
    state: 'running',
    attempts: 1,
    createdAt: '2026-09-13T11:01:00Z',
    updatedAt: '2026-09-13T11:01:30Z',
  },
  {
    id: 'job_000023',
    kind: 'youtrack.comment.push',
    key: 'ACME',
    state: 'failed',
    attempts: 5,
    createdAt: '2026-09-13T10:58:00Z',
    updatedAt: '2026-09-13T10:59:12Z',
    nextAttempt: '2026-09-13T11:01:00Z',
    deadLetter: true,
    lastError: {
      attempt: 5,
      class: 'terminal',
      message: '403 Forbidden <img src=x onerror=alert(1)>',
      at: '2026-09-13T10:59:12Z',
    },
  },
  {
    id: 'job_000024',
    kind: 'youtrack.kb.publish',
    key: 'ACME',
    state: 'done',
    attempts: 1,
    createdAt: '2026-09-13T10:50:00Z',
    updatedAt: '2026-09-13T10:51:00Z',
  },
];

function renderCard(syncEngine?: FakeSyncEngine) {
  const provider = new FakeProvider(syncEngine === undefined ? {} : { syncEngine });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <ProviderContext.Provider value={provider}>
        <ToastProvider>
          <SyncEngineCard />
        </ToastProvider>
      </ProviderContext.Provider>
    </QueryClientProvider>,
  );
  return provider;
}

describe('SyncEngineCard', () => {
  it('is absent on a runtime that has no engine, browser-only mode included', async () => {
    renderCard();

    await waitFor(() => {
      expect(screen.queryByText('Sync engine')).toBeNull();
    });
    expect(screen.queryByLabelText('Workers')).toBeNull();
  });

  it('loads the knobs in force on mount', async () => {
    renderCard({ settings: { workers: 4, batchSize: 50, rate: 10, maxAttempts: 7 } });

    expect(await screen.findByLabelText('Workers')).toHaveValue(4);
    expect(screen.getByLabelText('Batch size')).toHaveValue(50);
    expect(screen.getByLabelText('Rate limit')).toHaveValue(10);
    expect(screen.getByLabelText('Max attempts')).toHaveValue(7);
  });

  it('refuses an out-of-range value inline, before any request', async () => {
    const user = userEvent.setup();
    const provider = renderCard({});

    const workers = await screen.findByLabelText('Workers');
    await user.clear(workers);
    await user.type(workers, '99');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Workers must be between 1 and 64.');
    // Nothing was sent: the settings the fake holds are untouched.
    const settings = await provider.getSyncSettings();
    expect(settings.engine?.workers).toBe(2);
  });

  it('saves the knobs and says the change is process-only', async () => {
    const user = userEvent.setup();
    const provider = renderCard({});

    const workers = await screen.findByLabelText('Workers');
    await user.clear(workers);
    await user.type(workers, '8');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByText('Sync engine updated')).toBeInTheDocument();
    expect(screen.getByText(/configuration file has no sync.engine section/)).toBeInTheDocument();
    const settings = await provider.getSyncSettings();
    expect(settings.engine?.workers).toBe(8);
  });

  it('says so when the change reached the configuration file', async () => {
    const user = userEvent.setup();
    renderCard({ persisted: true });

    const batch = await screen.findByLabelText('Batch size');
    await user.clear(batch);
    await user.type(batch, '30');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    expect(
      await screen.findByText('The change was written to the configuration file.'),
    ).toBeInTheDocument();
  });

  it('says the queue is empty rather than showing an empty table', async () => {
    renderCard({});

    expect(
      await screen.findByText('The queue is empty. Nothing is waiting and nothing failed.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('renders every job state with its counts, and error text as plain text', async () => {
    renderCard({ jobs });

    const table = await screen.findByRole('table');
    expect(within(table).getByText('queued')).toBeInTheDocument();
    expect(within(table).getByText('running')).toBeInTheDocument();
    expect(within(table).getByText('failed')).toBeInTheDocument();
    expect(within(table).getByText('done')).toBeInTheDocument();
    expect(within(table).getByText('dead letter')).toBeInTheDocument();

    const counts = screen.getByTestId('sync-job-counts');
    expect(counts).toHaveTextContent('1 queued');
    expect(counts).toHaveTextContent('1 running');
    expect(counts).toHaveTextContent('1 failed');

    // The tracker's words are shown, and shown as text: no element was built
    // from them.
    const error = within(table).getByText('403 Forbidden <img src=x onerror=alert(1)>');
    expect(error).toBeInTheDocument();
    expect(table.querySelector('img')).toBeNull();
  });

  it('enables retry only where it applies and re-queues the job', async () => {
    const user = userEvent.setup();
    const provider = renderCard({ jobs });

    await screen.findByRole('table');
    expect(screen.getByRole('button', { name: 'Retry job_000021' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Retry job_000024' })).toBeDisabled();

    const retryFailed = screen.getByRole('button', { name: 'Retry job_000023' });
    expect(retryFailed).toBeEnabled();
    await user.click(retryFailed);

    expect(await screen.findByText('Job re-queued')).toBeInTheDocument();
    const page = await provider.listSyncJobs();
    expect(page.jobs.find((job) => job.id === 'job_000023')?.state).toBe('queued');
  });

  it('enables cancel only where it applies and withdraws the job', async () => {
    const user = userEvent.setup();
    const provider = renderCard({ jobs });

    await screen.findByRole('table');
    expect(screen.getByRole('button', { name: 'Cancel job_000023' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel job_000024' })).toBeDisabled();

    await user.click(screen.getByRole('button', { name: 'Cancel job_000021' }));

    expect(await screen.findByText('Job cancelled')).toBeInTheDocument();
    const page = await provider.listSyncJobs();
    expect(page.jobs.find((job) => job.id === 'job_000021')?.state).toBe('cancelled');
  });

  it('explains a refused transition instead of failing silently', async () => {
    const user = userEvent.setup();
    renderCard({
      jobs,
      retryError: {
        code: 'sync_job_not_retryable',
        message: 'Job job_000023 is done: only a failed or cancelled job can be retried.',
      },
    });

    await screen.findByRole('table');
    await user.click(screen.getByRole('button', { name: 'Retry job_000023' }));

    expect(await screen.findByText('Could not retry the job')).toBeInTheDocument();
    expect(screen.getByText(/only a failed or cancelled job can be retried/)).toBeInTheDocument();
  });

  it('re-reads the queue when a job event arrives, with no timer involved', async () => {
    const provider = renderCard({ jobs });

    await screen.findByRole('table');
    expect(screen.getByRole('button', { name: 'Cancel job_000021' })).toBeEnabled();

    // The engine finished the job somewhere else; the frame is only the signal
    // to read the listing again.
    await provider.cancelSyncJob('job_000021');
    provider.emitEvent({
      kind: 'syncJob',
      job: {
        phase: 'done',
        id: 'job_000021',
        kind: 'youtrack.import',
        key: 'ACME',
        state: 'cancelled',
        attempt: 1,
        processed: 20,
        total: 20,
        error: '',
        errorClass: '',
      },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Cancel job_000021' })).toBeDisabled();
    });
  });
});
