import type { Diagnostic } from '@/api/provider';
import { cn } from '@/lib/cn';

export type DiagnosticListProps = {
  diagnostics: Diagnostic[];
  title?: string;
  className?: string;
};

const severityStyles: Record<Diagnostic['severity'], string> = {
  error: 'text-destructive',
  warning: 'text-[hsl(var(--priority-high))]',
  info: 'text-muted-foreground',
};

/** Diagnostics with their codes, as the core reports them (docs 03 §16). */
export function DiagnosticList({ diagnostics, title, className }: DiagnosticListProps) {
  if (diagnostics.length === 0) return null;
  return (
    <div className={cn('rounded-md border border-border bg-secondary/40 p-3', className)} role="alert">
      {title ? <p className="mb-1 text-xs font-semibold uppercase tracking-wide">{title}</p> : null}
      <ul className="space-y-1 text-sm">
        {diagnostics.map((d, index) => (
          <li key={`${d.code}-${d.field ?? ''}-${index}`} className={severityStyles[d.severity]}>
            <code className="mr-2 rounded bg-background px-1 py-0.5 text-xs">{d.code}</code>
            {d.message}
            {d.field ? <span className="ml-1 text-muted-foreground">({d.field})</span> : null}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Field-scoped message, rendered under the input it belongs to. */
export function FieldIssue({ diagnostics, field }: { diagnostics: Diagnostic[]; field: string }) {
  const issue = diagnostics.find((d) => d.field === field);
  if (!issue) return null;
  return (
    <p className={cn('mt-1 text-xs', severityStyles[issue.severity])}>
      <code className="mr-1">{issue.code}</code>
      {issue.message}
    </p>
  );
}
