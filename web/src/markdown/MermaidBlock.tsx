/**
 * Mermaid, never at parse time (docs/05-web-app.md §7, "Mermaid").
 *
 * The block starts as the diagram source in a `<pre>`. On first visibility it
 * dynamically imports `mermaid` — its own chunk, downloaded only by pages that
 * actually contain a diagram — renders to SVG in a detached container and swaps
 * it in. A syntax error keeps the source visible with an error note, and a
 * theme change re-renders.
 *
 * `securityLevel: 'strict'` makes Mermaid sanitise its own output and disables
 * click handlers and inline HTML in diagram labels.
 */

import { useEffect, useRef, useState } from 'react';

import { useThemeMode } from '@/markdown/theme';

let sequence = 0;

export type MermaidBlockProps = {
  source: string;
  className?: string;
};

export function MermaidBlock({ source, className }: MermaidBlockProps) {
  const theme = useThemeMode();
  const container = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(() => typeof IntersectionObserver === 'undefined');
  const [svg, setSvg] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (visible || typeof IntersectionObserver === 'undefined') return;
    const node = container.current;
    if (!node) return;

    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) setVisible(true);
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [visible]);

  useEffect(() => {
    if (!visible) return;
    let cancelled = false;
    sequence += 1;
    const id = `mermaid-${sequence}`;

    const run = async () => {
      try {
        const mermaid = (await import('mermaid')).default;
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: 'strict',
          theme: theme === 'dark' ? 'dark' : 'default',
        });
        const rendered = await mermaid.render(id, source);
        if (!cancelled) {
          setSvg(rendered.svg);
          setError(null);
        }
      } catch (cause) {
        if (!cancelled) {
          setSvg(null);
          setError(cause instanceof Error ? cause.message : String(cause));
        }
      }
    };

    void run();
    return () => {
      cancelled = true;
      document.getElementById(`d${id}`)?.remove();
    };
  }, [source, theme, visible]);

  if (svg && !error) {
    return (
      <div
        ref={container}
        className={className ?? 'mermaid-diagram'}
        role="img"
        // Mermaid ran with `securityLevel: 'strict'`, which sanitises the SVG
        // it returns; the diagram source itself already passed our sanitiser.
        dangerouslySetInnerHTML={{ __html: svg }}
      />
    );
  }

  return (
    <div ref={container} className="mermaid-fallback">
      {error ? (
        <p className="mermaid-error" role="status">
          Diagram could not be rendered: {error}
        </p>
      ) : null}
      <pre className="mermaid">{source}</pre>
    </div>
  );
}
