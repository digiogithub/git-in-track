/**
 * Editor view of `project.yaml` (docs/03-data-model.md §6).
 *
 * `ProjectSummary` is the shape the provider guarantees today; the workflow
 * transitions, the estimation scale and the custom-field catalogue are optional
 * extras that a richer provider may attach. They are read defensively here so
 * the editor degrades gracefully instead of crashing when they are absent.
 */

import type { ItemType, Priority, ProjectSummary } from '@/api/provider';

export type StatusDef = ProjectSummary['statuses'][number];

export type CustomFieldType =
  | 'string'
  | 'text'
  | 'number'
  | 'bool'
  | 'date'
  | 'timestamp'
  | 'enum'
  | 'person'
  | 'list'
  | 'url';

export type CustomFieldDef = {
  key: string;
  type: CustomFieldType;
  values: string[];
  appliesTo: ItemType[];
};

export type EstimationScale = 'fibonacci' | 'linear' | 'tshirt' | 'none';

export type EditorProjectSchema = {
  key: string;
  name: string;
  statuses: StatusDef[];
  initialStatus: string;
  /** `from -> [to...]`; `null` means every transition is allowed. */
  transitions: Record<string, string[]> | null;
  priorities: Priority[];
  labels: string[];
  estimation: { scale: EstimationScale; values: number[] };
  customFields: CustomFieldDef[];
};

const defaultPriorities: Priority[] = ['critical', 'high', 'medium', 'low'];
const customFieldTypes = new Set<string>([
  'string',
  'text',
  'number',
  'bool',
  'date',
  'timestamp',
  'enum',
  'person',
  'list',
  'url',
]);
const itemTypes = new Set<string>(['epic', 'story', 'task', 'milestone', 'comment']);

const scaleDefaults: Record<EstimationScale, number[]> = {
  fibonacci: [1, 2, 3, 5, 8, 13, 21],
  linear: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
  tshirt: [],
  none: [],
};

export const emptySchema: EditorProjectSchema = {
  key: '',
  name: '',
  statuses: [],
  initialStatus: '',
  transitions: null,
  priorities: defaultPriorities,
  labels: [],
  estimation: { scale: 'fibonacci', values: scaleDefaults.fibonacci },
  customFields: [],
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function readStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((entry): entry is string => typeof entry === 'string');
}

function readTransitions(value: unknown): Record<string, string[]> | null {
  if (!isRecord(value)) return null;
  const out: Record<string, string[]> = {};
  for (const [from, to] of Object.entries(value)) {
    out[from] = readStringArray(to);
  }
  return Object.keys(out).length > 0 ? out : null;
}

function readEstimation(value: unknown): EditorProjectSchema['estimation'] {
  if (!isRecord(value)) return emptySchema.estimation;
  const rawScale = value.scale;
  const scale: EstimationScale =
    rawScale === 'linear' || rawScale === 'tshirt' || rawScale === 'none' ? rawScale : 'fibonacci';
  const values = Array.isArray(value.values)
    ? value.values.filter((entry): entry is number => typeof entry === 'number')
    : scaleDefaults[scale];
  return { scale, values };
}

function readCustomFields(value: unknown): CustomFieldDef[] {
  if (!Array.isArray(value)) return [];
  const out: CustomFieldDef[] = [];
  for (const entry of value) {
    if (!isRecord(entry)) continue;
    const key = entry.key;
    if (typeof key !== 'string' || key.length === 0) continue;
    const rawType = entry.type;
    const type: CustomFieldType =
      typeof rawType === 'string' && customFieldTypes.has(rawType)
        ? (rawType as CustomFieldType)
        : 'string';
    const appliesTo = readStringArray(entry.applies_to ?? entry.appliesTo).filter(
      (t): t is ItemType => itemTypes.has(t),
    );
    out.push({ key, type, values: readStringArray(entry.values), appliesTo });
  }
  return out;
}

/** Normalises a `ProjectSummary` (plus optional extras) into the editor schema. */
export function readProjectSchema(project: ProjectSummary | undefined): EditorProjectSchema {
  if (!project) return emptySchema;
  const extras = project as unknown as Record<string, unknown>;
  const workflow = isRecord(extras.workflow) ? extras.workflow : {};
  const statuses = project.statuses ?? [];
  const initial = workflow.initial ?? extras.initial;
  return {
    key: project.key,
    name: project.name,
    statuses,
    initialStatus:
      typeof initial === 'string' && initial.length > 0 ? initial : (statuses[0]?.id ?? ''),
    transitions: readTransitions(workflow.transitions ?? extras.transitions),
    priorities: project.priorities?.length ? project.priorities : defaultPriorities,
    labels: (project.labels ?? []).map((l) => l.name),
    estimation: readEstimation(extras.estimation),
    customFields: readCustomFields(extras.custom_fields ?? extras.customFields),
  };
}

/**
 * Statuses offered by the picker: the current one plus everything the workflow
 * allows to move to. Without a declared transition map every status is offered.
 */
export function allowedStatuses(schema: EditorProjectSchema, current: string): StatusDef[] {
  if (!schema.transitions) return schema.statuses;
  const allowed = new Set<string>([current, ...(schema.transitions[current] ?? [])]);
  const filtered = schema.statuses.filter((s) => allowed.has(s.id));
  return filtered.length > 0 ? filtered : schema.statuses;
}

/** Custom fields declared for this item type (an empty `applies_to` means all types). */
export function customFieldsFor(schema: EditorProjectSchema, type: ItemType): CustomFieldDef[] {
  return schema.customFields.filter((f) => f.appliesTo.length === 0 || f.appliesTo.includes(type));
}
