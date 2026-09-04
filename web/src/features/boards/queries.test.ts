import { describe, expect, it } from 'vitest';

import type { BoardCard, BoardView, CardMove } from '@/api/provider';

import { applyMoveToView } from './queries';

function card(ref: string, overrides: Partial<BoardCard> = {}): BoardCard {
  const [project = '', item = ''] = ref.split('/');
  return { ref, project, item, declared: true, remote: false, status: 'todo', ...overrides };
}

function view(): BoardView {
  return {
    id: 'delivery',
    kind: 'kanban',
    title: 'Delivery',
    path: '.pmngr/boards/delivery.md',
    rev: 'sha256:0000000000000001',
    projects: ['ACME'],
    filters: {},
    swimlanes: {},
    card: {},
    columns: [
      {
        id: 'todo',
        name: 'To Do',
        cards: [card('ACME/ACME-T-0001'), card('ACME/ACME-T-0002')],
        exceeded: false,
      },
      {
        id: 'doing',
        name: 'Doing',
        wip: 1,
        cards: [card('ACME/ACME-T-0003', { status: 'in_progress' })],
        exceeded: false,
      },
    ],
    unmapped: [],
    diagnostics: [],
  };
}

const base: Omit<CardMove, 'ref' | 'toColumn' | 'position'> = { board: 'delivery' };

describe('applyMoveToView', () => {
  const cases: { name: string; move: CardMove; check: (next: BoardView) => void }[] = [
    {
      name: 'moves a card between columns at the requested position',
      move: { ...base, ref: 'ACME/ACME-T-0002', toColumn: 'doing', position: 0 },
      check: (next) => {
        expect(next.columns[0]?.cards.map((c) => c.ref)).toEqual(['ACME/ACME-T-0001']);
        expect(next.columns[1]?.cards.map((c) => c.ref)).toEqual([
          'ACME/ACME-T-0002',
          'ACME/ACME-T-0003',
        ]);
      },
    },
    {
      name: 'marks the target column over its limit as the card lands',
      move: { ...base, ref: 'ACME/ACME-T-0002', toColumn: 'doing', position: 0 },
      check: (next) => {
        expect(next.columns[1]?.exceeded).toBe(true);
        expect(next.columns[0]?.exceeded).toBe(false);
      },
    },
    {
      name: 'reorders inside one column without touching the others',
      move: { ...base, ref: 'ACME/ACME-T-0002', toColumn: 'todo', position: 0 },
      check: (next) => {
        expect(next.columns[0]?.cards.map((c) => c.ref)).toEqual([
          'ACME/ACME-T-0002',
          'ACME/ACME-T-0001',
        ]);
        expect(next.columns[1]?.cards.map((c) => c.ref)).toEqual(['ACME/ACME-T-0003']);
      },
    },
    {
      name: 'clamps a position past the end of the column',
      move: { ...base, ref: 'ACME/ACME-T-0001', toColumn: 'doing', position: 99 },
      check: (next) => {
        expect(next.columns[1]?.cards.map((c) => c.ref)).toEqual([
          'ACME/ACME-T-0003',
          'ACME/ACME-T-0001',
        ]);
      },
    },
    {
      name: 'leaves the board alone for a card it does not show',
      move: { ...base, ref: 'ACME/ACME-T-9999', toColumn: 'doing', position: 0 },
      check: (next) => {
        expect(next.columns[0]?.cards).toHaveLength(2);
        expect(next.columns[1]?.cards).toHaveLength(1);
      },
    },
  ];

  for (const { name, move, check } of cases) {
    it(name, () => {
      check(applyMoveToView(view(), move));
    });
  }
});
