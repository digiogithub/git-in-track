import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, type FakeSearch } from '@/api/fake-provider';
import { ProviderContext } from '@/api/provider-context';
import { ToastProvider } from '@/components/ui/toast';
import { PandoSearchCard } from '@/features/settings/PandoSearchCard';

/**
 * The semantic-search settings card (story GIT-US-0091).
 *
 * What is worth pinning down is what the card promises: it exists only where
 * the runtime has the surface, it never offers a token field, it says whether a
 * change reached the configuration file, it follows a reindex through the event
 * hub rather than by polling, and it refuses to dress up the two refusals as
 * generic failures.
 */

/** A configured companion, as the settings endpoint reports it. */
const configured: FakeSearch = {
  fullTextSearch: 'pando',
  settings: {
    mcpUrl: 'http://127.0.0.1:9777/mcp',
    restUrl: 'http://127.0.0.1:9778',
    projectId: 'acme-api',
    indexed: [
      {
        repo: 'acme-api',
        root: '/home/dana/src/acme-api',
        docs: ['docs'],
        items: 412,
        pages: 38,
        comments: 96,
        code: {
          project: 'home_dana_src_acme-api',
          status: 'indexing',
          job: 'idx-1',
          note: 'Indexing the repository root; code search answers as the index fills.',
        },
      },
    ],
  },
};

function renderCard(search?: FakeSearch) {
  const provider = new FakeProvider(search === undefined ? {} : { search });
  render(
    <ProviderContext.Provider value={provider}>
      <ToastProvider>
        <PandoSearchCard />
      </ToastProvider>
    </ProviderContext.Provider>,
  );
  return provider;
}

describe('PandoSearchCard', () => {
  it('is absent on a runtime with no search settings, browser-only mode included', () => {
    renderCard();

    expect(screen.queryByText('Semantic search (Pando)')).toBeNull();
    expect(screen.queryByLabelText('Pando MCP URL')).toBeNull();
  });

  it('renders the settings document: backend, endpoints, probe and what Pando indexes', async () => {
    renderCard(configured);

    expect(await screen.findByText('pando')).toBeInTheDocument();
    expect(screen.getByLabelText('Pando MCP URL')).toHaveValue('http://127.0.0.1:9777/mcp');
    expect(screen.getByLabelText('Pando REST URL')).toHaveValue('http://127.0.0.1:9778');
    expect(screen.getByLabelText('Code project id')).toHaveValue('acme-api');
    // The corpus directory is gone with the exporter (epic GIT-EP-0020).
    expect(screen.queryByLabelText('Corpus directory')).toBeNull();
    expect(screen.getByTestId('pando-reachability')).toHaveTextContent('Pando answered at');
    // One row per mounted repository, saying what Pando was pointed at and how
    // much this companion's own index finds there: a documentation directory
    // holding no items is a misconfigured KBPath, and the row shows it.
    expect(screen.getByText('/home/dana/src/acme-api')).toBeVisible();
    expect(screen.getByText('docs')).toBeVisible();
    expect(screen.getByText('412')).toBeInTheDocument();
    expect(screen.getByText('96')).toBeInTheDocument();
    expect(screen.getByTestId('pando-index-summary')).toHaveTextContent(
      'nothing is copied anywhere else',
    );
  });

  it('never offers a token field, and says where a token comes from instead', async () => {
    renderCard(configured);

    await screen.findByLabelText('Pando MCP URL');
    expect(screen.queryByLabelText(/token/i)).toBeNull();
    expect(document.querySelectorAll('input[type="password"]')).toHaveLength(0);
    expect(screen.getByTestId('pando-token-note')).toHaveTextContent('GINTRACK_PANDO_MCP_TOKEN');
  });

  it('carries the remote-Pando warning next to the switch', async () => {
    renderCard(configured);

    expect(await screen.findByRole('switch', { name: 'Allow a remote Pando' })).not.toBeChecked();
    expect(screen.getByTestId('pando-remote-warning')).toHaveTextContent(
      'file writes, shell execution, agent spawning',
    );
  });

  it('keeps the embedding-model pin visible', async () => {
    renderCard(configured);

    expect(await screen.findByTestId('pando-model-warning')).toHaveTextContent(
      'per Pando instance, not per corpus',
    );
  });

  it('states the code-project registration, including why there is no code search', async () => {
    renderCard(configured);

    expect(await screen.findByText('indexing')).toBeInTheDocument();
    expect(screen.getByText('home_dana_src_acme-api')).toBeInTheDocument();
    expect(
      screen.getByText(/Indexing the repository root; code search answers as the index fills\./),
    ).toBeInTheDocument();
    // The split itself is stated: the code index cannot see the backlog.
    expect(screen.getByTestId('pando-index-summary')).toHaveTextContent(
      'skips dot-directories, so it never sees the backlog',
    );
  });

  it('says why there is no code search when Pando refused the registration', async () => {
    renderCard({
      ...configured,
      settings: {
        ...configured.settings,
        indexed: [
          {
            repo: 'acme-api',
            root: '/home/dana/src/acme-api',
            docs: ['docs'],
            items: 412,
            pages: 38,
            comments: 96,
            code: {
              project: 'home_dana_src_acme-api',
              status: 'unavailable',
              note: 'Pando did not accept the code project, so there is no code search: pando: unreachable',
            },
          },
        ],
      },
    });

    expect(await screen.findByText('unavailable')).toBeInTheDocument();
    expect(screen.getByText(/there is no code search: pando: unreachable/)).toBeInTheDocument();
  });

  it('saves the five location fields and says the change reached the file', async () => {
    const provider = renderCard(configured);
    const user = userEvent.setup();

    const projectField = await screen.findByLabelText('Code project id');
    await user.clear(projectField);
    await user.type(projectField, 'acme');
    await user.click(screen.getByRole('button', { name: 'Save settings' }));

    expect(await screen.findByText('Semantic search settings saved')).toBeInTheDocument();
    expect(screen.getByText('Written to the configuration file.')).toBeInTheDocument();
    await expect(provider.getSearchSettings()).resolves.toMatchObject({ projectId: 'acme' });
  });

  it('says when a change only reached the running process', async () => {
    renderCard({ ...configured, persisted: false });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Save settings' }));

    expect(await screen.findByText(/Applied to the running companion only/)).toBeInTheDocument();
  });

  it('reports a Pando that did not answer, with what it said', async () => {
    renderCard({
      ...configured,
      settings: {
        ...configured.settings,
        reachable: false,
        reachableError: 'dial tcp 127.0.0.1:9777: connection refused',
      },
    });

    expect(await screen.findByText('degraded')).toBeInTheDocument();
    expect(screen.getByTestId('pando-reachability')).toHaveTextContent('connection refused');
  });

  it('follows a reindex from the event hub through to the finished job', async () => {
    const provider = renderCard(configured);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Reindex now' }));

    const button = await screen.findByRole('button', { name: 'Reindexing…' });
    expect(button).toBeDisabled();

    act(() => {
      provider.emitEvent({
        kind: 'searchProgress',
        operationId: 'reindex-1',
        repoId: 'acme-api',
        phase: 'code',
        percent: 33,
        done: 1,
        total: 3,
        message: 'Indexing acme-api',
      });
    });
    expect(screen.getByTestId('pando-reindex-progress')).toHaveTextContent(
      'Indexing the source tree',
    );
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '33');

    // The terminal frame is only the cue: the counts and the honest
    // knowledge-base note live on the job the card then re-reads.
    provider.finishSearchReindex({
      phase: 'completed',
      kbNote: 'Not reindexed: no Pando REST URL is configured',
      repos: [{ repo: 'acme-api', codeJob: 'idx-7741' }],
    });
    act(() => {
      provider.emitEvent({
        kind: 'searchProgress',
        operationId: 'reindex-1',
        repoId: '',
        phase: 'completed',
        percent: 100,
        done: 3,
        total: 3,
        message: '',
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('pando-reindex-job')).toHaveTextContent(
        'Not reindexed: no Pando REST URL is configured',
      );
    });
    expect(await screen.findByRole('button', { name: 'Reindex now' })).toBeEnabled();
  });

  it('explains a reindex that is already running rather than failing generically', async () => {
    renderCard({
      ...configured,
      reindexError: { code: 'search_reindex_running', message: 'reindex running' },
    });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Reindex now' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('A reindex is already running');
    // The running job was untouched, so the button stays offered.
    expect(screen.getByRole('button', { name: 'Reindex now' })).toBeEnabled();
  });
});
