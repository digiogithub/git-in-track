import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import type { SearchHit } from '@/api/provider';
import { renderWithRouter } from '@/test/router';

import { WorkspaceSearch } from './WorkspaceSearch';

/** Hits from two repositories, which is the point of a workspace search. */
const hits: SearchHit[] = [
  {
    kind: 'item',
    id: 'ACME-US-0042',
    path: 'docs/.pmngr/stories/ACME-US-0042-login-with-sso.md',
    title: 'Login with SSO',
    snippet: 'single sign-on',
    score: 3,
    project: 'ACME',
    vaultId: 'repo-1',
  },
  {
    kind: 'page',
    path: 'knowledge/ways-of-working/definition-of-done.md',
    title: 'Definition of Done',
    snippet: 'a story is done when',
    score: 1,
    project: 'ACME-TEAM',
    vaultId: 'repo-team',
  },
];

describe('WorkspaceSearch', () => {
  it('labels every result with the project it came from', async () => {
    const provider = new FakeProvider({ repos: [] });
    const search = vi.spyOn(provider, 'search').mockResolvedValue(hits);
    renderWithRouter({ index: WorkspaceSearch, provider });

    const input = await screen.findByRole('searchbox', undefined, { timeout: 5000 });
    await userEvent.type(input, 'done');

    const list = await screen.findByRole('list', { name: /search results/i }, { timeout: 5000 });
    const rows = within(list).getAllByRole('listitem');
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByText('ACME')).toBeInTheDocument();
    expect(within(rows[1]!).getByText('ACME-TEAM')).toBeInTheDocument();

    // The query is not scoped to one project: the workspace answers as a whole.
    expect(search).toHaveBeenCalledWith(expect.objectContaining({ text: 'done' }));
    expect(search.mock.calls.at(-1)?.[0]).not.toHaveProperty('projectKey');
  });

  it('says so when nothing matched', async () => {
    const provider = new FakeProvider({ repos: [] });
    vi.spyOn(provider, 'search').mockResolvedValue([]);
    renderWithRouter({ index: WorkspaceSearch, provider });

    const input = await screen.findByRole('searchbox', undefined, { timeout: 5000 });
    await userEvent.type(input, 'nothing');

    expect(
      await screen.findByText(/nothing matched/i, undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it('does not query on a single character', async () => {
    const provider = new FakeProvider({ repos: [] });
    const search = vi.spyOn(provider, 'search').mockResolvedValue(hits);
    renderWithRouter({ index: WorkspaceSearch, provider });

    const input = await screen.findByRole('searchbox', undefined, { timeout: 5000 });
    await userEvent.type(input, 'a');

    expect(search).not.toHaveBeenCalled();
  });
});
