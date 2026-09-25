import { keepPreviousData, useQuery } from '@tanstack/react-query';

import type { DeltaPreviewOperation } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { mayHaveSpecDelta } from '@/features/editor/spec-lint';
import { useDebounced } from '@/features/youtrack/queries';
import { cn } from '@/lib/cn';

/** How long typing must pause before the preview asks the core again. */
const previewDelayMs = 400;

const opVariant: Record<DeltaPreviewOperation['op'], BadgeProps['variant']> = {
  ADDED: 'success',
  MODIFIED: 'info',
  REMOVED: 'destructive',
};

export type SpecDeltaPreviewProps = {
  project: string;
  /** The story or task being edited. */
  id?: string;
  /** The body as the editor holds it, saved or not. */
  body: string;
};

/**
 * The `## Spec Delta` of the body being edited, each operation next to what
 * the spec holds today (GIT-US-0132, doc 03 §21.8): an ADDED block with the
 * requirement it supersedes, a MODIFIED target's current text against the
 * proposed one, and a REMOVED target with its reason. The core parses the
 * delta and resolves every target — in the browser and in the companion alike
 * — so a target the repository does not hold reads as dangling here exactly as
 * it does in `W-DELTA-DANGLING`. Nothing renders for a body without a delta.
 */
export function SpecDeltaPreview({ project, id, body }: SpecDeltaPreviewProps) {
  const provider = useProvider();
  const settled = useDebounced(body, previewDelayMs);
  const enabled = project !== '' && mayHaveSpecDelta(settled);
  const query = useQuery({
    queryKey: ['items', project, 'delta-preview', id ?? '', settled],
    queryFn: () => provider.previewSpecDelta(project, { ...(id ? { id } : {}), body: settled }),
    enabled,
    placeholderData: keepPreviousData,
    staleTime: 0,
    gcTime: 0,
  });

  if (!enabled) return null;
  if (query.isError) {
    return (
      <section aria-label="Spec Delta preview" className="space-y-2">
        <h2 className="section-label">Spec Delta preview</h2>
        <p className="text-sm text-muted-foreground" role="status">
          The preview is not available:{' '}
          {query.error instanceof Error ? query.error.message : String(query.error)}
        </p>
      </section>
    );
  }
  const operations = query.data ?? [];
  if (operations.length === 0) return null;

  return (
    <section aria-label="Spec Delta preview" className="space-y-3">
      <h2 className="section-label">Spec Delta preview</h2>
      <ol className="space-y-3">
        {operations.map((op) => (
          <DeltaOperationCard key={`${op.line}-${op.op}-${op.target}`} op={op} />
        ))}
      </ol>
    </section>
  );
}

function DeltaOperationCard({ op }: { op: DeltaPreviewOperation }) {
  return (
    <li
      aria-label={`${op.op} ${op.target}`}
      className="space-y-2 rounded-md border border-border bg-card p-3"
    >
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={opVariant[op.op]}>{op.op}</Badge>
        <code className="font-mono text-xs">{op.target}</code>
        <span className="text-sm font-medium">{op.title}</span>
        {op.specTitle ? (
          <span className="text-xs text-muted-foreground">in {op.specTitle}</span>
        ) : null}
        <span className="ml-auto text-xs text-muted-foreground">line {op.line}</span>
      </div>

      {op.dangling ? (
        <p className="text-sm text-warning" role="status">
          <code className="mr-1 font-mono text-xs">W-DELTA-DANGLING</code>
          {op.dangling}. There is no current text to compare with.
        </p>
      ) : null}

      <DeltaBody op={op} />
    </li>
  );
}

function DeltaBody({ op }: { op: DeltaPreviewOperation }) {
  switch (op.op) {
    case 'ADDED':
      return (
        <div className={op.current ? 'grid gap-3 md:grid-cols-2' : ''}>
          {op.current ? (
            <TextPane
              label={`Supersedes ${op.current.ref} — ${op.current.title}`}
              text={op.current.text}
              tone="old"
            />
          ) : null}
          <TextPane label="Added" text={op.proposed ?? ''} tone="new" />
        </div>
      );
    case 'MODIFIED':
      return (
        <div className="grid gap-3 md:grid-cols-2">
          {op.current ? (
            <TextPane label={`Current — ${op.current.title}`} text={op.current.text} tone="old" />
          ) : null}
          <TextPane label="Proposed" text={op.proposed ?? ''} tone="new" />
        </div>
      );
    case 'REMOVED':
      return (
        <div className="space-y-2">
          {op.current ? (
            <TextPane
              label={`Removed — ${op.current.title}`}
              text={op.current.text}
              tone="removed"
            />
          ) : null}
          <p className="text-sm">
            <span className="font-medium">Reason:</span>{' '}
            {op.reason ? op.reason : <span className="text-destructive">missing</span>}
          </p>
        </div>
      );
  }
}

function TextPane({
  label,
  text,
  tone,
}: {
  label: string;
  text: string;
  tone: 'old' | 'new' | 'removed';
}) {
  const border =
    tone === 'new'
      ? 'border-success/40'
      : tone === 'removed'
        ? 'border-destructive/40'
        : 'border-border';
  return (
    <figure className="space-y-1">
      <figcaption className="text-xs font-medium text-muted-foreground">{label}</figcaption>
      <pre
        className={cn(
          'whitespace-pre-wrap break-words rounded-md border bg-surface-muted/40 p-2 font-mono text-xs',
          border,
          tone === 'removed' && 'line-through decoration-destructive/60',
        )}
      >
        {text === '' ? <span className="text-muted-foreground">(empty)</span> : text}
      </pre>
    </figure>
  );
}
