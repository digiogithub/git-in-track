import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import { ProviderError } from '@/api/provider';

import { renderBacklog } from './test-utils';

const STORY_REV = 'sha256:0000000000000042';

describe('ItemDetail', () => {
  it('shows the front matter, the links, the children and the comments', async () => {
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042' });

    expect(
      await screen.findByRole('heading', { name: 'Login with SSO', level: 1 }),
    ).toBeInTheDocument();

    // Front matter panel.
    expect(screen.getByText('Parent')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'ACME-EP-0001' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'ACME-M-0001' })).toBeInTheDocument();
    expect(screen.getByText('marta')).toBeInTheDocument();
    expect(screen.getByText('frontend')).toBeInTheDocument();
    expect(screen.getByText('8')).toBeInTheDocument();

    // Typed links carry the inverse label.
    expect(screen.getByText(/Blocked by/)).toBeInTheDocument();
    expect(screen.getByText(/target sees: Blocks/)).toBeInTheDocument();

    // Acceptance criteria progress: one of the two checkboxes is ticked.
    expect(screen.getByText('1 of 2 checked')).toBeInTheDocument();

    // Children (tasks of the story) with their status badge.
    const children = within(await screen.findByRole('list', { name: 'Child items' }));
    expect(children.getByRole('link', { name: 'ACME-T-0107' })).toBeInTheDocument();
    expect(children.getByText('Add OIDC client')).toBeInTheDocument();
    expect(children.getByText('To Do')).toBeInTheDocument();

    // Comments thread.
    const thread = within(await screen.findByRole('list', { name: 'Comment thread' }));
    expect(thread.getByText('Northwind is the pilot tenant.')).toBeInTheDocument();
    expect(thread.getByText('jose')).toBeInTheDocument();
  });

  it('renders the body through the Markdown pipeline', async () => {
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042' });

    expect(await screen.findByText('As an employee, I want SSO.')).toBeInTheDocument();
    expect(
      await screen.findByRole('heading', { name: /Acceptance Criteria/, level: 2 }),
    ).toBeInTheDocument();
  });

  it('moves the status through the provider with the revision on screen', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    const moveItem = vi.spyOn(provider, 'moveItem');
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    const select = await screen.findByLabelText('Status');
    await user.selectOptions(select, 'in_review');

    await waitFor(() => {
      expect(moveItem).toHaveBeenCalledWith('ACME-US-0042', 'in_review', STORY_REV);
    });
  });

  it('reports a stale revision instead of silently overwriting', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    vi.spyOn(provider, 'moveItem').mockRejectedValue(
      new ProviderError('stale_revision', 'Item ACME-US-0042 changed on disk'),
    );
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    const select = await screen.findByLabelText('Status');
    await user.selectOptions(select, 'in_review');

    expect(await screen.findByText('Changed on disk')).toBeInTheDocument();
    expect(await screen.findByText(/modified elsewhere/)).toBeInTheDocument();
  });

  it('disables the composer when the workspace is read-only', async () => {
    const provider = new FakeProvider({}, { readOnly: true });
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    expect(await screen.findByLabelText('Add a comment')).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Post comment' })).toBeDisabled();
    expect(screen.getByText(/read-only/i)).toBeInTheDocument();
  });
});
