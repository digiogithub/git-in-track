/**
 * Front-matter values shared by the form editor, the raw YAML editor and the
 * save path.
 *
 * Field names, canonical key order and validation codes follow
 * docs/03-data-model.md §3.2, §13 and §16. `id`, `type`, `created`, `updated`
 * and `author` are never edited here: the core maintains them (§3.2, story
 * GIT-US-0010 acceptance criteria).
 */

import { parse, stringify } from 'yaml';

import type { Diagnostic, Item, ItemPatch, ItemType, Priority } from '@/api/provider';
import type { Link, LinkKind } from '@/core-bridge/api';
import type { EditorProjectSchema } from '@/features/editor/project-schema';
import { customFieldsFor } from '@/features/editor/project-schema';

export const linkKinds: LinkKind[] = ['blocks', 'blocked_by', 'relates_to', 'duplicates'];

export const priorities: Priority[] = ['critical', 'high', 'medium', 'low'];

export type FrontMatterValues = {
  title: string;
  status: string;
  priority: Priority | null;
  parent: string | null;
  milestone: string | null;
  assignees: string[];
  labels: string[];
  estimate: number | null;
  due: string | null;
  links: Link[];
  custom: Record<string, unknown>;
};

/** Editable front-matter keys, in the canonical order of §3.2. */
export const editableKeys = [
  'title',
  'status',
  'priority',
  'parent',
  'milestone',
  'assignees',
  'labels',
  'estimate',
  'due',
  'links',
  'custom',
] as const;

const idPattern = /^[A-Z][A-Z0-9]{1,9}-(EP|US|T|M)-\d{4,}$/;
const datePattern = /^\d{4}-\d{2}-\d{2}$/;

export function emptyValues(status = ''): FrontMatterValues {
  return {
    title: '',
    status,
    priority: null,
    parent: null,
    milestone: null,
    assignees: [],
    labels: [],
    estimate: null,
    due: null,
    links: [],
    custom: {},
  };
}

export function valuesFromItem(item: Item): FrontMatterValues {
  return {
    title: item.title,
    status: item.status ?? '',
    priority: item.priority ?? null,
    parent: item.parent ?? null,
    milestone: item.milestone ?? null,
    assignees: [...(item.assignees ?? [])],
    labels: [...(item.labels ?? [])],
    estimate: item.estimate ?? null,
    due: item.due ?? null,
    links: (item.links ?? []).map((l) => ({ ...l })),
    custom: { ...(item.custom ?? {}) },
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isBlank(value: string | null): boolean {
  return value === null || value.trim() === '';
}

/** Front-matter mapping in canonical key order, with empty values omitted. */
export function valuesToObject(values: FrontMatterValues): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (values.title.trim()) out.title = values.title.trim();
  if (values.status) out.status = values.status;
  if (values.priority) out.priority = values.priority;
  if (!isBlank(values.parent)) out.parent = values.parent?.trim();
  if (!isBlank(values.milestone)) out.milestone = values.milestone?.trim();
  if (values.assignees.length > 0) out.assignees = [...values.assignees];
  if (values.labels.length > 0) out.labels = [...values.labels];
  if (values.estimate !== null) out.estimate = values.estimate;
  if (!isBlank(values.due)) out.due = values.due?.trim();
  if (values.links.length > 0) {
    out.links = values.links.map((l) => ({
      kind: l.kind,
      target: l.target,
      ...(l.note ? { note: l.note } : {}),
    }));
  }
  if (Object.keys(values.custom).length > 0) out.custom = { ...values.custom };
  return out;
}

/** The editable front matter as YAML text (the "Raw YAML" view). */
export function valuesToYaml(values: FrontMatterValues): string {
  const obj = valuesToObject(values);
  if (Object.keys(obj).length === 0) return '';
  return stringify(obj, { lineWidth: 0 });
}

export type YamlParseResult =
  | { ok: true; values: FrontMatterValues }
  | { ok: false; issues: Diagnostic[] };

function issue(code: string, message: string, field?: string): Diagnostic {
  return { code, severity: 'error', message, ...(field ? { field } : {}) };
}

function readLinks(raw: unknown, issues: Diagnostic[]): Link[] {
  if (raw === undefined || raw === null) return [];
  if (!Array.isArray(raw)) {
    issues.push(issue('E-FM-YAML', '`links` must be a list of {kind, target}', 'links'));
    return [];
  }
  const links: Link[] = [];
  for (const entry of raw) {
    if (!isRecord(entry)) {
      issues.push(issue('E-FM-YAML', '`links` entries must be mappings', 'links'));
      continue;
    }
    const kind = entry.kind;
    const target = entry.target;
    if (typeof kind !== 'string' || !linkKinds.includes(kind as LinkKind)) {
      issues.push(
        issue('E-ENUM', `Unknown link kind: ${String(kind)}. Use one of ${linkKinds.join(', ')}`, 'links'),
      );
      continue;
    }
    if (typeof target !== 'string' || target.trim() === '') {
      issues.push(issue('E-FM-YAML', '`links` entries need a `target`', 'links'));
      continue;
    }
    const note = entry.note;
    links.push({
      kind: kind as LinkKind,
      target: target.trim(),
      ...(typeof note === 'string' && note ? { note } : {}),
    });
  }
  return links;
}

function readStrings(raw: unknown, field: string, issues: Diagnostic[]): string[] {
  if (raw === undefined || raw === null) return [];
  if (typeof raw === 'string') return [raw];
  if (!Array.isArray(raw)) {
    issues.push(issue('E-FM-YAML', `\`${field}\` must be a list of strings`, field));
    return [];
  }
  const out: string[] = [];
  for (const entry of raw) {
    if (typeof entry !== 'string') {
      issues.push(issue('E-FM-YAML', `\`${field}\` must be a list of strings`, field));
      continue;
    }
    if (entry.trim()) out.push(entry.trim());
  }
  return out;
}

function readOptionalString(raw: unknown, field: string, issues: Diagnostic[]): string | null {
  if (raw === undefined || raw === null) return null;
  if (typeof raw !== 'string') {
    issues.push(issue('E-FM-YAML', `\`${field}\` must be a string`, field));
    return null;
  }
  return raw.trim() === '' ? null : raw.trim();
}

/** Parses the raw YAML view back into form values; never throws. */
export function valuesFromYaml(text: string): YamlParseResult {
  const issues: Diagnostic[] = [];
  let doc: unknown;
  if (text.trim() === '') {
    return { ok: true, values: emptyValues() };
  }
  try {
    doc = parse(text) as unknown;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return { ok: false, issues: [issue('E-FM-YAML', message)] };
  }
  if (!isRecord(doc)) {
    return { ok: false, issues: [issue('E-FM-YAML', 'Front matter must be a YAML mapping')] };
  }

  const title = doc.title;
  if (title !== undefined && typeof title !== 'string') {
    issues.push(issue('E-TITLE', '`title` must be a string', 'title'));
  }
  const status = doc.status;
  if (status !== undefined && typeof status !== 'string') {
    issues.push(issue('E-FM-YAML', '`status` must be a string', 'status'));
  }
  const rawPriority = doc.priority;
  let priority: Priority | null = null;
  if (rawPriority !== undefined && rawPriority !== null) {
    if (typeof rawPriority === 'string' && priorities.includes(rawPriority as Priority)) {
      priority = rawPriority as Priority;
    } else {
      issues.push(
        issue('E-ENUM', `\`priority\` must be one of ${priorities.join(', ')}`, 'priority'),
      );
    }
  }
  const rawEstimate = doc.estimate;
  let estimate: number | null = null;
  if (rawEstimate !== undefined && rawEstimate !== null) {
    if (typeof rawEstimate === 'number' && Number.isFinite(rawEstimate)) {
      estimate = rawEstimate;
    } else {
      issues.push(issue('E-FM-YAML', '`estimate` must be a number', 'estimate'));
    }
  }
  const rawCustom = doc.custom;
  let custom: Record<string, unknown> = {};
  if (rawCustom !== undefined && rawCustom !== null) {
    if (isRecord(rawCustom)) {
      custom = { ...rawCustom };
    } else {
      issues.push(issue('E-FM-YAML', '`custom` must be a mapping', 'custom'));
    }
  }

  const values: FrontMatterValues = {
    title: typeof title === 'string' ? title : '',
    status: typeof status === 'string' ? status : '',
    priority,
    parent: readOptionalString(doc.parent, 'parent', issues),
    milestone: readOptionalString(doc.milestone, 'milestone', issues),
    assignees: readStrings(doc.assignees, 'assignees', issues),
    labels: readStrings(doc.labels, 'labels', issues),
    estimate,
    due: readOptionalString(doc.due, 'due', issues),
    links: readLinks(doc.links, issues),
    custom,
  };

  if (issues.length > 0) return { ok: false, issues };
  return { ok: true, values };
}

/**
 * Local, synchronous validation used while typing. The core's `validateItem`
 * remains authoritative and runs before every save.
 */
export function validateValues(
  values: FrontMatterValues,
  schema: EditorProjectSchema,
  type: ItemType,
): Diagnostic[] {
  const out: Diagnostic[] = [];
  const push = (code: string, severity: Diagnostic['severity'], message: string, field: string) => {
    out.push({ code, severity, message, field });
  };

  if (values.title.trim() === '') {
    push('E-TITLE', 'error', 'Title is required', 'title');
  } else if (values.title.trim().length > 200) {
    push('E-TITLE', 'error', 'Title must be 200 characters or fewer', 'title');
  }

  if (schema.statuses.length > 0 && !schema.statuses.some((s) => s.id === values.status)) {
    push('E-STATUS-UNKNOWN', 'error', `Status "${values.status}" is not in the workflow`, 'status');
  }

  if (values.priority && !schema.priorities.includes(values.priority)) {
    push('E-ENUM', 'error', `Priority "${values.priority}" is not declared`, 'priority');
  }

  if (!isBlank(values.due) && !datePattern.test(values.due ?? '')) {
    push('E-DATE-FORMAT', 'error', '`due` must be a YYYY-MM-DD date', 'due');
  }

  if (type === 'epic' && !isBlank(values.parent)) {
    push('E-REF-PARENT-TYPE', 'error', 'An epic cannot have a parent', 'parent');
  }

  for (const field of ['parent', 'milestone'] as const) {
    const value = values[field];
    if (!isBlank(value) && !idPattern.test(value ?? '')) {
      push('W-REF-DANGLING', 'warning', `"${value ?? ''}" is not a valid item id`, field);
    }
  }

  for (const link of values.links) {
    if (!idPattern.test(link.target)) {
      push('W-REF-DANGLING', 'warning', `Link target "${link.target}" is not a valid item id`, 'links');
    }
  }

  const scale = schema.estimation;
  if (
    values.estimate !== null &&
    (scale.scale === 'fibonacci' || scale.scale === 'linear') &&
    scale.values.length > 0 &&
    !scale.values.includes(values.estimate)
  ) {
    push(
      'W-ESTIMATE-SCALE',
      'warning',
      `Estimate ${values.estimate} is not in the ${scale.scale} scale (${scale.values.join(', ')})`,
      'estimate',
    );
  }

  if (schema.labels.length > 0) {
    for (const label of values.labels) {
      if (!schema.labels.includes(label)) {
        push('W-LABEL-UNDECLARED', 'warning', `Label "${label}" is not in the catalog`, 'labels');
      }
    }
  }

  const declared = customFieldsFor(schema, type);
  for (const [key, value] of Object.entries(values.custom)) {
    const def = declared.find((d) => d.key === key);
    if (!def) {
      push('W-CF-UNDECLARED', 'warning', `Custom field "${key}" is not declared`, `custom.${key}`);
      continue;
    }
    if (def.type === 'enum' && def.values.length > 0 && typeof value === 'string') {
      if (value !== '' && !def.values.includes(value)) {
        push(
          'E-ENUM',
          'error',
          `Custom field "${key}" must be one of ${def.values.join(', ')}`,
          `custom.${key}`,
        );
      }
    }
    if (def.type === 'number' && value !== null && value !== '' && typeof value !== 'number') {
      push('E-CF-TYPE', 'error', `Custom field "${key}" must be a number`, `custom.${key}`);
    }
    if (def.type === 'bool' && typeof value !== 'boolean') {
      push('E-CF-TYPE', 'error', `Custom field "${key}" must be true or false`, `custom.${key}`);
    }
    if (def.type === 'date' && typeof value === 'string' && value !== '' && !datePattern.test(value)) {
      push('E-DATE-FORMAT', 'error', `Custom field "${key}" must be a YYYY-MM-DD date`, `custom.${key}`);
    }
  }

  return out;
}

export function hasErrors(diagnostics: Diagnostic[]): boolean {
  return diagnostics.some((d) => d.severity === 'error');
}

function sameArray(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i]);
}

function sameLinks(a: readonly Link[], b: readonly Link[]): boolean {
  return (
    a.length === b.length &&
    a.every((l, i) => {
      const other = b[i];
      return (
        other !== undefined &&
        l.kind === other.kind &&
        l.target === other.target &&
        (l.note ?? '') === (other.note ?? '')
      );
    })
  );
}

function sameCustom(a: Record<string, unknown>, b: Record<string, unknown>): boolean {
  const ka = Object.keys(a).sort();
  const kb = Object.keys(b).sort();
  if (!sameArray(ka, kb)) return false;
  return ka.every((k) => JSON.stringify(a[k]) === JSON.stringify(b[k]));
}

/**
 * The smallest patch that turns `item` into `values` + `body`: `set` for
 * changed fields, `unset` for cleared ones, `body` only when it changed.
 */
export function buildPatch(item: Item, values: FrontMatterValues, body: string): ItemPatch {
  const set: Record<string, unknown> = {};
  const unset: string[] = [];

  const title = values.title.trim();
  if (title !== '' && title !== item.title) set.title = title;
  if (values.status !== '' && values.status !== item.status) set.status = values.status;

  const scalars = [
    ['priority', values.priority, item.priority],
    ['parent', isBlank(values.parent) ? null : values.parent?.trim(), item.parent],
    ['milestone', isBlank(values.milestone) ? null : values.milestone?.trim(), item.milestone],
    ['due', isBlank(values.due) ? null : values.due?.trim(), item.due],
  ] as const;
  for (const [field, next, current] of scalars) {
    if (next === null || next === undefined) {
      if (current !== undefined && current !== null) unset.push(field);
    } else if (next !== current) {
      set[field] = next;
    }
  }

  if (values.estimate === null) {
    if (item.estimate !== undefined && item.estimate !== null) unset.push('estimate');
  } else if (values.estimate !== item.estimate) {
    set.estimate = values.estimate;
  }

  const lists = [
    ['assignees', values.assignees, item.assignees ?? []],
    ['labels', values.labels, item.labels ?? []],
  ] as const;
  for (const [field, next, current] of lists) {
    if (next.length === 0) {
      if (current.length > 0) unset.push(field);
    } else if (!sameArray(next, current)) {
      set[field] = [...next];
    }
  }

  if (values.links.length === 0) {
    if ((item.links ?? []).length > 0) unset.push('links');
  } else if (!sameLinks(values.links, item.links ?? [])) {
    set.links = values.links.map((l) => ({ ...l }));
  }

  const custom = values.custom;
  if (Object.keys(custom).length === 0) {
    if (Object.keys(item.custom ?? {}).length > 0) unset.push('custom');
  } else if (!sameCustom(custom, item.custom ?? {})) {
    set.custom = { ...custom };
  }

  const patch: ItemPatch = {};
  if (Object.keys(set).length > 0) patch.set = set;
  if (unset.length > 0) patch.unset = unset;
  if (body !== item.body) patch.body = body;
  return patch;
}

export function isEmptyPatch(patch: ItemPatch): boolean {
  return patch.set === undefined && patch.unset === undefined && patch.body === undefined;
}

/**
 * Whole-file text for a pre-save `validateItem({ text })`: canonical front
 * matter (identity fields included) followed by the body.
 */
export function serializeItem(
  item: Pick<Item, 'id' | 'type' | 'created' | 'updated' | 'author'>,
  values: FrontMatterValues,
  body: string,
): string {
  const head: Record<string, unknown> = { id: item.id, type: item.type };
  const rest = valuesToObject(values);
  const merged: Record<string, unknown> = { ...head };
  for (const key of ['title', 'status', 'priority', 'parent', 'milestone'] as const) {
    if (key in rest) merged[key] = rest[key];
  }
  if ('assignees' in rest) merged.assignees = rest.assignees;
  if (item.author) merged.author = item.author;
  for (const key of ['labels', 'estimate'] as const) {
    if (key in rest) merged[key] = rest[key];
  }
  if (item.created) merged.created = item.created;
  if (item.updated) merged.updated = item.updated;
  if ('due' in rest) merged.due = rest.due;
  for (const key of ['links', 'custom'] as const) {
    if (key in rest) merged[key] = rest[key];
  }
  const front = stringify(merged, { lineWidth: 0 });
  return `---\n${front}---\n\n${body}`;
}
