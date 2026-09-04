import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleTeam } from '@/api/fake-provider';
import { renderWithRouter } from '@/test/router';

import { BoardList } from './BoardList';

describe('BoardList', () => {
  it('lists the boards of the team repository', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: sampleTeam }) });

    const link = await screen.findByRole('link', { name: 'Delivery' });
    expect(link).toHaveAttribute('href', '/boards/delivery');
    const kanban = link.closest('li') as HTMLElement;
    expect(within(kanban).getByText('kanban')).toBeInTheDocument();
    expect(within(kanban).getByText('ACME')).toBeInTheDocument();
    expect(within(kanban).getByText('WEB')).toBeInTheDocument();
  });

  it('marks a scrum board with the sprint it runs', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: sampleTeam }) });

    const link = await screen.findByRole('link', { name: 'SSO Sprint Board' });
    const scrum = link.closest('li') as HTMLElement;
    expect(within(scrum).getByText('scrum')).toBeInTheDocument();
    expect(within(scrum).getByText('ACME-TEAM-S-0007')).toBeInTheDocument();
  });

  it('says so when no team repository is open', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: null }) });

    expect(await screen.findByText('No board to show')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Delivery' })).not.toBeInTheDocument();
  });
});
