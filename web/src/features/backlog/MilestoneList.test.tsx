import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { renderBacklog } from './test-utils';

describe('MilestoneList', () => {
  it('lists milestones with their due date and progress', async () => {
    renderBacklog({ path: '/p/ACME/milestones' });

    expect(await screen.findByRole('link', { name: 'ACME-M-0001' })).toBeInTheDocument();
    expect(screen.getByText('Public Beta')).toBeInTheDocument();
    expect(screen.getByText('Due 2026-11-15')).toBeInTheDocument();

    // One story points at the milestone and it is not done yet.
    const bar = screen.getByRole('progressbar', { name: /Public Beta/ });
    expect(bar).toHaveAttribute('aria-valuenow', '0');
    expect(bar).toHaveAttribute('aria-valuemax', '1');

    const link = screen.getByRole('link', { name: 'See its items' });
    expect(link).toHaveAttribute('href', '/p/ACME/items?milestone=ACME-M-0001');
  });
});
