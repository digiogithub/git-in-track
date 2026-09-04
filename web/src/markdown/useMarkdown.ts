/**
 * Renders Markdown off the render path.
 *
 * `renderMarkdown` is asynchronous (Shiki is a lazily imported chunk), so the
 * hook keeps the previously rendered tree visible while the next one is
 * prepared instead of flashing a skeleton on every keystroke or navigation.
 *
 * `options` is a dependency: memoise it in the caller (`useMemo`), otherwise
 * every parent render re-renders the document.
 */

import { startTransition, useEffect, useState } from 'react';

import { renderMarkdown } from '@/markdown/pipeline';
import type { RenderOptions, RenderResult } from '@/markdown/types';

export type MarkdownState = {
  status: 'pending' | 'ready' | 'error';
  result: RenderResult | null;
  error: Error | null;
};

const INITIAL: MarkdownState = { status: 'pending', result: null, error: null };

export function useMarkdown(source: string, options: RenderOptions = {}): MarkdownState {
  const [state, setState] = useState<MarkdownState>(INITIAL);

  useEffect(() => {
    let cancelled = false;
    setState((previous) => ({ ...previous, status: 'pending', error: null }));

    renderMarkdown(source, options).then(
      (result) => {
        if (cancelled) return;
        startTransition(() => setState({ status: 'ready', result, error: null }));
      },
      (error: unknown) => {
        if (cancelled) return;
        setState({
          status: 'error',
          result: null,
          error: error instanceof Error ? error : new Error(String(error)),
        });
      },
    );

    return () => {
      cancelled = true;
    };
  }, [source, options]);

  return state;
}
