import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import {
  FakeProvider,
  sampleBoard,
  sampleScrumBoard,
  sampleSprint,
  sampleTeam,
} from '@/api/fake-provider';
import { ToastProvider } from '@/components/ui/toast';
import { renderWithRouter } from '@/test/router';

import { BoardList } from './BoardList';
import { BoardCanvas } from './BoardView';
import { SprintList } from './SprintList';

/**
 * Creating, editing and deleting a board, and opening the first sprint of a
 * scrum board (story GIT-US-0032). The fake provider enforces the same
 * refusals the Go core does, so these are behaviour tests, not mock theatre.
 */

function boardProvider(overrides: Partial<ConstructorParameters<typeof FakeProvider>[0]> = {}) {
  return new FakeProvider({ team: sampleTeam, ...overrides });
}

function renderList(provider: FakeProvider) {
  return renderWithRouter({
    index: () => (
      <ToastProvider>
        <BoardList />
      </ToastProvider>
    ),
    provider,
  });
}

function renderBoard(provider: FakeProvider, slug: string) {
  return renderWithRouter({
    index: () => (
      <ToastProvider>
        <BoardCanvas slug={slug} />
      </ToastProvider>
    ),
    provider,
  });
}

describe('creating a board', () => {
  it('writes a board with the name, the kind and the scope the user chose', async () => {
    const user = userEvent.setup();
    const provider = boardProvider();
    renderList(provider);

    await user.click(await screen.findByRole('button', { name: 'New board' }));
    const form = await screen.findByRole('form', { name: 'New board' });

    await user.type(screen.getByLabelText('Board name'), 'Squad Delivery');
    await user.selectOptions(screen.getByLabelText('Kind'), 'scrum');
    // Unchecking "every project" lists them all, then WEB is dropped: the
    // scope narrows to ACME.
    await user.click(screen.getByLabelText('Every project the team declares'));
    await user.click(screen.getByLabelText('WEB'));
    await user.click(screen.getByLabelText('story'));
    await user.click(within(form).getByRole('button', { name: 'Create board' }));

    await waitFor(async () => {
      const boards = await provider.listBoards();
      expect(boards.map((b) => b.id)).toContain('squad-delivery');
    });
    const created = await provider.getBoard('squad-delivery');
    expect(created.kind).toBe('scrum');
    expect(created.projects).toEqual(['ACME']);
    expect(created.filters.types).toEqual(['story']);
    expect(created.backlogColumn).toBe('sprint_backlog');
  });

  it('refuses a name whose slug is already a board', async () => {
    const user = userEvent.setup();
    renderList(boardProvider());

    await user.click(await screen.findByRole('button', { name: 'New board' }));
    const form = await screen.findByRole('form', { name: 'New board' });
    await user.type(screen.getByLabelText('Board name'), sampleBoard.title);
    await user.click(within(form).getByRole('button', { name: 'Create board' }));

    expect(await screen.findByText('That board already exists')).toBeInTheDocument();
  });

  it('offers no create control on a read-only workspace', async () => {
    renderList(new FakeProvider({ team: sampleTeam }, { readOnly: true }));
    expect(await screen.findByRole('button', { name: 'New board' })).toBeDisabled();
  });
});

describe('editing a board', () => {
  it('widens the scope and the filters, which is how items reach a board', async () => {
    const user = userEvent.setup();
    const provider = boardProvider();
    renderBoard(provider, 'delivery');

    await user.click(await screen.findByRole('button', { name: 'Board settings' }));
    const form = await screen.findByRole('form', { name: 'Board settings' });
    // The board file lists two types; unchecking both shows every type.
    await user.click(screen.getByLabelText('story'));
    await user.click(screen.getByLabelText('task'));
    await user.click(screen.getByLabelText('epic'));
    await user.click(within(form).getByRole('button', { name: 'Save board' }));

    await waitFor(async () => {
      const saved = await provider.getBoard('delivery');
      expect(saved.filters.types).toEqual(['epic']);
    });
  });

  it('deletes a board behind a confirmation', async () => {
    const user = userEvent.setup();
    const provider = boardProvider();
    renderBoard(provider, 'delivery');

    await user.click(await screen.findByRole('button', { name: 'Board settings' }));
    await user.click(await screen.findByRole('button', { name: 'Delete board' }));
    await user.click(await screen.findByRole('button', { name: 'Delete board' }));

    await waitFor(async () => {
      const boards = await provider.listBoards();
      expect(boards.map((b) => b.id)).not.toContain('delivery');
    });
  });

  it('refuses to delete the board a sprint is running on', async () => {
    const user = userEvent.setup();
    const provider = boardProvider();
    renderBoard(provider, sampleScrumBoard.id);

    await user.click(await screen.findByRole('button', { name: 'Board settings' }));
    await user.click(await screen.findByRole('button', { name: 'Delete board' }));
    await user.click(await screen.findByRole('button', { name: 'Delete board' }));

    expect(await screen.findByText('A sprint still belongs to this board')).toBeInTheDocument();
    const boards = await provider.listBoards();
    expect(boards.map((b) => b.id)).toContain(sampleScrumBoard.id);
  });
});

describe('the first sprint of a scrum board', () => {
  /** The sample scrum board with its sprint detached: a brand-new board. */
  function emptyScrumProvider() {
    const board = { ...structuredClone(sampleScrumBoard) };
    delete board.sprint;
    return new FakeProvider({ team: sampleTeam, boards: [sampleBoard, board], sprints: [] });
  }

  it('offers to plan one instead of a dead end', async () => {
    const user = userEvent.setup();
    const provider = emptyScrumProvider();
    renderBoard(provider, sampleScrumBoard.id);

    expect(await screen.findByText('No sprint yet')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Plan the first sprint' }));

    const form = await screen.findByRole('form', { name: 'New sprint' });
    await user.type(screen.getByLabelText('Title'), 'Sprint 1');
    await user.type(screen.getByLabelText('Start'), '2026-09-07');
    await user.type(screen.getByLabelText('End'), '2026-09-20');
    await user.click(within(form).getByRole('button', { name: 'Create sprint' }));

    // The sprint exists and the board now points at it, which is what makes the
    // sprint panel render at all.
    await waitFor(async () => {
      const sprints = await provider.listSprints({ board: sampleScrumBoard.id });
      expect(sprints).toHaveLength(1);
    });
    await waitFor(async () => {
      const board = await provider.getBoard(sampleScrumBoard.id);
      expect(board.sprint).toBeDefined();
    });
  });

  it('points a board at a sprint that already exists from the sprint list', async () => {
    const user = userEvent.setup();
    const board = { ...structuredClone(sampleScrumBoard) };
    delete board.sprint;
    const provider = new FakeProvider({
      team: sampleTeam,
      boards: [sampleBoard, board],
      sprints: [sampleSprint],
    });
    renderWithRouter({
      index: () => (
        <ToastProvider>
          <SprintList />
        </ToastProvider>
      ),
      provider,
    });

    const button = await screen.findByRole('button', {
      name: `Show it on ${sampleScrumBoard.title}`,
    });
    await user.click(button);

    await waitFor(async () => {
      const updated = await provider.getBoard(sampleScrumBoard.id);
      expect(updated.sprint).toBe(sampleSprint.id);
    });
  });
});
