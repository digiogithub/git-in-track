import { useEffect, useState } from 'react';

import type { Diagnostic, ItemType, Priority } from '@/api/provider';
import { CustomFieldsEditor } from '@/components/editor/CustomFieldsEditor';
import { DiagnosticList, FieldIssue } from '@/components/editor/DiagnosticList';
import { ItemPicker } from '@/components/editor/ItemPicker';
import { LinksEditor } from '@/components/editor/LinksEditor';
import { TagInput } from '@/components/editor/TagInput';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import type { FrontMatterValues } from '@/features/editor/front-matter';
import { valuesFromYaml, valuesToYaml } from '@/features/editor/front-matter';
import type { EditorProjectSchema } from '@/features/editor/project-schema';
import { allowedStatuses, customFieldsFor } from '@/features/editor/project-schema';
import { cn } from '@/lib/cn';

export type FrontMatterFormProps = {
  type: ItemType;
  values: FrontMatterValues;
  onChange: (values: FrontMatterValues) => void;
  schema: EditorProjectSchema;
  projectKey: string;
  /** Diagnostics from local validation and from the core, keyed by `field`. */
  diagnostics?: Diagnostic[];
  disabled?: boolean;
  className?: string;
};

const selectClass =
  'h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50';

/** Which item types can be a parent of `type` (docs/03-data-model.md §8, §9). */
function parentTypes(type: ItemType): ItemType[] {
  if (type === 'story') return ['epic'];
  if (type === 'task') return ['story', 'epic'];
  return [];
}

/**
 * Front-matter editor driven by `project.yaml`: a generated form plus a
 * two-way "Raw YAML" view (docs/05-web-app.md §8).
 */
export function FrontMatterForm({
  type,
  values,
  onChange,
  schema,
  projectKey,
  diagnostics = [],
  disabled = false,
  className,
}: FrontMatterFormProps) {
  const [raw, setRaw] = useState(false);
  const [rawText, setRawText] = useState(() => valuesToYaml(values));
  const [rawIssues, setRawIssues] = useState<Diagnostic[]>([]);

  // While the raw view is closed it mirrors the form; it must not fight the
  // user's typing while it is open.
  useEffect(() => {
    if (!raw) setRawText(valuesToYaml(values));
  }, [raw, values]);

  const patch = (next: Partial<FrontMatterValues>) => {
    onChange({ ...values, ...next });
  };

  const statuses = allowedStatuses(schema, values.status);
  const customFields = customFieldsFor(schema, type);
  const parents = parentTypes(type);
  const estimateSuggestions = schema.estimation.values;

  const applyRaw = (text: string) => {
    setRawText(text);
    const result = valuesFromYaml(text);
    if (result.ok) {
      setRawIssues([]);
      onChange(result.values);
      return;
    }
    setRawIssues(result.issues);
  };

  return (
    <section className={cn('space-y-4', className)} aria-label="Front matter">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
          Front matter
        </h2>
        <div className="flex items-center gap-2">
          <Label htmlFor="raw-yaml-toggle">Raw YAML</Label>
          <Switch
            id="raw-yaml-toggle"
            aria-label="Raw YAML"
            checked={raw}
            disabled={disabled}
            onCheckedChange={(next) => {
              if (next) {
                setRawText(valuesToYaml(values));
                setRawIssues([]);
                setRaw(true);
                return;
              }
              const result = valuesFromYaml(rawText);
              if (!result.ok) {
                setRawIssues(result.issues);
                return;
              }
              setRawIssues([]);
              onChange(result.values);
              setRaw(false);
            }}
          />
        </div>
      </div>

      {raw ? (
        <div className="space-y-2">
          <Label htmlFor="raw-yaml">Front matter YAML</Label>
          <Textarea
            id="raw-yaml"
            rows={16}
            spellCheck={false}
            disabled={disabled}
            value={rawText}
            aria-invalid={rawIssues.length > 0}
            onChange={(event) => {
              applyRaw(event.target.value);
            }}
          />
          <p className="text-xs text-muted-foreground">
            <code>id</code>, <code>type</code>, <code>created</code>, <code>updated</code> and{' '}
            <code>author</code> are maintained by git-in-track and are not edited here.
          </p>
          <DiagnosticList diagnostics={rawIssues} title="YAML errors" />
          {rawIssues.length > 0 ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setRawText(valuesToYaml(values));
                setRawIssues([]);
              }}
            >
              Restore last valid
            </Button>
          ) : null}
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          <div className="md:col-span-2">
            <Label htmlFor="fm-title">Title</Label>
            <Input
              id="fm-title"
              value={values.title}
              disabled={disabled}
              onChange={(event) => {
                patch({ title: event.target.value });
              }}
            />
            <FieldIssue diagnostics={diagnostics} field="title" />
          </div>

          <div>
            <Label htmlFor="fm-status">Status</Label>
            <select
              id="fm-status"
              className={selectClass}
              value={values.status}
              disabled={disabled}
              onChange={(event) => {
                patch({ status: event.target.value });
              }}
            >
              {statuses.map((status) => (
                <option key={status.id} value={status.id}>
                  {status.name}
                </option>
              ))}
            </select>
            <FieldIssue diagnostics={diagnostics} field="status" />
          </div>

          <div>
            <Label htmlFor="fm-priority">Priority</Label>
            <select
              id="fm-priority"
              className={selectClass}
              value={values.priority ?? ''}
              disabled={disabled}
              onChange={(event) => {
                const next = event.target.value;
                patch({ priority: next === '' ? null : (next as Priority) });
              }}
            >
              <option value="">—</option>
              {schema.priorities.map((priority) => (
                <option key={priority} value={priority}>
                  {priority}
                </option>
              ))}
            </select>
            <FieldIssue diagnostics={diagnostics} field="priority" />
          </div>

          {parents.length > 0 ? (
            <div>
              <Label htmlFor="fm-parent">Parent</Label>
              <ItemPicker
                id="fm-parent"
                label="Parent"
                value={values.parent}
                projectKey={projectKey}
                types={parents}
                disabled={disabled}
                onChange={(next) => {
                  patch({ parent: next });
                }}
              />
              <FieldIssue diagnostics={diagnostics} field="parent" />
            </div>
          ) : null}

          {type !== 'milestone' ? (
            <div>
              <Label htmlFor="fm-milestone">Milestone</Label>
              <ItemPicker
                id="fm-milestone"
                label="Milestone"
                value={values.milestone}
                projectKey={projectKey}
                types={['milestone']}
                disabled={disabled}
                onChange={(next) => {
                  patch({ milestone: next });
                }}
              />
              <FieldIssue diagnostics={diagnostics} field="milestone" />
            </div>
          ) : null}

          <div>
            <Label htmlFor="fm-assignees">Assignees</Label>
            <TagInput
              id="fm-assignees"
              label="Assignees"
              values={values.assignees}
              disabled={disabled}
              placeholder="handle, then Enter"
              onChange={(next) => {
                patch({ assignees: next });
              }}
            />
          </div>

          <div>
            <Label htmlFor="fm-labels">Labels</Label>
            <TagInput
              id="fm-labels"
              label="Labels"
              values={values.labels}
              suggestions={schema.labels}
              disabled={disabled}
              onChange={(next) => {
                patch({ labels: next });
              }}
            />
            <FieldIssue diagnostics={diagnostics} field="labels" />
          </div>

          <div>
            <Label htmlFor="fm-estimate">Estimate</Label>
            <Input
              id="fm-estimate"
              type="number"
              inputMode="decimal"
              list="estimate-scale"
              value={values.estimate === null ? '' : String(values.estimate)}
              disabled={disabled}
              onChange={(event) => {
                const next = event.target.value;
                patch({ estimate: next === '' ? null : Number(next) });
              }}
            />
            {estimateSuggestions.length > 0 ? (
              <datalist id="estimate-scale">
                {estimateSuggestions.map((value) => (
                  <option key={value} value={value} />
                ))}
              </datalist>
            ) : null}
            <FieldIssue diagnostics={diagnostics} field="estimate" />
          </div>

          <div>
            <Label htmlFor="fm-due">Due</Label>
            <Input
              id="fm-due"
              type="date"
              value={values.due ?? ''}
              disabled={disabled}
              onChange={(event) => {
                patch({ due: event.target.value === '' ? null : event.target.value });
              }}
            />
            <FieldIssue diagnostics={diagnostics} field="due" />
          </div>

          <div className="md:col-span-2">
            <Label>Links</Label>
            <LinksEditor
              links={values.links}
              disabled={disabled}
              onChange={(next) => {
                patch({ links: next });
              }}
            />
            <FieldIssue diagnostics={diagnostics} field="links" />
          </div>

          <div className="md:col-span-2">
            <CustomFieldsEditor
              fields={customFields}
              values={values.custom}
              diagnostics={diagnostics}
              disabled={disabled}
              onChange={(next) => {
                patch({ custom: next });
              }}
            />
          </div>
        </div>
      )}
    </section>
  );
}
