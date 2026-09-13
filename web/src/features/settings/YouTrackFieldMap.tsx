/**
 * Settings — the YouTrack field map (stories GIT-US-0055 and GIT-US-0065).
 *
 * An import is only as good as this table: it decides which YouTrack custom
 * field carries a git-in-track status, priority, type, assignee or estimate.
 * The two halves both come from the companion — the YouTrack side discovered
 * from the instance, the git-in-track side declared by `config.FieldMapKeys` —
 * so neither list is hard-coded here and neither can drift.
 *
 * Two rules make the table honest rather than convenient:
 *
 *  - **A default is proposed, never applied silently.** On first open every
 *    unmapped row is matched case-insensitively against the instance's real
 *    field names and marked as a proposal, so what is saved is what someone
 *    looked at.
 *  - **A mapping that points at a field the instance no longer has is a
 *    warning, not a deletion.** It stays selected, and clearing it is an
 *    explicit act: a rename in YouTrack must not silently unmap a field here.
 */

import { useQuery } from '@tanstack/react-query';
import { TriangleAlert } from 'lucide-react';
import { useEffect, useId, useMemo, useState } from 'react';

import type { YouTrackSettings } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import { youtrackMessage } from '@/features/settings/youtrack-messages';

/** The value of the "no mapping" option; `''` is not a legal field name. */
const UNMAPPED = '';

/**
 * What a git-in-track field is usually called in YouTrack, most specific first.
 * Only used to *propose*: nothing here is applied without a save.
 */
const DEFAULT_CANDIDATES: Record<string, string[]> = {
  status: ['state', 'stage'],
  priority: ['priority'],
  type: ['type', 'issue type'],
  assignee: ['assignee', 'assigned to'],
  labels: ['tags', 'labels'],
  estimate: ['estimation', 'estimate', 'story points'],
  milestone: ['fix versions', 'fix version', 'milestone'],
  due: ['due date', 'due'],
  sprint: ['sprint', 'iteration'],
};

/** How each git-in-track field is labelled in the table. */
const FIELD_LABELS: Record<string, string> = {
  status: 'Status',
  priority: 'Priority',
  type: 'Type',
  assignee: 'Assignee',
  labels: 'Labels',
  estimate: 'Estimate',
  milestone: 'Milestone',
  due: 'Due date',
  sprint: 'Sprint',
};

export type YouTrackFieldMapProps = {
  settings: YouTrackSettings;
  /** The YouTrack project whose fields are offered; the draft, not the saved one. */
  project: string;
  saving: boolean;
  onSave: (fieldMap: Record<string, string>) => void;
};

/** The proposal for one row, or `undefined` when nothing matched. */
function propose(key: string, names: string[]): string | undefined {
  const byLower = new Map(names.map((name) => [name.toLowerCase(), name]));
  for (const candidate of DEFAULT_CANDIDATES[key] ?? []) {
    const hit = byLower.get(candidate);
    if (hit !== undefined) return hit;
  }
  return undefined;
}

export function YouTrackFieldMap({ settings, project, saving, onSave }: YouTrackFieldMapProps) {
  const provider = useOptionalProvider();
  const [draft, setDraft] = useState<Record<string, string>>(settings.fieldMap);
  /** The rows a default was proposed for, so the table can say which ones. */
  const [proposed, setProposed] = useState<string[]>([]);
  const headingId = useId();

  const fields = useQuery({
    queryKey: ['youtrack', 'fields', project],
    queryFn: () => provider?.listYouTrackFields(project) ?? Promise.resolve(null),
    enabled: provider !== null && project !== '' && settings.hasToken,
    retry: false,
  });

  const names = useMemo(
    () => (fields.data?.fields ?? []).map((field) => field.name),
    [fields.data],
  );
  const keys = fields.data?.gintrackFields ?? Object.keys(settings.fieldMap);

  // A saved map always wins; the proposal fills the rows that have none. It
  // runs when the field list arrives, which is also what makes changing the
  // project re-propose against the new instance data.
  useEffect(() => {
    if (names.length === 0) return;
    setDraft((current) => {
      const next = { ...current };
      const marked: string[] = [];
      for (const key of keys) {
        if (next[key] !== undefined && next[key] !== '') continue;
        const suggestion = propose(key, names);
        if (suggestion === undefined) continue;
        next[key] = suggestion;
        marked.push(key);
      }
      setProposed(marked);
      return next;
    });
    // `keys` is derived from the same query as `names`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [names]);

  const missing = useMemo(
    () =>
      Object.entries(draft).filter(
        ([, name]) => name !== '' && names.length > 0 && !names.includes(name),
      ),
    [draft, names],
  );

  const rows = keys.length > 0 ? keys : Object.keys(FIELD_LABELS);

  return (
    <section aria-labelledby={headingId} className="space-y-3 border-t border-border pt-4">
      <div className="space-y-1">
        <h3 id={headingId} className="font-medium">
          Field map
        </h3>
        <p className="text-muted-foreground">
          Which YouTrack custom field carries each git-in-track field. An unmapped field is left
          alone by an import instead of being guessed at.
        </p>
      </div>

      {!settings.hasToken || project === '' ? (
        <p className="text-muted-foreground">
          Save a token and a YouTrack project to read that project’s custom fields.
        </p>
      ) : fields.isPending ? (
        <p className="text-muted-foreground">Reading the fields of {project}…</p>
      ) : fields.isError ? (
        <p role="alert" className="text-destructive">
          {youtrackMessage(fields.error)}
        </p>
      ) : (
        <>
          <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[10rem_1fr]">
            {rows.map((key) => {
              const value = draft[key] ?? UNMAPPED;
              const gone = value !== '' && !names.includes(value);
              return (
                <FieldRow
                  key={key}
                  fieldKey={key}
                  value={value}
                  names={names}
                  gone={gone}
                  proposed={proposed.includes(key)}
                  onChange={(next) => {
                    setProposed((current) => current.filter((marked) => marked !== key));
                    setDraft((current) => ({ ...current, [key]: next }));
                  }}
                />
              );
            })}
          </dl>

          {missing.length === 0 ? null : (
            <div
              role="alert"
              className="space-y-2 rounded-md border border-warning/40 bg-warning/10 p-3"
            >
              <p className="flex gap-2">
                <TriangleAlert
                  aria-hidden="true"
                  className="mt-0.5 h-4 w-4 shrink-0 text-warning"
                />
                <span>
                  {missing.map(([, name]) => name).join(', ')} {missing.length === 1 ? 'is' : 'are'}{' '}
                  mapped here but no longer exist in {project}. The mapping is kept — a field may
                  have been renamed — and an import will skip it until you point it somewhere real.
                </span>
              </p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setDraft((current) => {
                    const next = { ...current };
                    for (const [key] of missing) delete next[key];
                    return next;
                  });
                }}
              >
                {missing.length === 1
                  ? 'Clear the mapping that no longer exists'
                  : 'Clear the mappings that no longer exist'}
              </Button>
            </div>
          )}

          <Button
            type="button"
            disabled={saving}
            onClick={() => {
              const clean: Record<string, string> = {};
              for (const [key, name] of Object.entries(draft)) {
                if (name !== '') clean[key] = name;
              }
              onSave(clean);
            }}
          >
            Save field map
          </Button>
        </>
      )}
    </section>
  );
}

function FieldRow({
  fieldKey,
  value,
  names,
  gone,
  proposed,
  onChange,
}: {
  fieldKey: string;
  value: string;
  names: string[];
  gone: boolean;
  proposed: boolean;
  onChange: (value: string) => void;
}) {
  const id = useId();
  const label = FIELD_LABELS[fieldKey] ?? fieldKey;
  return (
    <>
      <dt>
        <label htmlFor={id}>{label}</label>
      </dt>
      <dd className="flex items-center gap-2">
        <Select
          id={id}
          value={value}
          className="max-w-xs"
          onChange={(event) => {
            onChange(event.target.value);
          }}
        >
          <option value={UNMAPPED}>Not mapped</option>
          {names.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
          {/* A mapping the instance no longer offers stays selectable, so the
              table never silently drops it. */}
          {gone ? <option value={value}>{value} (missing)</option> : null}
        </Select>
        {gone ? (
          <Badge variant="warning">missing in YouTrack</Badge>
        ) : proposed ? (
          <Badge variant="info">proposed</Badge>
        ) : value === '' ? (
          <Badge variant="outline">unmapped</Badge>
        ) : null}
      </dd>
    </>
  );
}
