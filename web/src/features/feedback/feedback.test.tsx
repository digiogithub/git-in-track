import { act, renderHook } from '@testing-library/react';
import type { Element as HastElement, Root } from 'hast';
import { afterEach, describe, expect, it } from 'vitest';

import {
  feedbackKey,
  resetFeedbackCache,
  useFeedbackDraft,
  type FeedbackNote,
} from '@/features/feedback/feedback-store';
import { formatFeedbackComment } from '@/features/feedback/format';
import { lineLabel, locateQuote, readSelection } from '@/features/feedback/selection';
import { clearMarkdownCache, renderMarkdown } from '@/markdown';

function elements(root: Root): HastElement[] {
  const out: HastElement[] = [];
  const walk = (node: Root | HastElement) => {
    for (const child of node.children) {
      if (child.type === 'element') {
        out.push(child);
        walk(child);
      }
    }
  };
  walk(root);
  return out;
}

afterEach(() => {
  localStorage.clear();
  resetFeedbackCache();
  document.getSelection()?.removeAllRanges();
  document.body.innerHTML = '';
});

describe('source lines', () => {
  const source = '# Title\n\nFirst line\nsecond line\n\n- one\n- two\n';

  it('stamps the source range of every block when asked', async () => {
    clearMarkdownCache();
    const result = await renderMarkdown(source, { sourceLines: true });
    const paragraph = elements(result.root).find((node) => node.tagName === 'p');
    expect(paragraph?.properties['dataLineStart']).toBe('3');
    expect(paragraph?.properties['dataLineEnd']).toBe('4');
    const items = elements(result.root).filter((node) => node.tagName === 'li');
    expect(items.map((node) => node.properties['dataLineStart'])).toEqual(['6', '7']);
  });

  it('leaves the tree alone by default', async () => {
    clearMarkdownCache();
    const result = await renderMarkdown(source);
    expect(elements(result.root).some((node) => 'dataLineStart' in node.properties)).toBe(false);
  });
});

describe('selection', () => {
  it('maps a selection to the lines of the blocks it spans', () => {
    const container = document.createElement('div');
    container.innerHTML =
      '<p data-line-start="3" data-line-end="4">Hello brave world</p>' +
      '<p data-line-start="6" data-line-end="6">Second block</p>';
    document.body.append(container);
    const [first, second] = [...container.querySelectorAll('p')];
    const range = document.createRange();
    range.setStart(first?.firstChild as Text, 6);
    range.setEnd(second?.firstChild as Text, 6);
    document.getSelection()?.addRange(range);

    const anchor = readSelection(container, '');
    expect(anchor).toMatchObject({ quote: 'brave worldSecond', startLine: 3, endLine: 6 });
  });

  it('ignores a selection outside the container', () => {
    const container = document.createElement('div');
    const outside = document.createElement('p');
    outside.textContent = 'elsewhere';
    document.body.append(container, outside);
    const range = document.createRange();
    range.selectNodeContents(outside);
    document.getSelection()?.addRange(range);
    expect(readSelection(container, '')).toBeNull();
  });

  it('finds a quote in the source when no block is stamped', () => {
    const source = '# Title\n\nSome *emphasised* text\nthat wraps.\n';
    expect(locateQuote('emphasised text that wraps', source)).toEqual({ start: 3, end: 4 });
    expect(locateQuote('missing', source)).toBeUndefined();
  });

  it('labels line ranges', () => {
    expect(lineLabel(3, 5)).toBe('Lines 3–5');
    expect(lineLabel(3, 3)).toBe('Line 3');
    expect(lineLabel()).toBe('');
  });
});

describe('feedback comment', () => {
  it('quotes every note with its lines', () => {
    const notes: FeedbackNote[] = [
      {
        id: 'a',
        quote: 'Use OIDC',
        startLine: 2,
        endLine: 3,
        note: 'Which provider?',
        created: '',
      },
      { id: 'b', quote: 'Northwind', note: 'Confirm the tenant.', created: '' },
    ];
    expect(formatFeedbackComment(notes)).toBe(
      [
        '**Feedback** · 2 notes',
        '---',
        '**1.** Description, lines 2–3',
        '> Use OIDC',
        'Which provider?',
        '---',
        '**2.** Description',
        '> Northwind',
        'Confirm the tenant.',
      ].join('\n\n'),
    );
  });
});

describe('feedback drafts', () => {
  const item = { kind: 'item', project: 'ACME', ref: 'ACME-US-0042' } as const;
  const page = { kind: 'kb', project: 'ACME', ref: 'docs/overview.md' } as const;

  it('keeps each target in its own storage key and restores it', () => {
    const first = renderHook(() => useFeedbackDraft(item));
    const second = renderHook(() => useFeedbackDraft(page));
    act(() => first.result.current.addNote({ quote: 'x', note: 'on the item' }));
    act(() => second.result.current.addNote({ quote: 'y', note: 'on the page' }));

    expect(localStorage.getItem(feedbackKey(item))).toContain('on the item');
    expect(localStorage.getItem(feedbackKey(page))).toContain('on the page');

    // A reload reads the drafts back, mode included.
    resetFeedbackCache();
    const restored = renderHook(() => useFeedbackDraft(item));
    expect(restored.result.current.draft.active).toBe(true);
    expect(restored.result.current.draft.notes.map((note) => note.note)).toEqual(['on the item']);
  });

  it('forgets the draft once it is cleared', () => {
    const { result } = renderHook(() => useFeedbackDraft(item));
    act(() => result.current.addNote({ quote: 'x', note: 'n' }));
    act(() => result.current.clear());
    expect(localStorage.getItem(feedbackKey(item))).toBeNull();
    expect(result.current.draft).toEqual({ active: false, notes: [] });
  });
});
