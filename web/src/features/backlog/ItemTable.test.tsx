import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { FakeProvider, sampleItems } from '@/api/fake-provider';

import { renderBacklog } from './test-utils';

describe('ItemTable', () => {
  it('renders the sample items with their key fields', async () => {
    renderBacklog({ path: '/p/ACME/items' });

    expect(await screen.findByRole('link', { name: 'ACME-US-0042' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Login with SSO' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Single sign-on' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Public Beta' })).toBeInTheDocument();

    // Columns required by the story.
    for (const column of ['ID', 'Type', 'Title', 'Status', 'Priority', 'Estimate', 'Updated']) {
      expect(screen.getByRole('columnheader', { name: new RegExp(column, 'i') })).toBeVisible();
    }

    const row = screen.getByRole('link', { name: 'ACME-US-0042' }).closest('tr');
    expect(row).not.toBeNull();
    const cells = within(row as HTMLElement);
    expect(cells.getByText('In Progress')).toBeInTheDocument();
    expect(cells.getByText('high')).toBeInTheDocument();
    expect(cells.getByText('marta')).toBeInTheDocument();
    expect(cells.getByText('8')).toBeInTheDocument();
  });

  it('filters by status taken from the URL search params', async () => {
    renderBacklog({ path: '/p/ACME/items?status=backlog' });

    expect(await screen.findByRole('link', { name: 'Logout everywhere' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Login with SSO' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Public Beta' })).not.toBeInTheDocument();
  });

  it('combines a type filter and a full-text query from the URL', async () => {
    renderBacklog({ path: '/p/ACME/items?type=story,task&q=sso' });

    expect(await screen.findByRole('link', { name: 'Login with SSO' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Single sign-on' })).not.toBeInTheDocument();
  });

  it('moves the selected items in bulk with their current revision', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    const moveItem = vi.spyOn(provider, 'moveItem');
    renderBacklog({ path: '/p/ACME/items?status=backlog', provider });

    await user.click(await screen.findByRole('checkbox', { name: 'Select ACME-US-0043' }));

    await user.selectOptions(screen.getByLabelText('Move status to'), 'todo');
    await user.click(screen.getByRole('button', { name: 'Move' }));
    await user.click(await screen.findByRole('button', { name: 'Move items' }));

    const expected = sampleItems.find((item) => item.id === 'ACME-US-0043');
    await waitFor(() => {
      expect(moveItem).toHaveBeenCalledWith('ACME-US-0043', 'todo', expected?.rev);
    });
  });
});
