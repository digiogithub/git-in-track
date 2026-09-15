import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import type { SearchHit } from '@/api/provider';
import { useUiPrefs } from '@/app/ui-prefs';
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
    source: 'core',
  },
  {
    kind: 'page',
    path: 'knowledge/ways-of-working/definition-of-done.md',
    title: 'Definition of Done',
    snippet: 'a story is done when',
    score: 1,
    project: 'ACME-TEAM',
    vaultId: 'repo-team',
    source: 'core',
  },
];

/** What a Pando index adds: a passage, a score and no exact term in common. */
const semanticHit: SearchHit = {
  kind: 'page',
  path: 'knowledge/security/identity-provider.md',
  title: 'Identity provider',
  snippet: 'The identity provider issues the token every login exchange relies on.',
  score: 0.82,
  project: 'ACME-TEAM',
  vaultId: 'repo-team',
  source: 'pando',
};

/** A fake whose companion reports a semantic index behind it. */
function pandoProvider(): FakeProvider {
  return new FakeProvider({ repos: [], search: { fullTextSearch: 'pando' } });
}

describe('WorkspaceSearch', () => {
  beforeEach(() => {
    globalThis.localStorage.clear();
    useUiPrefs.getState().setSemanticResults(true);
  });

  it('labels every result with the project it came from', async () => {
    const provider = new FakeProvider({ repos: [] });
    const search = vi.spyOn(provider, 'search').mockResolvedValue({ hits });
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
    vi.spyOn(provider, 'search').mockResolvedValue({ hits: [] });
    renderWithRouter({ index: WorkspaceSearch, provider });

    const input = await screen.findByRole('searchbox', undefined, { timeout: 5000 });
    await userEvent.type(input, 'nothing');

    expect(
      await screen.findByText(/nothing matched/i, undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it('does not query on a single character', async () => {
    const provider = new FakeProvider({ repos: [] });
    const search = vi.spyOn(provider, 'search').mockResolvedValue({ hits });
    renderWithRouter({ index: WorkspaceSearch, provider });

    const input = await screen.findByRole('searchbox', undefined, { timeout: 5000 });
    await userEvent.type(input, 'a');

    expect(search).not.toHaveBeenCalled();
  });

  it('groups semantic hits below the exact ones', async () => {
    const provider = pandoProvider();
    vi.spyOn(provider, 'search').mockResolvedValue({ hits: [...hits, semanticHit] });
    renderWithRouter({ index: WorkspaceSearch, provider });

    await userEvent.type(
      await screen.findByRole('searchbox', undefined, { timeout: 5000 }),
      'login',
    );

    const exact = await screen.findByRole('list', { name: /search results/i }, { timeout: 5000 });
    const related = screen.getByRole('list', { name: /related by meaning/i });
    expect(within(exact).getAllByRole('listitem')).toHaveLength(2);
    expect(within(related).getAllByRole('listitem')).toHaveLength(1);
    expect(within(related).getByText('Identity provider')).toBeInTheDocument();
    // Exact matches come first in the document, not just in the data.
    expect(exact.compareDocumentPosition(related)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    // The relevance is shown, subdued, next to the hit.
    expect(within(related).getByText('82% match')).toBeInTheDocument();
  });

  it('leaves no semantic section behind when the capability is not pando', async () => {
    const provider = new FakeProvider({ repos: [] });
    vi.spyOn(provider, 'search').mockResolvedValue({ hits: [...hits, semanticHit] });
    renderWithRouter({ index: WorkspaceSearch, provider });

    await userEvent.type(
      await screen.findByRole('searchbox', undefined, { timeout: 5000 }),
      'login',
    );

    await screen.findByRole('list', { name: /search results/i }, { timeout: 5000 });
    expect(screen.queryByRole('list', { name: /related by meaning/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/related by meaning/i)).not.toBeInTheDocument();
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
    expect(screen.queryByText('Identity provider')).not.toBeInTheDocument();
  });

  it('highlights the query terms the snippet contains', async () => {
    const provider = pandoProvider();
    vi.spyOn(provider, 'search').mockResolvedValue({ hits: [semanticHit] });
    renderWithRouter({ index: WorkspaceSearch, provider });

    await userEvent.type(
      await screen.findByRole('searchbox', undefined, { timeout: 5000 }),
      'token',
    );

    const related = await screen.findByRole(
      'list',
      { name: /related by meaning/i },
      { timeout: 5000 },
    );
    const marks = within(related).getAllByText('token', { selector: 'mark' });
    expect(marks).toHaveLength(1);
    // The rest of the passage is still there, unmarked.
    expect(related.textContent).toContain('The identity provider issues the token');
  });

  it('shows HTML inside a snippet as text', async () => {
    const provider = pandoProvider();
    const injected = '<script>window.pwned = true;</script> login flow';
    vi.spyOn(provider, 'search').mockResolvedValue({
      hits: [{ ...semanticHit, snippet: injected }],
    });
    renderWithRouter({ index: WorkspaceSearch, provider });

    await userEvent.type(
      await screen.findByRole('searchbox', undefined, { timeout: 5000 }),
      'login',
    );

    const related = await screen.findByRole(
      'list',
      { name: /related by meaning/i },
      { timeout: 5000 },
    );
    expect(related.textContent).toContain('<script>window.pwned = true;</script>');
    expect(related.querySelector('script')).toBeNull();
    expect((globalThis as { pwned?: boolean }).pwned).toBeUndefined();
  });

  it('clamps a long snippet behind an expand control', async () => {
    const provider = pandoProvider();
    const long = `login ${'the identity provider issues a token. '.repeat(12)}`;
    vi.spyOn(provider, 'search').mockResolvedValue({ hits: [{ ...semanticHit, snippet: long }] });
    renderWithRouter({ index: WorkspaceSearch, provider });

    await userEvent.type(
      await screen.findByRole('searchbox', undefined, { timeout: 5000 }),
      'login',
    );

    const expand = await screen.findByRole('button', { name: /show more/i }, { timeout: 5000 });
    expect(expand).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(expand);
    expect(screen.getByRole('button', { name: /show less/i })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    expect(screen.getByRole('list', { name: /related by meaning/i }).textContent).toContain(
      long.trimEnd(),
    );
  });

  it('hides the semantic section when the toggle is off and remembers it', async () => {
    const provider = pandoProvider();
    vi.spyOn(provider, 'search').mockResolvedValue({ hits: [...hits, semanticHit] });
    const first = renderWithRouter({ index: WorkspaceSearch, provider });

    await userEvent.type(
      await screen.findByRole('searchbox', undefined, { timeout: 5000 }),
      'login',
    );
    await screen.findByRole('list', { name: /related by meaning/i }, { timeout: 5000 });

    await userEvent.click(screen.getByRole('switch', { name: /related by meaning/i }));
    expect(screen.queryByRole('list', { name: /related by meaning/i })).not.toBeInTheDocument();
    expect(useUiPrefs.getState().semanticResults).toBe(false);
    expect(globalThis.localStorage.getItem('gintrack:ui-prefs')).toContain(
      '"semanticResults":false',
    );

    // A fresh mount reads the same preference back: the section stays hidden.
    first.unmount();
    renderWithRouter({ index: WorkspaceSearch, provider });
    await userEvent.type(
      await screen.findByRole('searchbox', undefined, { timeout: 5000 }),
      'login',
    );
    await screen.findByRole('list', { name: /search results/i }, { timeout: 5000 });
    expect(screen.queryByRole('list', { name: /related by meaning/i })).not.toBeInTheDocument();
  });

  it('says so when the semantic half could not be reached', async () => {
    const provider = pandoProvider();
    vi.spyOn(provider, 'search').mockResolvedValue({ hits, degraded: true });
    renderWithRouter({ index: WorkspaceSearch, provider });

    await userEvent.type(
      await screen.findByRole('searchbox', undefined, { timeout: 5000 }),
      'login',
    );

    expect(
      await screen.findByText(/semantic search unavailable/i, undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
  });
});
