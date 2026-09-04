import { useMemo } from 'react';

import { cn } from '@/lib/cn';
import { MarkdownContent, useMarkdown, type RenderOptions } from '@/markdown';

export type MarkdownPreviewProps = {
  value: string;
  className?: string;
};

/**
 * Split-view preview of the body, rendered with the shared unified pipeline
 * (docs/05-web-app.md §7) so the editor and the read views agree.
 *
 * Highlighting is off here: the preview re-renders on every keystroke and the
 * Shiki chunk is not worth the latency while typing.
 */
export function MarkdownPreview({ value, className }: MarkdownPreviewProps) {
  const options = useMemo<RenderOptions>(() => ({ highlight: false }), []);
  const { status, result, error } = useMarkdown(value, options);

  return (
    <div
      className={cn('flex h-full flex-col overflow-hidden rounded-md border border-border', className)}
    >
      <p className="border-b border-border bg-secondary/60 px-3 py-1 text-[11px] uppercase tracking-wide text-muted-foreground">
        Preview
      </p>
      <div data-testid="markdown-preview" className="flex-1 overflow-auto p-3 text-sm leading-6">
        {status === 'error' ? (
          <p className="text-destructive" role="alert">
            {error?.message ?? 'Could not render this Markdown'}
          </p>
        ) : result ? (
          <MarkdownContent result={result} />
        ) : (
          <p className="text-muted-foreground">Rendering…</p>
        )}
      </div>
    </div>
  );
}
