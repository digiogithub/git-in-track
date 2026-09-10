import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { FakeProvider, sampleComments } from '@/api/fake-provider';
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
    expect(await thread.findByText('Northwind is the pilot tenant.')).toBeInTheDocument();
    expect(thread.getByText('jose')).toBeInTheDocument();
  });

  it('renders comment bodies as Markdown', async () => {
    const provider = new FakeProvider({
      comments: [
        {
          ...sampleComments[0]!,
          body: 'Pilot is **Northwind**.\n\n- first\n- second\n\nSee [[ACME-EP-0001]].',
          // A fresh rev, so the render cache does not serve the sample body.
          rev: 'sha256:00000000000000c2',
        },
      ],
    });
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    const thread = within(await screen.findByRole('list', { name: 'Comment thread' }));
    const strong = await thread.findByText('Northwind');
    expect(strong.tagName).toBe('STRONG');
    expect(thread.getAllByRole('listitem').map((li) => li.textContent)).toEqual(
      expect.arrayContaining(['first', 'second']),
    );
    expect(thread.getByRole('link', { name: 'ACME-EP-0001' })).toHaveAttribute(
      'href',
      '/p/ACME/items/ACME-EP-0001',
    );
    expect(thread.queryByText(/\*\*Northwind\*\*/)).not.toBeInTheDocument();
  });

  it('creates a task under the story on screen', async () => {
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042' });

    const link = await screen.findByRole('link', { name: 'New task in ACME-US-0042' });
    expect(link.getAttribute('href')).toContain('/p/ACME/items/new');
    expect(link.getAttribute('href')).toContain('type=task');
    expect(link.getAttribute('href')).toContain('parent=ACME-US-0042');
  });

  it('creates a story under the epic on screen', async () => {
    renderBacklog({ path: '/p/ACME/items/ACME-EP-0001' });

    const link = await screen.findByRole('link', { name: 'New story in ACME-EP-0001' });
    expect(link.getAttribute('href')).toContain('type=story');
    expect(link.getAttribute('href')).toContain('parent=ACME-EP-0001');
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
  it('ticks an acceptance criterion from the detail view', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    const setTaskItem = vi.spyOn(provider, 'setTaskItem');
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    expect(await screen.findByText('1 of 2 checked')).toBeInTheDocument();

    // "- [ ] PKCE flow" is line 8 of the body; the renderer stamps that line on
    // the checkbox and the core rewrites exactly it.
    const pending = await screen.findByRole('checkbox', { name: 'Toggle task on line 8' });
    expect(pending).not.toBeChecked();
    await user.click(pending);

    await waitFor(() => {
      expect(setTaskItem).toHaveBeenCalledWith('ACME-US-0042', 8, true, STORY_REV);
    });
    expect(await screen.findByText('2 of 2 checked')).toBeInTheDocument();

    const saved = await provider.getItem('ACME-US-0042');
    expect(saved.body).toContain('- [x] PKCE flow');
    expect(saved.body).toContain('- [x] Button shown');
    expect(saved.body).toContain('As an employee, I want SSO.');
  });

  it('reports a stale revision when a criterion is ticked on a stale body', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    vi.spyOn(provider, 'setTaskItem').mockRejectedValue(
      new ProviderError('stale_revision', 'Item ACME-US-0042 changed on disk'),
    );
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    await user.click(await screen.findByRole('checkbox', { name: 'Toggle task on line 8' }));

    expect(await screen.findByText('Changed on disk')).toBeInTheDocument();
  });

  it('leaves the checkboxes read-only in a read-only workspace', async () => {
    const provider = new FakeProvider({}, { readOnly: true });
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    const boxes = await screen.findAllByRole('checkbox');
    for (const box of boxes) expect(box).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Delete' })).toBeDisabled();
  });

  it('lists what points at an item before deleting it', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    const deleteItem = vi.spyOn(provider, 'deleteItem');
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    await user.click(await screen.findByRole('button', { name: 'Delete' }));

    const dialog = within(await screen.findByRole('alertdialog', { name: /Delete ACME-US-0042/ }));
    const references = within(await dialog.findByRole('list', { name: 'Inbound references' }));
    expect(references.getByRole('link', { name: 'ACME-T-0107' })).toBeInTheDocument();
    expect(references.getByText(/parent/)).toBeInTheDocument();
    expect(dialog.getByText(/this one as their parent/)).toBeInTheDocument();
    // Nothing is written until the confirmation.
    expect(deleteItem).not.toHaveBeenCalled();

    await user.click(dialog.getByRole('button', { name: 'Delete item' }));

    await waitFor(() => {
      expect(deleteItem).toHaveBeenCalledWith('ACME-US-0042', STORY_REV, {});
    });
    // Soft delete: the file stays, flagged, so nothing that referenced it dangles.
    const deleted = await provider.getItem('ACME-US-0042');
    expect(deleted.deleted).toBe(true);
  });

  it('closes the delete dialog without writing when it is cancelled', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    const deleteItem = vi.spyOn(provider, 'deleteItem');
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    await user.click(await screen.findByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('alertdialog', { name: /Delete ACME-US-0042/ });
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    });
    expect(deleteItem).not.toHaveBeenCalled();
  });
});
