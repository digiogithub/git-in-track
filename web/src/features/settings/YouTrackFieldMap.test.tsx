import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { FakeProvider, type FakeYouTrack } from '@/api/fake-provider';
import type { YouTrackFieldMapping, YouTrackSettings } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';
import { YouTrackFieldMap } from '@/features/settings/YouTrackFieldMap';

/**
 * Mapping git-in-track fields, and their values, onto real YouTrack ones
 * (GIT-US-0065, task GIT-T-0139).
 *
 * The table has to be honest in three directions: a default is proposed rather
 * than silently applied; a mapping pointing at something that no longer exists
 * is kept and flagged rather than dropped — a rename in YouTrack must never
 * quietly unmap anything here; and a value the instance said nothing about
 * proposes nothing, which is why an absent `isResolved` may not become "done".
 */

const baseSettings: YouTrackSettings = {
  // The key of `sampleProject`, so the local half of a value mapping — this
  // project's statuses and priorities — is really there to be offered.
  projectKey: 'ACME',
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
    renderMap({ fieldMap: { status: { field: 'Assignee' } } });

    await waitFor(() => {
      expect(screen.getByLabelText('Status')).toHaveValue('Assignee');
    });
  });

  it('warns about a mapping the instance no longer offers instead of dropping it', async () => {
    renderMap({ fieldMap: { status: { field: 'Estado' } } });

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Estado');
    expect(alert).toHaveTextContent('no longer exist in ACME');
    // Still selected: the mapping survives until someone says otherwise.
    expect(screen.getByLabelText('Status')).toHaveValue('Estado');
    expect(screen.getByText('missing in YouTrack')).toBeInTheDocument();
  });

  it('clears a stale mapping only when asked to', async () => {
    renderMap({ fieldMap: { status: { field: 'Estado' } } });
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
    const saved = onSave.mock.calls[0]?.[0] as Record<string, YouTrackFieldMapping>;
    expect(saved['status']?.field).toBe('State');
    expect(saved['priority']?.field).toBe('Priority');
    expect(saved['type']?.field).toBe('Type');
    expect(saved).not.toHaveProperty('sprint');
  });

  it('explains why there is nothing to map yet when no token is stored', () => {
    renderMap({ hasToken: false });

    expect(screen.getByText(/Save a token and a YouTrack project/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Status')).toBeNull();
  });

  // --------------------------------------------------------- value mapping

  it('offers a row per value of the fields whose values can be mapped', async () => {
    renderMap();

    // The State bundle, mapped onto this project's own workflow statuses.
    expect(await screen.findByRole('heading', { name: 'State values → Status' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Priority values → Priority' })).toBeInTheDocument();
    // Assignee and Estimate carry no bundle, so they have a field row and
    // nothing below it.
    expect(screen.queryByRole('heading', { name: /Assignee values/ })).toBeNull();
  });

  it('proposes a value by name and marks the proposal', async () => {
    renderMap();

    await screen.findByRole('heading', { name: 'State values → Status' });
    // "In Progress" is `in_progress` here: the same word, spelled the two ways
    // the two systems spell it.
    await waitFor(() => {
      expect(screen.getByLabelText('In Progress')).toHaveValue('in_progress');
    });
    expect(screen.getByLabelText('Open')).toHaveValue('');
  });

  it('proposes a done status for a resolved state, and nothing for an unknown one', async () => {
    renderMap();

    await screen.findByRole('heading', { name: 'State values → Status' });
    // "Fixed" matches no status by name, but the instance said it resolves.
    await waitFor(() => {
      expect(screen.getByLabelText('Fixed')).toHaveValue('done');
    });
    // "Obsolete" carries no flag at all: silence is not a claim that it closes
    // an issue, so nothing is proposed for it.
    expect(screen.getByLabelText('Obsolete')).toHaveValue('');
  });

  it('shows an archived value rather than hiding the mapping it still needs', async () => {
    renderMap();

    await screen.findByRole('heading', { name: 'State values → Status' });
    expect(screen.getByLabelText('Obsolete')).toBeInTheDocument();
    expect(screen.getByText('archived')).toBeInTheDocument();
  });

  it('saves the value map under the field it belongs to', async () => {
    const { onSave } = renderMap();
    const user = userEvent.setup();

    await screen.findByRole('heading', { name: 'State values → Status' });
    await waitFor(() => {
      expect(screen.getByLabelText('In Progress')).toHaveValue('in_progress');
    });
    await user.selectOptions(screen.getByLabelText('Open'), 'todo');
    await user.click(screen.getByRole('button', { name: 'Save field map' }));

    const saved = onSave.mock.calls[0]?.[0] as Record<string, YouTrackFieldMapping>;
    expect(saved['status']).toEqual({
      field: 'State',
      values: { 'In Progress': 'in_progress', Fixed: 'done', Open: 'todo' },
    });
    // A field with no value mapping is written flat, with no empty `values`.
    expect(saved['assignee']).toEqual({ field: 'Assignee' });
  });

  it('drops the values of a field that is pointed somewhere else', async () => {
    const { onSave } = renderMap();
    const user = userEvent.setup();

    await screen.findByRole('heading', { name: 'State values → Status' });
    await waitFor(() => {
      expect(screen.getByLabelText('In Progress')).toHaveValue('in_progress');
    });
    // The values were read off the State bundle; Type's bundle is a different
    // vocabulary, so carrying them over would be nonsense.
    await user.selectOptions(screen.getByLabelText('Status'), 'Type');
    await user.click(screen.getByRole('button', { name: 'Save field map' }));

    const saved = onSave.mock.calls[0]?.[0] as Record<string, YouTrackFieldMapping>;
    // Nothing in the Type bundle names a status, so the entry is written flat.
    expect(saved['status']).toEqual({ field: 'Type' });
  });
});
