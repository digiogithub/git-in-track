/**
 * TanStack Query hooks for team boards (docs/05-web-app.md §9).
 *
 * A board is a view over items that live in other repositories, so a move
 * invalidates both the board and the backlog of the project the card belongs
 * to. The move itself is optimistic: the card lands instantly, the previous
 * board snapshot is kept for rollback, and a failure puts the card back and
 * says why.
 */

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query';
import { useEffect } from 'react';

import type { BoardMoveResult, BoardSummary, BoardView, CardMove } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { backlogKeys } from '@/features/backlog/queries';

/** Key factory. Every board key lives under the `boards` prefix. */
export const boardKeys = {
  all: () => ['boards'] as const,
  list: () => ['boards', 'list'] as const,
  detail: (slug: string) => ['boards', 'detail', slug] as const,
};

export function useBoards() {
  const provider = useProvider();
  return useQuery<BoardSummary[]>({
    queryKey: boardKeys.list(),
    queryFn: () => provider.listBoards(),
  });
}

export function useBoard(slug: string) {
  const provider = useProvider();
  return useQuery<BoardView>({
    queryKey: boardKeys.detail(slug),
    queryFn: () => provider.getBoard(slug),
    enabled: slug.length > 0,
  });
}

/** Refetches the board when anything in the workspace changes underneath it. */
export function useBoardEvents(slug: string): void {
  const provider = useProvider();
  const queryClient = useQueryClient();

  useEffect(
    () =>
      provider.subscribe((event) => {
        if (event.kind === 'items' || event.kind === 'index' || event.kind === 'repo') {
          void queryClient.invalidateQueries({ queryKey: boardKeys.detail(slug) });
        }
      }),
    [provider, queryClient, slug],
  );
}

type MoveContext = { previous: BoardView | undefined };

/**
 * Moves a card, optimistically. The card is placed where the user dropped it
 * before the write lands; a rejected move (a stale revision, a WIP limit, a
 * remote card) restores the snapshot the user was looking at.
 */
export function useMoveCard(
  slug: string,
): UseMutationResult<BoardMoveResult, Error, CardMove, MoveContext> {
  const provider = useProvider();
  const queryClient = useQueryClient();

  return useMutation<BoardMoveResult, Error, CardMove, MoveContext>({
    mutationFn: (move) => provider.moveCard(move),
    onMutate: async (move) => {
      await queryClient.cancelQueries({ queryKey: boardKeys.detail(slug) });
      const previous = queryClient.getQueryData<BoardView>(boardKeys.detail(slug));
      if (previous) {
        queryClient.setQueryData(boardKeys.detail(slug), applyMoveToView(previous, move));
      }
      return { previous };
    },
    onError: (_error, _move, context) => {
      if (context?.previous) {
        queryClient.setQueryData(boardKeys.detail(slug), context.previous);
      }
    },
    onSuccess: (result) => {
      queryClient.setQueryData(boardKeys.detail(slug), result.board);
      if (result.item) {
        const project = result.item.id.split('-')[0] ?? '';
        void queryClient.invalidateQueries({ queryKey: backlogKeys.project(project) });
      }
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: boardKeys.detail(slug) });
    },
  });
}

/**
 * The optimistic half of a move: the card leaves its column and lands at the
 * requested position, and the WIP flags are recomputed so the header turns red
 * at the same moment the card arrives.
 */
export function applyMoveToView(view: BoardView, move: CardMove): BoardView {
  const card = view.columns.flatMap((column) => column.cards).find((c) => c.ref === move.ref);
  if (!card) return view;

  const target = view.columns.find((column) => column.id === move.toColumn);
  const status = move.status ?? target?.cards.find((c) => !c.remote)?.status ?? card.status;

  const columns = view.columns.map((column) => {
    const without = column.cards.filter((c) => c.ref !== move.ref);
    if (column.id !== move.toColumn) {
      return { ...column, cards: without, exceeded: overLimit(column.wip, without.length) };
    }
    const at = move.position < 0 || move.position > without.length ? without.length : move.position;
    const moved = { ...card, ...(status === undefined ? {} : { status }) };
    const cards = [...without.slice(0, at), moved, ...without.slice(at)];
    return { ...column, cards, exceeded: overLimit(column.wip, cards.length) };
  });
  return { ...view, columns };
}

function overLimit(wip: number | undefined, count: number): boolean {
  return wip !== undefined && wip > 0 && count > wip;
}
