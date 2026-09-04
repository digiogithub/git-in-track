import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleTeam } from '@/api/fake-provider';
import { renderWithRouter } from '@/test/router';

import { TeamPanel } from './TeamPanel';

describe('TeamPanel', () => {
  it('renders nothing when no team repository is open', async () => {
    renderWithRouter({ index: TeamPanel, provider: new FakeProvider({ repos: [] }) });

    // The panel resolves to null: the heading never appears.
    await expect(
      screen.findByRole('heading', { name: 'Team' }, { timeout: 1000 }),
    ).rejects.toThrow();
  });

  it('shows the team, its members and its knowledge base', async () => {
    renderWithRouter({ index: TeamPanel, provider: new FakeProvider({ team: sampleTeam }) });

    expect(
      await screen.findByText('ACME Delivery Team', undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.getByText('ACME-TEAM')).toBeInTheDocument();
    expect(screen.getByText(/Members \(2 active\)/)).toBeInTheDocument();
    expect(screen.getByText(/Jose Ruiz · lead/)).toBeInTheDocument();
    expect(screen.getByText(/Laura Prat · dev · inactive/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /team knowledge base/i })).toHaveAttribute(
      'href',
      '/p/ACME-TEAM/kb',
    );
  });

  it('marks a project nobody cloned instead of hiding it', async () => {
    renderWithRouter({ index: TeamPanel, provider: new FakeProvider({ team: sampleTeam }) });

    expect(
      await screen.findByText('Marketing Website', undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.getByText('not cloned')).toBeInTheDocument();
    expect(screen.getByText(/https:\/\/gitlab.com\/acme\/website.git/)).toBeInTheDocument();

    // The cloned one is browsable; the remote one offers no backlog link.
    expect(screen.getByRole('link', { name: /ACME backlog/ })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /WEB backlog/ })).not.toBeInTheDocument();
  });

  it('reports a malformed team.yaml instead of swallowing it', async () => {
    const provider = new FakeProvider({
      team: {
        ...sampleTeam,
        diagnostics: [
          { code: 'E-TEAM-KEY-DUP', severity: 'error', message: 'duplicate project key "ACME"' },
        ],
      },
    });
    renderWithRouter({ index: TeamPanel, provider });

    const alert = await screen.findByRole('alert', undefined, { timeout: 5000 });
    expect(alert).toHaveTextContent('E-TEAM-KEY-DUP');
    expect(alert).toHaveTextContent('duplicate project key');
  });
});
