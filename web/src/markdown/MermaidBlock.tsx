/**
 * Mermaid diagrams, never at parse time (docs/05-web-app.md §7, "Mermaid").
 *
 * The block starts as the diagram source in a `<pre>`. On first visibility it
 * dynamically imports Pele (`pele/lazy`, ADR-042), whose entry is small and
 * which loads each diagram type on demand, so only pages that actually contain
 * a diagram download a renderer, and only for the types they use. The SVG
 * replaces the source; a syntax error keeps the source visible with the error
 * as a text note.
 *
 * Colours come from `--pele-*` CSS variables mapped to the design tokens in
 * `markdown.css`, so a theme switch restyles the SVG without a re-render.
 *
 * Security (https://pele.run/security): Pele escapes diagram text, emits no
 * HTML labels or scripts, ignores `click` callbacks and always blocks
 * `javascript:`/`data:`/`vbscript:` links. The render options are fixed here —
 * nothing in the diagram source can supply them — and narrow links to the same
 * schemes the Markdown sanitiser allows. The `icons` and `math` hooks, which
 * insert unfiltered markup, are never passed.
 */

import type { RenderOptions } from 'pele/lazy';
import { useEffect, useId, useRef, useState } from 'react';

/** The same schemes `sanitize.ts` allows on a Markdown `href`. */
const LINK_SCHEMES = ['http', 'https', 'mailto'];

/**
 * The fixed options every diagram renders with. Diagram images would make
 * network requests the asset resolver never sees, so they are off.
 */
function peleOptions(idPrefix: string, fontFamily: string): RenderOptions {
  return {
    idPrefix,
    // The size mermaid used here, so a diagram sits at prose scale.
    fontSize: 14,
    linkSchemes: LINK_SCHEMES,
    linkRel: 'noopener noreferrer nofollow',
    images: false,
    ...(fontFamily ? { fontFamily } : {}),
  };
}

export type MermaidBlockProps = {
  source: string;
  className?: string;
};

export function MermaidBlock({ source, className }: MermaidBlockProps) {
  // SVG ids (markers, gradients) are document-global: give every diagram its
  // own prefix so two diagrams on one page never share an arrowhead.
  const idPrefix = `pele${useId().replace(/[^\w-]/g, '')}`;
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

    const run = async () => {
      try {
        const { renderAsync } = await import('pele/lazy');
        // Measure labels in the font the SVG will be drawn in.
        const node = container.current;
        const font = node ? getComputedStyle(node).fontFamily : '';
        const rendered = await renderAsync(source, peleOptions(idPrefix, font));
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
    };
  }, [idPrefix, source, visible]);

  if (svg && !error) {
    return (
      <div
        ref={container}
        className={className ?? 'mermaid-diagram'}
        role="img"
        // Pele escapes every label and filters links and styles; the options
        // above are fixed and the diagram source cannot change them.
        dangerouslySetInnerHTML={{ __html: svg }}
      />
    );
  }

  return (
    <div ref={container} className="mermaid-fallback">
      {error ? (
        // A React text node: an error message that quotes the source is
        // shown as text, never parsed as HTML.
        <p className="mermaid-error" role="status">
          Diagram could not be rendered: {error}
        </p>
      ) : null}
      <pre className="mermaid">{source}</pre>
    </div>
  );
}
