/**
 * Conflict resolver — story GIT-US-0022, docs/05-web-app.md §5 and
 * docs/06-git-sync.md §5.
 *
 * A conflicted file is never shown as raw conflict markers. The front matter is
 * a field-by-field table on parsed values (mine / theirs / merged, with the
 * automatic result preselected), the body is a three-way view per hunk, and
 * keep-mine, keep-theirs and a manual edit are available for every conflict,
 * whatever its shape.
 *
 * Everything the merge decided on its own is shown as a row with the rule it
 * applied, and every row can be flipped: nothing is auto-resolved silently.
 * The merge itself runs in the Go core (`internal/core/merge.go`) in both
 * runtimes, so the browser and the companion never drift.
 */

import { useCallback, useEffect, useState } from 'react';

import type {
  ConflictAnalysis,
  ConflictFieldDecision,
  ConflictHunk,
  ConflictResolveResult,
} from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Textarea } from '@/components/ui/textarea';

/** How a hunk or a field can be decided. */
type Side = 'ours' | 'theirs' | 'base' | 'both' | 'edited';

/** What one value looks like in the table; YAML values are rendered as text. */
function show(value: unknown): string {
  if (value === undefined || value === null) return '—';
  if (Array.isArray(value)) return value.map((entry) => show(entry)).join(', ');
  if (typeof value === 'object') return JSON.stringify(value);
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value) ?? '—';
}

export function ConflictResolver({
  repoId,
  path,
  onResolved,
  onClose,
}: {
  repoId: string;
  path: string;
  onResolved?: (result: ConflictResolveResult) => void;
  onClose?: () => void;
}) {
  const provider = useOptionalProvider();
  const [analysis, setAnalysis] = useState<ConflictAnalysis | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [hunks, setHunks] = useState<Record<string, string>>({});
  const [hunkText, setHunkText] = useState<Record<string, string>>({});
  const [manual, setManual] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<ConflictResolveResult | null>(null);

  const load = useCallback(async () => {
    if (!provider) return;
    setAnalysis(await provider.readConflict(repoId, path));
  }, [provider, repoId, path]);

  useEffect(() => {
    void load().catch((cause: unknown) => {
      setError(cause instanceof Error ? cause.message : String(cause));
    });
  }, [load]);

  if (!provider) return null;

  const resolve = (resolution: 'ours' | 'theirs' | 'merged' | 'manual') => {
    setBusy(true);
    setError(null);
    provider
      .resolveConflict(repoId, path, {
        resolution,
        ...(resolution === 'manual' && manual !== null ? { content: manual } : {}),
        ...(resolution === 'merged' && Object.keys(fields).length > 0 ? { fields } : {}),
        ...(resolution === 'merged' && Object.keys(hunks).length > 0 ? { hunks } : {}),
        ...(resolution === 'merged' && Object.keys(hunkText).length > 0 ? { hunkText } : {}),
      })
      .then((result) => {
        setDone(result);
        onResolved?.(result);
      })
      .catch((cause: unknown) => {
        setError(cause instanceof Error ? cause.message : String(cause));
      })
      .finally(() => {
        setBusy(false);
      });
  };

  const abort = () => {
    setBusy(true);
    setError(null);
    provider
      .abortSync(repoId)
      .then(() => {
        onClose?.();
      })
      .catch((cause: unknown) => {
        setError(cause instanceof Error ? cause.message : String(cause));
      })
      .finally(() => {
        setBusy(false);
      });
  };

  const merge = analysis?.merge;
  const unresolved = (merge?.hunks ?? []).filter(
    (hunk) => hunk.conflicted && hunks[String(hunk.index)] === undefined,
  ).length;

  return (
    <Card aria-label={`Conflict in ${path}`}>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2">
          <span className="font-mono text-sm">{path}</span>
          <Badge variant="destructive">{analysis?.kind ?? 'content'}</Badge>
          {analysis?.operation ? <Badge variant="outline">{analysis.operation}</Badge> : null}
        </CardTitle>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" disabled={busy} onClick={abort}>
            Abort and restore
          </Button>
          {onClose ? (
            <Button variant="outline" size="sm" onClick={onClose}>
              Close
            </Button>
          ) : null}
        </div>
      </CardHeader>

      <CardContent className="space-y-5 text-sm">
        {error ? (
          <p role="alert" className="rounded-md border border-destructive/40 p-3 text-destructive">
            {error}
          </p>
        ) : null}

        {done ? (
          <p role="status" className="rounded-md border border-border bg-secondary/50 p-3">
            {done.result.continued
              ? `Resolved. The ${analysis?.operation ?? 'integration'} finished and your files are back in one piece.`
              : `Resolved and staged. ${done.result.remaining?.length ?? 0} file(s) still need a decision.`}
          </p>
        ) : null}

        {analysis === null ? <p className="text-muted-foreground">Reading the conflict…</p> : null}

        {analysis && analysis.versions.binary ? (
          <p className="text-muted-foreground">
            This file is binary, so there is nothing to merge line by line: keep mine or keep
            theirs.
          </p>
        ) : null}

        {merge && merge.fields && merge.fields.length > 0 ? (
          <section aria-label="Front matter" className="space-y-2">
            <h3 className="font-medium">Front matter</h3>
            <p className="text-muted-foreground">
              Merged field by field on parsed values, never as text, so nobody’s label or assignee
              is dropped. Every automatic decision is a row you can flip.
            </p>
            <div className="overflow-x-auto">
              <table className="w-full text-left">
                <thead className="text-xs uppercase tracking-wide text-muted-foreground">
                  <tr>
                    <th className="py-1 pr-3">Field</th>
                    <th className="py-1 pr-3">Mine</th>
                    <th className="py-1 pr-3">Theirs</th>
                    <th className="py-1 pr-3">Merged</th>
                    <th className="py-1">Keep</th>
                  </tr>
                </thead>
                <tbody>
                  {merge.fields.map((field) => (
                    <FieldRow
                      key={field.field}
                      field={field}
                      choice={fields[field.field]}
                      onChoose={(side) => {
                        setFields((current) => ({ ...current, [field.field]: side }));
                      }}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        ) : null}

        {merge && merge.hunks && merge.hunks.length > 0 ? (
          <section aria-label="Body" className="space-y-3">
            <h3 className="font-medium">Body</h3>
            {merge.hunks.map((hunk) => (
              <HunkView
                key={hunk.index}
                hunk={hunk}
                choice={hunks[String(hunk.index)]}
                text={hunkText[String(hunk.index)]}
                onChoose={(side) => {
                  setHunks((current) => ({ ...current, [String(hunk.index)]: side }));
                }}
                onEdit={(value) => {
                  setHunkText((current) => ({ ...current, [String(hunk.index)]: value }));
                  setHunks((current) => ({ ...current, [String(hunk.index)]: 'edited' }));
                }}
              />
            ))}
          </section>
        ) : null}

        {merge ? (
          <section aria-label="Result" className="space-y-2">
            <h3 className="font-medium">Result</h3>
            <p className="text-muted-foreground">
              {unresolved === 0
                ? 'Every hunk has a decision. Accepting writes this file, stages it and finishes the integration.'
                : `${unresolved} hunk(s) still need a decision, or keep mine, keep theirs or edit the file yourself.`}
            </p>
            <Textarea
              aria-label="Merged file"
              readOnly={manual === null}
              value={manual ?? done?.merge.content ?? merge.content}
              onChange={(event) => {
                setManual(event.target.value);
              }}
              className="min-h-[10rem]"
            />
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                disabled={busy || unresolved > 0 || manual !== null}
                onClick={() => {
                  resolve('merged');
                }}
              >
                Accept merged
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={busy}
                onClick={() => {
                  setManual(manual ?? merge.content);
                }}
              >
                Edit manually
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={busy || manual === null}
                onClick={() => {
                  resolve('manual');
                }}
              >
                Accept my edit
              </Button>
            </div>
          </section>
        ) : null}

        <div className="flex flex-wrap gap-2 border-t border-border pt-3">
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => {
              resolve('ours');
            }}
          >
            Keep mine
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => {
              resolve('theirs');
            }}
          >
            Keep theirs
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

/** One front-matter field, with what each side had and what the merge chose. */
function FieldRow({
  field,
  choice,
  onChoose,
}: {
  field: ConflictFieldDecision;
  choice: string | undefined;
  onChoose: (side: Side) => void;
}) {
  const current = choice ?? field.choice;
  return (
    <tr className="border-t border-border align-top">
      <th scope="row" className="py-2 pr-3 text-left font-medium">
        <span className="font-mono">{field.field}</span>
        {field.review ? (
          <Badge variant="destructive" className="ml-2">
            review
          </Badge>
        ) : null}
        {field.note ? <p className="text-xs text-muted-foreground">{field.note}</p> : null}
      </th>
      <td className="py-2 pr-3">{show(field.ours)}</td>
      <td className="py-2 pr-3">{show(field.theirs)}</td>
      <td className="py-2 pr-3">{show(choice === 'ours' ? field.ours : choice === 'theirs' ? field.theirs : field.merged)}</td>
      <td className="py-2">
        <div className="flex gap-1">
          <Button
            variant={current === 'ours' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              onChoose('ours');
            }}
          >
            Mine
          </Button>
          <Button
            variant={current === 'theirs' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              onChoose('theirs');
            }}
          >
            Theirs
          </Button>
        </div>
      </td>
    </tr>
  );
}

/** One body hunk, three ways, with the heading it falls under. */
function HunkView({
  hunk,
  choice,
  text,
  onChoose,
  onEdit,
}: {
  hunk: ConflictHunk;
  choice: string | undefined;
  text: string | undefined;
  onChoose: (side: Side) => void;
  onEdit: (value: string) => void;
}) {
  const current = choice ?? hunk.choice;
  return (
    <article
      aria-label={`Hunk ${String(hunk.index + 1)}`}
      className="space-y-2 rounded-md border border-border p-3"
    >
      <header className="flex flex-wrap items-center gap-2">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">
          {hunk.section ? hunk.section : 'Body'}
        </span>
        {hunk.conflicted ? (
          <Badge variant="destructive">both sides changed this</Badge>
        ) : (
          <Badge variant="outline">auto: {hunk.choice}</Badge>
        )}
        {hunk.note ? <span className="text-xs text-muted-foreground">{hunk.note}</span> : null}
      </header>

      <div className="grid gap-2 md:grid-cols-3">
        <Pane label="Mine" body={hunk.ours} />
        <Pane label="Theirs" body={hunk.theirs} />
        <Pane label="Base" body={hunk.base} />
      </div>

      <div className="flex flex-wrap gap-1">
        {(['ours', 'theirs', 'both', 'base'] as const).map((side) => (
          <Button
            key={side}
            variant={current === side ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              onChoose(side);
            }}
          >
            {side === 'ours'
              ? 'Take mine'
              : side === 'theirs'
                ? 'Take theirs'
                : side === 'both'
                  ? 'Take both'
                  : 'Take base'}
          </Button>
        ))}
      </div>

      <Textarea
        aria-label={`Edit hunk ${String(hunk.index + 1)}`}
        value={text ?? hunk.merged}
        onChange={(event) => {
          onEdit(event.target.value);
        }}
      />
    </article>
  );
}

/** One side of a hunk. */
function Pane({ label, body }: { label: string; body: string }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <pre className="overflow-x-auto whitespace-pre-wrap rounded-md bg-secondary/40 p-2 font-mono text-xs">
        {body === '' ? '(nothing)' : body}
      </pre>
    </div>
  );
}
