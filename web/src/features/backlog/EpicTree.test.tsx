import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { renderBacklog } from './test-utils';

describe('EpicTree', () => {
  it('lists epics with their stories and a roll-up', async () => {
    renderBacklog({ path: '/p/ACME/epics' });

    expect(await screen.findByRole('link', { name: 'ACME-EP-0001' })).toBeInTheDocument();
    expect(screen.getByText('Single sign-on')).toBeInTheDocument();

    // Epics start expanded, so their stories are visible.
    expect(screen.getByRole('link', { name: 'ACME-US-0042' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'ACME-US-0043' })).toBeInTheDocument();

    // Nothing under the epic is done yet.
    const bar = screen.getByRole('progressbar', { name: /Single sign-on/ });
    expect(bar).toHaveAttribute('aria-valuenow', '0');
    expect(bar).toHaveAttribute('aria-valuemax', '2');
  });

  it('collapses and expands a node', async () => {
    const user = userEvent.setup();
    renderBacklog({ path: '/p/ACME/epics' });

    const toggle = await screen.findByRole('button', { name: 'Collapse ACME-EP-0001' });
    await user.click(toggle);

    expect(screen.queryByRole('link', { name: 'ACME-US-0042' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Expand ACME-EP-0001' }));
    expect(screen.getByRole('link', { name: 'ACME-US-0042' })).toBeInTheDocument();
  });

  it('nests the tasks of a story under it', async () => {
    const user = userEvent.setup();
    renderBacklog({ path: '/p/ACME/epics' });

    await user.click(await screen.findByRole('button', { name: 'Expand ACME-US-0042' }));

    const row = screen.getByRole('link', { name: 'ACME-T-0107' }).closest('div');
    expect(within(row as HTMLElement).getByText('Add OIDC client')).toBeInTheDocument();
  });
});
