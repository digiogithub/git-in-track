import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { MarkdownEditor } from '@/components/editor/MarkdownEditor';

// CodeMirror measures the document; jsdom implements neither of these.
beforeAll(() => {
  const emptyRects: DOMRectList = Object.assign(
    [] as unknown as DOMRectList,
    { item: () => null },
  );
  Range.prototype.getClientRects = () => emptyRects;
  Range.prototype.getBoundingClientRect = () => new DOMRect();
});

function content(): string {
  return document.querySelector('.cm-content')?.textContent ?? '';
}

describe('MarkdownEditor', () => {
  it('mounts CodeMirror with the controlled value', async () => {
    render(<MarkdownEditor value="# Title\n\nBody text" onChange={vi.fn()} label="Item body" />);

    await waitFor(() => {
      expect(content()).toContain('Body text');
    });
    expect(screen.getByRole('textbox', { name: 'Item body' })).toBeInTheDocument();
  });

  it('replaces the document when the value prop changes', async () => {
    const { rerender } = render(
      <MarkdownEditor value="first" onChange={vi.fn()} label="Item body" />,
    );
    await waitFor(() => {
      expect(content()).toContain('first');
    });

    rerender(<MarkdownEditor value="second" onChange={vi.fn()} label="Item body" />);
    await waitFor(() => {
      expect(content()).toContain('second');
    });
  });

  it('applies a toolbar command and flushes the debounced change', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<MarkdownEditor value="Hello" onChange={onChange} label="Item body" />);

    await user.click(screen.getByRole('button', { name: 'Bold' }));

    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith('****Hello');
    });
  });

  it('toggles the split preview', async () => {
    const user = userEvent.setup();
    render(<MarkdownEditor value="Hello" onChange={vi.fn()} label="Item body" />);

    expect(screen.queryByTestId('markdown-preview')).not.toBeInTheDocument();
    const toggle = screen.getByRole('button', { name: /preview/i });
    await user.click(toggle);

    expect(await screen.findByTestId('markdown-preview')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Hide preview' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('offers the whole formatting toolbar', () => {
    render(<MarkdownEditor value="" onChange={vi.fn()} label="Item body" />);
    for (const name of ['Bold', 'Italic', 'Heading', 'Task list item', 'Link', 'Code']) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument();
    }
  });

  it('does not write while read-only', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<MarkdownEditor value="Hello" onChange={onChange} label="Item body" readOnly />);

    await user.click(screen.getByRole('button', { name: 'Bold' }));
    expect(onChange).not.toHaveBeenCalled();
  });
});
