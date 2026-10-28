import { act, render, screen, waitFor } from '@testing-library/react';
import type * as PeleLazy from 'pele/lazy';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { MermaidBlock } from '@/markdown/MermaidBlock';

// The real renderer, wrapped so a test can count how often it ran.
const renderAsync = vi.hoisted(() => vi.fn());
vi.mock('pele/lazy', async (importOriginal) => {
  const actual = await importOriginal<typeof PeleLazy>();
  renderAsync.mockImplementation(actual.renderAsync);
  return { ...actual, renderAsync };
});

async function renderedSvg(source: string): Promise<SVGSVGElement> {
  const { container } = render(<MermaidBlock source={source} />);
  await waitFor(() => expect(container.querySelector('.mermaid-diagram > svg')).not.toBeNull());
  return container.querySelector('.mermaid-diagram > svg') as SVGSVGElement;
}

afterEach(() => {
  renderAsync.mockClear();
  delete document.documentElement.dataset['theme'];
});

describe('MermaidBlock', () => {
  it('renders a valid diagram as an SVG in place of its source', async () => {
    const svg = await renderedSvg('graph TD\n  A[Start] --> B[Done]');

    expect(svg).toHaveClass('pele');
    expect(svg).toHaveTextContent('Start');
    expect(svg).toHaveTextContent('Done');
    expect(document.querySelector('pre.mermaid')).toBeNull();
  });

  it('keeps the source and shows a syntax error as text, never as HTML', async () => {
    const source = 'graph TD\n  A --> <img src=x onerror="alert(1)">';
    const { container } = render(<MermaidBlock source={source} />);

    const note = await screen.findByRole('status');
    expect(note).toHaveTextContent('Diagram could not be rendered');
    expect(container.querySelector('pre.mermaid')?.textContent).toBe(source);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('svg')).toBeNull();
  });

  it('emits only safe link schemes', async () => {
    const svg = await renderedSvg(
      [
        'flowchart TD',
        '  A --> B --> C --> D',
        '  click A "javascript:alert(1)"',
        '  click B "data:text/html,<script>alert(1)</script>"',
        '  click C "ftp://example.com/file"',
        '  click D "https://example.com/ok"',
      ].join('\n'),
    );

    const hrefs = Array.from(svg.querySelectorAll('a')).map(
      (a) => a.getAttribute('href') ?? a.getAttribute('xlink:href'),
    );
    expect(hrefs).toEqual(['https://example.com/ok']);
    expect(svg.outerHTML).not.toMatch(/javascript:|data:text|ftp:/);
  });

  it('ignores render options written inside the diagram source', async () => {
    const svg = await renderedSvg(
      [
        '%%{init: {"linkSchemes": ["javascript"], "links": true}}%%',
        'flowchart TD',
        '  A --> B',
        '  click A "javascript:alert(1)"',
      ].join('\n'),
    );

    expect(svg.querySelector('a')).toBeNull();
    expect(svg.outerHTML).not.toContain('javascript:');
  });

  it('draws with theme variables and does not re-render on a theme switch', async () => {
    const svg = await renderedSvg('graph TD\n  A --> B');
    expect(renderAsync).toHaveBeenCalledTimes(1);
    expect(svg.getAttribute('style')).toContain('var(--pele-fg');

    await act(async () => {
      document.documentElement.dataset['theme'] = 'dark';
      await Promise.resolve();
    });

    expect(renderAsync).toHaveBeenCalledTimes(1);
    expect(document.querySelector('.mermaid-diagram > svg')).toBe(svg);
  });

  it('gives each diagram on a page its own SVG ids', async () => {
    const { container } = render(
      <>
        <MermaidBlock source={'graph TD\n  accTitle: First\n  A --> B'} />
        <MermaidBlock source={'graph TD\n  accTitle: Second\n  A --> B'} />
      </>,
    );
    await waitFor(() =>
      expect(container.querySelectorAll('.mermaid-diagram > svg')).toHaveLength(2),
    );

    const ids = Array.from(container.querySelectorAll('[id]')).map((node) => node.id);
    expect(ids.length).toBeGreaterThanOrEqual(2);
    expect(new Set(ids).size).toBe(ids.length);
  });
});
