import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { FakeProvider, type FakeYouTrack } from '@/api/fake-provider';
import type { YouTrackSettings } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';
import { YouTrackFieldMap } from '@/features/settings/YouTrackFieldMap';

/**
 * Mapping git-in-track fields onto real YouTrack custom fields (GIT-US-0065).
 *
 * The table has to be honest in two directions: a default is proposed rather
 * than silently applied, and a mapping pointing at a field the instance no
 * longer has is kept and flagged rather than dropped — a rename in YouTrack
 * must never quietly unmap a field here.
 */

const baseSettings: YouTrackSettings = {
  projectKey: 'GIT',
  configured: true,
  url: 'https://yt.example.com/youtrack',
  project: 'ACME',
  fieldMap: {},
  pushComments: 'manual',
  kbSync: 'manual',
  kbSyncDirection: 'push',
  hasToken: true,
  tokenSource: 'file',
  persisted: true,
  projectPath: 'docs/.pmngr/project.yaml',
  repo: 'repo-1',
};

function renderMap(
  settings: Partial<YouTrackSettings> = {},
  youtrack: FakeYouTrack = {},
  onSave = vi.fn(),
) {
  const provider = new FakeProvider({ youtrack });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <ProviderContext.Provider value={provider}>
        <YouTrackFieldMap
          settings={{ ...baseSettings, ...settings }}
          project={settings.project ?? baseSettings.project}
          saving={false}
          onSave={onSave}
        />
      </ProviderContext.Provider>
    </QueryClientProvider>,
  );
  return { provider, onSave };
}

describe('YouTrackFieldMap', () => {
  it('proposes a default for every row it can match, and marks it as a proposal', async () => {
    renderMap();

    const status = await screen.findByLabelText('Status');
    expect(status).toHaveValue('State');
    expect(screen.getByLabelText('Priority')).toHaveValue('Priority');
    expect(screen.getByLabelText('Estimate')).toHaveValue('Estimation');
    expect(screen.getAllByText('proposed').length).toBeGreaterThan(0);
  });

  it('leaves a row nothing matched visibly unmapped rather than guessing', async () => {
    renderMap();

    await screen.findByLabelText('Status');
    // The sample instance has no sprint-shaped field.
    expect(screen.getByLabelText('Sprint')).toHaveValue('');
    expect(screen.getAllByText('unmapped').length).toBeGreaterThan(0);
  });

  it('keeps a saved mapping over a proposal', async () => {
    renderMap({ fieldMap: { status: 'Assignee' } });

    await waitFor(() => {
      expect(screen.getByLabelText('Status')).toHaveValue('Assignee');
    });
  });

  it('warns about a mapping the instance no longer offers instead of dropping it', async () => {
    renderMap({ fieldMap: { status: 'Estado' } });

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Estado');
    expect(alert).toHaveTextContent('no longer exist in ACME');
    // Still selected: the mapping survives until someone says otherwise.
    expect(screen.getByLabelText('Status')).toHaveValue('Estado');
    expect(screen.getByText('missing in YouTrack')).toBeInTheDocument();
  });

  it('clears a stale mapping only when asked to', async () => {
    renderMap({ fieldMap: { status: 'Estado' } });
    const user = userEvent.setup();

    await user.click(
      await screen.findByRole('button', { name: /Clear the mapping that no longer exists/ }),
    );

    await waitFor(() => {
      expect(screen.queryByRole('alert')).toBeNull();
    });
    // A cleared row is left unmapped rather than re-guessed: the clearing was
    // the user's decision, and a proposal would quietly undo it.
    expect(screen.getByLabelText('Status')).toHaveValue('');
  });

  it('saves the map it shows, dropping the unmapped rows', async () => {
    const { onSave } = renderMap();
    const user = userEvent.setup();

    await screen.findByLabelText('Status');
    await user.selectOptions(screen.getByLabelText('Type'), 'Type');
    await user.click(screen.getByRole('button', { name: 'Save field map' }));

    expect(onSave).toHaveBeenCalledTimes(1);
    const saved = onSave.mock.calls[0]?.[0] as Record<string, string>;
    expect(saved).toMatchObject({ status: 'State', priority: 'Priority', type: 'Type' });
    expect(saved).not.toHaveProperty('sprint');
  });

  it('explains why there is nothing to map yet when no token is stored', () => {
    renderMap({ hasToken: false });

    expect(screen.getByText(/Save a token and a YouTrack project/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Status')).toBeNull();
  });
});
