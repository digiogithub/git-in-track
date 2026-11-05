import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider, sampleItems, sampleProject } from '@/api/fake-provider';
import type { Item, ItemFilter } from '@/api/provider';

import { filterStorageKey, readStoredFilter } from './filter-memory';
import { renderBacklog } from './test-utils';

const ACME_KEY = filterStorageKey('ACME');

const openStatuses = ['backlog', 'todo', 'in_progress', 'in_review'];

function shippedItem(): Item {
  const base = sampleItems.find((item) => item.id === 'ACME-T-0107');
  if (!base) throw new Error('sample task missing');
  return { ...base, id: 'ACME-T-0999', title: 'Shipped already', status: 'done' };
}

function providerWithDoneItem(): FakeProvider {
  return new FakeProvider({
    projects: [sampleProject, { ...sampleProject, key: 'BETA', name: 'Beta' }],
    items: [...sampleItems, shippedItem()],
  });
}

function stored(key: string): unknown {
  const raw = localStorage.getItem(key);
  return raw === null ? null : JSON.parse(raw);
}

/** Every `status` filter the list asked the provider for, in call order. */
function statusCalls(spy: { mock: { calls: [ItemFilter][] } }): unknown[] {
  return spy.mock.calls.map((call) => call[0].status);
}

beforeEach(() => {
  localStorage.clear();
});

describe('readStoredFilter', () => {
  it('returns a stored filter that still fits the workflow', () => {
    localStorage.setItem(ACME_KEY, JSON.stringify({ status: 'todo', label: 'auth' }));
    expect(readStoredFilter(ACME_KEY, sampleProject)).toMatchObject({
      status: 'todo',
      label: 'auth',
    });
  });

  it.each([
    ['bad JSON', '{not json'],
    ['a non-object', '[1,2]'],
    ['an empty object', '{}'],
    ['a status the workflow dropped', JSON.stringify({ status: 'todo,gone' })],
    ['an unknown quick view', JSON.stringify({ view: 'nope' })],
  ])('falls back to nothing and forgets %s', (_label, raw) => {
    localStorage.setItem(ACME_KEY, raw);
    expect(readStoredFilter(ACME_KEY, sampleProject)).toBeNull();
    expect(localStorage.getItem(ACME_KEY)).toBeNull();
  });

  it('keys a project by its repository when the provider reports one', () => {
    expect(filterStorageKey('ACME', 'repo-1')).not.toBe(filterStorageKey('ACME', 'repo-2'));
    expect(filterStorageKey('ACME')).not.toBe(filterStorageKey('BETA'));
  });
});

describe('backlog filter memory', () => {
  it('opens on pending work when nothing is stored and the URL is empty', async () => {
    const provider = providerWithDoneItem();
    const listItems = vi.spyOn(provider, 'listItems');
    renderBacklog({ path: '/p/ACME/items', provider });

    expect(await screen.findByRole('link', { name: 'Login with SSO' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Shipped already' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'All open' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    // The unfiltered list is never fetched on the way to the default: every
    // list call (milestones aside) already carries the pending statuses.
    const listCalls = listItems.mock.calls
      .map((call) => call[0])
      .filter((query) => query.type === undefined);
    expect(listCalls.length).toBeGreaterThan(0);
    for (const query of listCalls) expect(query.status).toEqual(openStatuses);
    expect(stored(ACME_KEY)).toEqual({ view: 'open' });
  });

  it('stores a filter change and restores it on the next visit', async () => {
    const user = userEvent.setup();
    const first = renderBacklog({ path: '/p/ACME/items', provider: providerWithDoneItem() });

    await user.click(await screen.findByLabelText('Backlog'));
    await waitFor(() => {
      expect(stored(ACME_KEY)).toEqual({ status: 'backlog' });
    });
    first.unmount();

    renderBacklog({ path: '/p/ACME/items', provider: providerWithDoneItem() });
    expect(await screen.findByRole('link', { name: 'Logout everywhere' })).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByRole('link', { name: 'Login with SSO' })).not.toBeInTheDocument();
    });
  });

  it('keeps each project on its own filter', async () => {
    localStorage.setItem(filterStorageKey('BETA'), JSON.stringify({ view: 'all' }));
    const provider = providerWithDoneItem();
    const listItems = vi.spyOn(provider, 'listItems');
    renderBacklog({ path: '/p/ACME/items', provider });

    await waitFor(() => {
      expect(stored(ACME_KEY)).toEqual({ view: 'open' });
    });
    expect(statusCalls(listItems)).toContainEqual(openStatuses);
    expect(stored(filterStorageKey('BETA'))).toEqual({ view: 'all' });
  });

  it('lets a filter in the URL win and stores it as the last filter', async () => {
    localStorage.setItem(ACME_KEY, JSON.stringify({ status: 'backlog' }));
    renderBacklog({ path: '/p/ACME/items?status=in_progress', provider: providerWithDoneItem() });

    expect(await screen.findByRole('link', { name: 'Login with SSO' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Logout everywhere' })).not.toBeInTheDocument();
    await waitFor(() => {
      expect(stored(ACME_KEY)).toEqual({ status: 'in_progress' });
    });
  });

  it('remembers clearing to everything, and resets back to the default', async () => {
    const user = userEvent.setup();
    const first = renderBacklog({ path: '/p/ACME/items', provider: providerWithDoneItem() });

    await user.click(await screen.findByRole('button', { name: /Clear filters/ }));
    expect(await screen.findByRole('link', { name: 'Shipped already' })).toBeInTheDocument();
    await waitFor(() => {
      expect(stored(ACME_KEY)).toEqual({ view: 'all' });
    });
    first.unmount();

    renderBacklog({ path: '/p/ACME/items', provider: providerWithDoneItem() });
    expect(await screen.findByRole('link', { name: 'Shipped already' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Reset to default/ }));
    await waitFor(() => {
      expect(screen.queryByRole('link', { name: 'Shipped already' })).not.toBeInTheDocument();
    });
    expect(stored(ACME_KEY)).toEqual({ view: 'open' });
    expect(screen.queryByRole('button', { name: /Reset to default/ })).not.toBeInTheDocument();
  });

  it.each([
    ['bad JSON', '{"status":'],
    ['an unknown status', JSON.stringify({ status: 'archived' })],
  ])('falls back to the default on %s without crashing', async (_label, raw) => {
    localStorage.setItem(ACME_KEY, raw);
    renderBacklog({ path: '/p/ACME/items', provider: providerWithDoneItem() });

    expect(await screen.findByRole('link', { name: 'Login with SSO' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Shipped already' })).not.toBeInTheDocument();
    await waitFor(() => {
      expect(stored(ACME_KEY)).toEqual({ view: 'open' });
    });
  });
});
