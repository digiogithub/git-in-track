import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import type { ConflictAnalysis } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';
import { ConflictResolver } from '@/features/sync/ConflictResolver';

/** The analysis both runtimes return for a conflicted story. */
function analysis(): ConflictAnalysis {
  return {
    repo: 'demo',
    path: 'docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md',
    kind: 'content',
    operation: 'rebase',
    strategy: 'rebase',
    versions: {
      path: 'docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md',
      kind: 'content',
      base: 'base',
      ours: 'mine',
      theirs: 'theirs',
      hasBase: true,
      hasOurs: true,
      hasTheirs: true,
      rebased: true,
      binary: false,
    },
    merge: {
      path: 'docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md',
      structured: true,
      fields: [
        {
          field: 'status',
          kind: 'scalar',
          ours: 'in_progress',
          theirs: 'done',
          merged: 'done',
          choice: 'theirs',
          review: true,
          note: 'both sides changed it; the remote value was kept — check this',
        },
        {
          field: 'labels',
          kind: 'set',
          ours: ['frontend', 'mine'],
          theirs: ['frontend', 'checkout'],
          merged: ['checkout', 'frontend', 'mine'],
          choice: 'merged',
          review: false,
          note: 'additions from both sides kept',
        },
      ],
      hunks: [
        {
          index: 0,
          section: '## Description',
          base: 'One.',
          ours: 'Mine.',
          theirs: 'Theirs.',
          merged: 'Mine.',
          choice: 'ours',
          conflicted: true,
          suggestion: 'ours',
          note: 'both sides changed this region',
        },
      ],
      content: '---\nstatus: done\n---\n\nMine.\n',
      conflicted: 1,
      review: 1,
      clean: false,
    },
  };
}

/** Renders the resolver against a provider that holds one conflict. */
function renderResolver(): FakeProvider {
  const provider = new FakeProvider();
  const conflict = analysis();
  provider.conflicts.set(`demo:${conflict.path}`, conflict);
  render(
    <ProviderContext.Provider value={provider}>
      <ConflictResolver repoId="demo" path={conflict.path} />
    </ProviderContext.Provider>,
  );
  return provider;
}

describe('ConflictResolver', () => {
  it('shows the front matter field by field with the automatic decision and its reason', async () => {
    renderResolver();

    expect(await screen.findByText('status')).toBeInTheDocument();
    expect(screen.getByText('labels')).toBeInTheDocument();
    // Both sides' values are visible, so no field can be dropped unseen.
    expect(screen.getByText('in_progress')).toBeInTheDocument();
    expect(screen.getAllByText('done').length).toBeGreaterThan(0);
    expect(screen.getByText('checkout, frontend, mine')).toBeInTheDocument();
    // An automatic decision that needed judgement is flagged for review.
    expect(screen.getByText('review')).toBeInTheDocument();
    expect(screen.getByText(/additions from both sides kept/)).toBeInTheDocument();
  });

  it('shows the body hunk three ways with the heading it falls under', async () => {
    renderResolver();

    expect(await screen.findByText('## Description')).toBeInTheDocument();
    expect(screen.getAllByText('Mine.').length).toBeGreaterThan(0);
    expect(screen.getByText('Theirs.')).toBeInTheDocument();
    expect(screen.getByText('One.')).toBeInTheDocument();
    expect(screen.getByText('both sides changed this')).toBeInTheDocument();
  });

  it('sends the per-field and per-hunk decisions when the merge is accepted', async () => {
    const provider = renderResolver();
    const user = userEvent.setup();

    await screen.findByText('status');
    await user.click(screen.getByRole('button', { name: 'Take theirs' }));
    // Flipping the status row back to ours is the "auto-merged but overridable"
    // rule of docs/06 §5.2.
    const statusRow = screen.getByText('status').closest('tr');
    expect(statusRow).not.toBeNull();
    await user.click(
      screen.getAllByRole('button', { name: 'Mine' }).find((button) => statusRow?.contains(button))!,
    );
    await user.click(screen.getByRole('button', { name: 'Accept merged' }));

    await waitFor(() => {
      expect(provider.resolutions).toHaveLength(1);
    });
    const [recorded] = provider.resolutions;
    expect(recorded?.resolution.resolution).toBe('merged');
    expect(recorded?.resolution.fields).toEqual({ status: 'ours' });
    expect(recorded?.resolution.hunks).toEqual({ '0': 'theirs' });
  });

  it('offers keep mine and keep theirs for every conflict', async () => {
    const provider = renderResolver();
    const user = userEvent.setup();

    await screen.findByRole('button', { name: 'Keep theirs' });
    await user.click(screen.getByRole('button', { name: 'Keep theirs' }));

    await waitFor(() => {
      expect(provider.resolutions).toHaveLength(1);
    });
    expect(provider.resolutions[0]?.resolution.resolution).toBe('theirs');
    expect(await screen.findByRole('status')).toHaveTextContent(/Resolved/);
  });

  it('sends the edited file when the user writes the resolution themselves', async () => {
    const provider = renderResolver();
    const user = userEvent.setup();

    await screen.findByRole('button', { name: 'Edit manually' });
    await user.click(screen.getByRole('button', { name: 'Edit manually' }));
    const editor = screen.getByLabelText('Merged file');
    await user.clear(editor);
    await user.type(editor, 'hand written');
    await user.click(screen.getByRole('button', { name: 'Accept my edit' }));

    await waitFor(() => {
      expect(provider.resolutions).toHaveLength(1);
    });
    expect(provider.resolutions[0]?.resolution.resolution).toBe('manual');
    expect(provider.resolutions[0]?.resolution.content).toBe('hand written');
  });

  it('refuses to accept a merge while a hunk has no decision', async () => {
    renderResolver();

    expect(await screen.findByRole('button', { name: 'Accept merged' })).toBeDisabled();
    expect(screen.getByText(/hunk\(s\) still need a decision/)).toBeInTheDocument();
  });

  it('explains that a binary conflict has only the two escape hatches', async () => {
    const provider = new FakeProvider();
    const binary = analysis();
    binary.versions.binary = true;
    delete binary.merge;
    provider.conflicts.set(`demo:${binary.path}`, binary);
    render(
      <ProviderContext.Provider value={provider}>
        <ConflictResolver repoId="demo" path={binary.path} />
      </ProviderContext.Provider>,
    );

    expect(await screen.findByText(/binary/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Keep mine' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Keep theirs' })).toBeEnabled();
  });
});
