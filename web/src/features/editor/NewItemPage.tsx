import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams, useRouterState } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';

import type { Diagnostic, ItemDraft } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { DiagnosticList } from '@/components/editor/DiagnosticList';
import { FrontMatterForm } from '@/components/editor/FrontMatterForm';
import { MarkdownEditor } from '@/components/editor/MarkdownEditor';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import type { FrontMatterValues } from '@/features/editor/front-matter';
import { emptyValues, hasErrors, validateValues } from '@/features/editor/front-matter';
import { readProjectSchema } from '@/features/editor/project-schema';
import { parseNewItemSearch } from '@/features/editor/search';
import type { EditableItemType } from '@/features/editor/templates';
import { bodyTemplate, editableItemTypes, isPristineTemplate } from '@/features/editor/templates';

const selectClass =
  'h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring';

/** Create an epic, story, task or milestone and open its detail page. */
export function NewItemPage() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const searchStr = useRouterState({ select: (state) => state.location.searchStr });
  const search = useMemo(() => parseNewItemSearch(searchStr), [searchStr]);
  const provider = useProvider();
  const navigate = useNavigate();

  const projectsQuery = useQuery({
    queryKey: ['projects'],
    queryFn: () => provider.listProjects(),
  });
  const schema = useMemo(
    () => readProjectSchema(projectsQuery.data?.find((p) => p.key === projectKey)),
    [projectsQuery.data, projectKey],
  );

  const [type, setType] = useState<EditableItemType>(search.type);
  const [body, setBody] = useState(() => bodyTemplate(search.type));
  const [values, setValues] = useState<FrontMatterValues>(() => ({
    ...emptyValues(),
    parent: search.parent ?? null,
    milestone: search.milestone ?? null,
  }));
  const [creating, setCreating] = useState(false);
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);
  const [createError, setCreateError] = useState<string | null>(null);

  // The workflow arrives asynchronously; adopt its initial status once.
  useEffect(() => {
    if (schema.initialStatus === '') return;
    setValues((current) => (current.status === '' ? { ...current, status: schema.initialStatus } : current));
  }, [schema.initialStatus]);

  const changeType = (next: EditableItemType) => {
    setType(next);
    setBody((current) => (isPristineTemplate(current) ? bodyTemplate(next) : current));
    if (next === 'epic' || next === 'milestone') {
      setValues((current) => ({ ...current, parent: null }));
    }
  };

  const localDiagnostics = useMemo(
    () => validateValues(values, schema, type),
    [values, schema, type],
  );
  const shown = diagnostics.length > 0 ? diagnostics : localDiagnostics;

  const create = async () => {
    const local = validateValues(values, schema, type);
    setDiagnostics(local);
    if (hasErrors(local)) return;

    setCreating(true);
    setCreateError(null);
    const draft: ItemDraft = {
      project: projectKey,
      type,
      title: values.title.trim(),
      ...(values.status ? { status: values.status } : {}),
      ...(values.priority ? { priority: values.priority } : {}),
      ...(values.parent ? { parent: values.parent } : {}),
      ...(values.milestone ? { milestone: values.milestone } : {}),
      ...(values.assignees.length > 0 ? { assignees: values.assignees } : {}),
      ...(values.labels.length > 0 ? { labels: values.labels } : {}),
      ...(values.estimate !== null ? { estimate: values.estimate } : {}),
      ...(values.due ? { due: values.due } : {}),
      ...(values.links.length > 0 ? { links: values.links } : {}),
      ...(Object.keys(values.custom).length > 0 ? { custom: values.custom } : {}),
      body,
    };
    try {
      const created = await provider.createItem(draft);
      await navigate({
        to: '/p/$project/items/$id',
        params: { project: projectKey, id: created.id },
      });
    } catch (err) {
      if (err instanceof ProviderError && err.code === 'validation_failed') {
        setDiagnostics([{ code: err.code, severity: 'error', message: err.message }]);
      } else {
        setCreateError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setCreating(false);
    }
  };

  const readOnly = !provider.capabilities.write;

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-mono text-xs text-muted-foreground">{projectKey}</p>
          <h1 className="text-2xl font-semibold tracking-tight">New item</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            onClick={() => {
              void navigate({ to: '/p/$project/items', params: { project: projectKey } });
            }}
          >
            Cancel
          </Button>
          <Button
            disabled={creating || readOnly}
            onClick={() => {
              void create();
            }}
          >
            {creating ? 'Creating…' : 'Create'}
          </Button>
        </div>
      </header>

      {readOnly ? (
        <p className="text-sm text-muted-foreground" role="status">
          This workspace is read-only.
        </p>
      ) : null}
      {createError ? (
        <p className="text-sm text-destructive" role="alert">
          {createError}
        </p>
      ) : null}

      <div className="max-w-xs">
        <Label htmlFor="new-item-type">Type</Label>
        <select
          id="new-item-type"
          className={selectClass}
          value={type}
          disabled={readOnly}
          onChange={(event) => {
            changeType(event.target.value as EditableItemType);
          }}
        >
          {editableItemTypes.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
      </div>

      <DiagnosticList diagnostics={shown} title="Validation" />

      <FrontMatterForm
        type={type}
        values={values}
        schema={schema}
        projectKey={projectKey}
        diagnostics={shown}
        disabled={readOnly}
        onChange={setValues}
      />

      <section className="space-y-2" aria-label="Body">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">Body</h2>
        <MarkdownEditor label="Item body" value={body} readOnly={readOnly} onChange={setBody} />
      </section>
    </div>
  );
}
