import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleTeam } from '@/api/fake-provider';
import { renderWithRouter } from '@/test/router';

import { BoardList } from './BoardList';

describe('BoardList', () => {
  it('lists the boards of the team repository', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: sampleTeam }) });

    const link = await screen.findByRole('link', { name: 'Delivery' });
    expect(link).toHaveAttribute('href', '/boards/delivery');
    expect(screen.getByText('kanban')).toBeInTheDocument();
    expect(screen.getByText('ACME')).toBeInTheDocument();
    expect(screen.getByText('WEB')).toBeInTheDocument();
  });

  it('says so when no team repository is open', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: null }) });

    expect(await screen.findByText('No board to show')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Delivery' })).not.toBeInTheDocument();
  });
});
