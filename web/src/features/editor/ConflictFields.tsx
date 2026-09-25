import type { ConflictField } from '@/api/provider';

export type ConflictFieldRow = ConflictField & {
  /** A human label for the field; defaults to its name. */
  label?: string;
};

/**
 * The per-field diff of a refused conditional write (`stale_revision`,
 * R-REV-3a): for each field still in conflict, what is on disk now ("theirs")
 * next to what the write wanted ("yours"). A field the runtime names without
 * quoting — a body or block text — reads "changed on disk" unless the caller
 * supplies both sides. Plain text only: repository content is data.
 */
export function ConflictFields({ fields }: { fields: readonly ConflictFieldRow[] }) {
  return (
    <dl aria-label="Fields in conflict" className="space-y-3">
      {fields.map((field) => (
        <div
          key={field.field}
          data-conflict-field={field.field}
          className="space-y-1.5 rounded-md border border-border p-3"
        >
          <dt className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {field.label ?? field.field}
          </dt>
          <dd className="grid gap-2 sm:grid-cols-2">
            <ConflictSide
              heading="Theirs (on disk)"
              value={field.current}
              tone="border-border-strong"
            />
            <ConflictSide heading="Yours" value={field.proposed} tone="border-accent" />
          </dd>
        </div>
      ))}
    </dl>
  );
}

function ConflictSide({
  heading,
  value,
  tone,
}: {
  heading: string;
  value: string | undefined;
  tone: string;
}) {
  return (
    <div className={`min-w-0 space-y-1 border-l-2 pl-2 ${tone}`}>
      <p className="text-2xs font-medium text-muted-foreground">{heading}</p>
      {value === undefined ? (
        <p className="text-sm italic text-muted-foreground">changed on disk</p>
      ) : value === '' ? (
        <p className="text-sm italic text-muted-foreground">empty</p>
      ) : (
        <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words font-mono text-xs">
          {value}
        </pre>
      )}
    </div>
  );
}
