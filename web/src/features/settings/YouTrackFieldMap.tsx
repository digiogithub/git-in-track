/**
 * Settings — the YouTrack field map (stories GIT-US-0055 and GIT-US-0065).
 *
 * An import is only as good as this table, and the table answers two questions,
 * not one:
 *
 *  1. **Which** YouTrack custom field carries a git-in-track field — the field
 *     called "State" carries `status`.
 *  2. **What** one of that field's values means here — the value "In Progress"
 *     is the local status `in_progress`.
 *
 * Both halves come from the companion. The YouTrack side is discovered from the
 * instance (`GET /youtrack/fields` answers each bundle-backed field with its
 * allowed values), the git-in-track side is declared by `config.FieldMapKeys`,
 * and the three fields whose *values* may be mapped are declared too, as
 * `valueMappableFields` — so no list is hard-coded here and none can drift.
 *
 * Three rules make the table honest rather than convenient:
 *
 *  - **A default is proposed, never applied silently.** On first open every
 *    unmapped field is matched case-insensitively against the instance's real
 *    field names, and every unmapped value against the local vocabulary, each
 *    marked as a proposal — so what is saved is what someone looked at.
 *  - **An unknown flag proposes nothing.** `isResolved` is absent rather than
 *    `false` when the instance never said whether a state closes an issue, and
 *    an absent flag must not propose a done status.
 *  - **A mapping that points at something the instance no longer has is a
 *    warning, not a deletion.** It stays selected, and clearing it is an
 *    explicit act: a rename in YouTrack must not silently unmap anything here.
 */

import { useQuery } from '@tanstack/react-query';
import { TriangleAlert } from 'lucide-react';
import { useEffect, useId, useMemo, useState } from 'react';

import type { YouTrackFieldMapping, YouTrackFieldValue, YouTrackSettings } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import { useProject } from '@/features/backlog/queries';
import { readProjectSchema } from '@/features/editor/project-schema';
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

/** The item types an issue can be imported as; a comment is not one of them. */
const ITEM_TYPE_TARGETS = ['epic', 'story', 'task', 'milestone'];

/** The four core priorities, used when the project declares none of its own. */
const DEFAULT_PRIORITY_TARGETS = ['critical', 'high', 'medium', 'low'];

/** One local value a YouTrack value can be mapped onto. */
type Target = { id: string; label: string };

export type YouTrackFieldMapProps = {
  settings: YouTrackSettings;
  /** The YouTrack project whose fields are offered; the draft, not the saved one. */
  project: string;
  saving: boolean;
  onSave: (fieldMap: Record<string, YouTrackFieldMapping>) => void;
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

/** "In Progress", "in-progress" and "in_progress" are the same word here. */
function normalize(text: string): string {
  return text.toLowerCase().replace(/[\s_-]+/g, '');
}

/**
 * The local value a YouTrack value most likely means.
 *
 * A name match comes first, in both directions of the pair, because it is the
 * only evidence that does not depend on this build's opinions. For a state
 * value, a `isResolved: true` flag is the fallback — and only `true`: the key
 * is absent, not `false`, when the instance never said, and proposing "done"
 * from silence would close issues nobody said were closed.
 */
function proposeValue(
  key: string,
  value: YouTrackFieldValue,
  targets: Target[],
  doneStatus: string | undefined,
): string | undefined {
  const wanted = normalize(value.name);
  const hit = targets.find(
    (target) => normalize(target.id) === wanted || normalize(target.label) === wanted,
  );
  if (hit) return hit.id;
  if (key === 'status' && value.isResolved === true) return doneStatus;
  return undefined;
}

/** The id a value row is keyed by in the "proposed" set. */
const valueKey = (key: string, value: string) => JSON.stringify([key, value]);

export function YouTrackFieldMap({ settings, project, saving, onSave }: YouTrackFieldMapProps) {
  const provider = useOptionalProvider();
  const [draft, setDraft] = useState<Record<string, YouTrackFieldMapping>>(settings.fieldMap);
  /** The rows a default was proposed for, so the table can say which ones. */
  const [proposed, setProposed] = useState<string[]>([]);
  /** The same, per value row, keyed by `valueKey`. */
  const [proposedValues, setProposedValues] = useState<string[]>([]);
  const headingId = useId();

  // Scoped to the git-in-track project the connection belongs to: a companion
  // serving several repositories refuses the unscoped call rather than guess.
  const fields = useQuery({
    queryKey: ['youtrack', 'fields', settings.projectKey, project],
    queryFn: () =>
      provider?.listYouTrackFields(
        project,
        settings.projectKey === '' ? {} : { projectKey: settings.projectKey },
      ) ?? Promise.resolve(null),
    enabled: provider !== null && project !== '' && settings.hasToken,
    retry: false,
  });

  // The local half of a value mapping: what this project's statuses,
  // priorities and item types are actually called.
  const localProject = useProject(settings.projectKey);
  const schema = useMemo(() => readProjectSchema(localProject.data), [localProject.data]);
  const targets = useMemo<Record<string, Target[]>>(
    () => ({
      status: schema.statuses.map((status) => ({ id: status.id, label: status.name })),
      priority: (schema.priorities.length > 0 ? schema.priorities : DEFAULT_PRIORITY_TARGETS).map(
        (priority) => ({ id: priority, label: priority }),
      ),
      type: ITEM_TYPE_TARGETS.map((type) => ({ id: type, label: type })),
    }),
    [schema],
  );
  /** The status a resolved state proposes, when the project declares one. */
  const doneStatus = useMemo(
    () => schema.statuses.find((status) => status.category === 'done')?.id,
    [schema],
  );

  const names = useMemo(
    () => (fields.data?.fields ?? []).map((field) => field.name),
    [fields.data],
  );
  const keys = fields.data?.gintrackFields ?? Object.keys(settings.fieldMap);
  const valueMappable = fields.data?.valueMappableFields ?? [];

  // A saved map always wins; the proposal fills what has none. It runs when the
  // field list arrives, which is also what makes changing the project
  // re-propose against the new instance data.
  useEffect(() => {
    if (names.length === 0) return;
    setDraft((current) => {
      const next: Record<string, YouTrackFieldMapping> = { ...current };
      const marked: string[] = [];
      for (const key of keys) {
        const entry = next[key];
        if (entry !== undefined && entry.field !== '') continue;
        const suggestion = propose(key, names);
        if (suggestion === undefined) continue;
        next[key] = { ...entry, field: suggestion };
        marked.push(key);
      }
      setProposed(marked);
      return next;
    });
    // `keys` is derived from the same query as `names`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [names]);

  /** The value-mappable rows that have a bundle to show values from. */
  const valueSections = useMemo(() => {
    const all = fields.data?.fields ?? [];
    return valueMappable
      .map((key) => {
        const name = draft[key]?.field ?? '';
        const field = all.find((candidate) => candidate.name === name);
        return { key, field };
      })
      .filter(
        (section): section is { key: string; field: NonNullable<typeof section.field> } =>
          section.field !== undefined && section.field.values.length > 0,
      );
    // `valueMappable` is derived from the same query as `fields.data`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fields.data, draft]);

  // The same proposal rule, one level down. It runs whenever the set of value
  // rows changes — which includes pointing `status` at a different field, and
  // the local project arriving after the instance's fields.
  useEffect(() => {
    if (valueSections.length === 0) return;
    const next = { ...draft };
    const marked: string[] = [];
    let changed = false;
    for (const section of valueSections) {
      const entry = next[section.key];
      if (entry === undefined) continue;
      const values = { ...entry.values };
      let touched = false;
      for (const value of section.field.values) {
        const already = values[value.name];
        if (already !== undefined && already !== '') continue;
        const suggestion = proposeValue(section.key, value, targets[section.key] ?? [], doneStatus);
        if (suggestion === undefined) continue;
        values[value.name] = suggestion;
        marked.push(valueKey(section.key, value.name));
        touched = true;
      }
      if (!touched) continue;
      next[section.key] = { ...entry, values };
      changed = true;
    }
    if (!changed) return;
    setDraft(next);
    setProposedValues((current) => [...new Set([...current, ...marked])]);
  }, [valueSections, targets, doneStatus, draft]);

  const missing = useMemo(
    () =>
      Object.entries(draft).filter(
        ([, entry]) => entry.field !== '' && names.length > 0 && !names.includes(entry.field),
      ),
    [draft, names],
  );

  const rows = keys.length > 0 ? keys : Object.keys(FIELD_LABELS);

  const setField = (key: string, name: string) => {
    setProposed((current) => current.filter((marked) => marked !== key));
    setDraft((current) => {
      // Pointing a field somewhere else invalidates the values that were read
      // off the old one: they belong to a bundle that is no longer in play.
      const entry = current[key];
      const values = entry?.field === name ? entry.values : undefined;
      const mapping: YouTrackFieldMapping =
        values === undefined ? { field: name } : { field: name, values };
      return { ...current, [key]: mapping };
    });
  };

  const setValue = (key: string, from: string, to: string) => {
    setProposedValues((current) => current.filter((marked) => marked !== valueKey(key, from)));
    setDraft((current) => {
      const entry = current[key] ?? { field: '' };
      const values = { ...entry.values };
      if (to === UNMAPPED) delete values[from];
      else values[from] = to;
      return { ...current, [key]: { ...entry, values } };
    });
  };

  return (
    <section aria-labelledby={headingId} className="space-y-3 border-t border-border pt-4">
      <div className="space-y-1">
        <h3 id={headingId} className="font-medium">
          Field map
        </h3>
        <p className="text-muted-foreground">
          Which YouTrack custom field carries each git-in-track field, and what its values mean
          here. An unmapped field or value is left alone by an import instead of being guessed at.
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
              const value = draft[key]?.field ?? UNMAPPED;
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
                    setField(key, next);
                  }}
                />
              );
            })}
          </dl>

          {valueSections.map((section) => (
            <ValueSection
              key={section.key}
              fieldKey={section.key}
              fieldName={section.field.name}
              values={section.field.values}
              targets={targets[section.key] ?? []}
              mapped={draft[section.key]?.values ?? {}}
              proposed={proposedValues}
              onChange={(from, to) => {
                setValue(section.key, from, to);
              }}
            />
          ))}

          {(fields.data?.fields ?? [])
            .filter((field) => field.warnings.length > 0)
            .map((field) => (
              <p key={field.id} role="status" className="text-xs text-muted-foreground">
                The values of {field.name} could not be read: {field.warnings.join(' ')}
              </p>
            ))}

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
                  {missing.map(([, entry]) => entry.field).join(', ')}{' '}
                  {missing.length === 1 ? 'is' : 'are'} mapped here but no longer exist in {project}
                  . The mapping is kept — a field may have been renamed — and an import will skip it
                  until you point it somewhere real.
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
              onSave(cleanFieldMap(draft));
            }}
          >
            Save field map
          </Button>
        </>
      )}
    </section>
  );
}

/**
 * The map as it is written: unmapped fields dropped, and an empty value map
 * dropped with them, so that turning a value mapping on and off again leaves
 * the configuration file as it was found.
 */
function cleanFieldMap(
  draft: Record<string, YouTrackFieldMapping>,
): Record<string, YouTrackFieldMapping> {
  const clean: Record<string, YouTrackFieldMapping> = {};
  for (const [key, entry] of Object.entries(draft)) {
    if (entry.field === '') continue;
    const values: Record<string, string> = {};
    for (const [from, to] of Object.entries(entry.values ?? {})) {
      if (to !== '') values[from] = to;
    }
    clean[key] = {
      field: entry.field,
      ...(Object.keys(values).length === 0 ? {} : { values }),
    };
  }
  return clean;
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

/**
 * One field's values, in the order the instance lists them.
 *
 * An archived value is shown rather than hidden: it is no longer offered on new
 * issues but it is still on old ones, and those are exactly the issues an
 * import reads.
 */
function ValueSection({
  fieldKey,
  fieldName,
  values,
  targets,
  mapped,
  proposed,
  onChange,
}: {
  fieldKey: string;
  fieldName: string;
  values: YouTrackFieldValue[];
  targets: Target[];
  mapped: Record<string, string>;
  proposed: string[];
  onChange: (from: string, to: string) => void;
}) {
  const headingId = useId();
  const label = FIELD_LABELS[fieldKey] ?? fieldKey;
  return (
    <section aria-labelledby={headingId} className="space-y-2 rounded-md border border-border p-3">
      <h4 id={headingId} className="font-medium">
        {fieldName} values → {label}
      </h4>
      <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[10rem_1fr]">
        {values.map((value) => (
          <ValueRow
            key={value.name}
            value={value}
            targets={targets}
            mapped={mapped[value.name] ?? UNMAPPED}
            proposed={proposed.includes(valueKey(fieldKey, value.name))}
            onChange={(next) => {
              onChange(value.name, next);
            }}
          />
        ))}
      </dl>
    </section>
  );
}

function ValueRow({
  value,
  targets,
  mapped,
  proposed,
  onChange,
}: {
  value: YouTrackFieldValue;
  targets: Target[];
  mapped: string;
  proposed: boolean;
  onChange: (value: string) => void;
}) {
  const id = useId();
  const gone = mapped !== '' && !targets.some((target) => target.id === mapped);
  return (
    <>
      <dt className="flex items-center gap-2">
        <label htmlFor={id}>{value.label}</label>
        {value.archived ? <Badge variant="outline">archived</Badge> : null}
      </dt>
      <dd className="flex items-center gap-2">
        <Select
          id={id}
          value={mapped}
          className="max-w-xs"
          onChange={(event) => {
            onChange(event.target.value);
          }}
        >
          <option value={UNMAPPED}>Not mapped</option>
          {targets.map((target) => (
            <option key={target.id} value={target.id}>
              {target.label}
            </option>
          ))}
          {/* A value mapped onto something this project no longer declares
              stays selectable, for the same reason a field does. */}
          {gone ? <option value={mapped}>{mapped} (missing)</option> : null}
        </Select>
        {gone ? (
          <Badge variant="warning">missing here</Badge>
        ) : proposed ? (
          <Badge variant="info">proposed</Badge>
        ) : mapped === '' ? (
          <Badge variant="outline">unmapped</Badge>
        ) : null}
      </dd>
    </>
  );
}
