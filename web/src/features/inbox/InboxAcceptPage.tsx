import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';

import type { Diagnostic, Item } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { DiagnosticList } from '@/components/editor/DiagnosticList';
import { FrontMatterForm } from '@/components/editor/FrontMatterForm';
import { MarkdownEditor } from '@/components/editor/MarkdownEditor';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import { backlogKeys, useProject } from '@/features/backlog/queries';
import { ConflictDialog } from '@/features/editor/ConflictDialog';
import type { FrontMatterValues } from '@/features/editor/front-matter';
import {
  buildPatch,
  hasErrors,
  isEmptyPatch,
  validateValues,
  valuesFromItem,
} from '@/features/editor/front-matter';
import { readProjectSchema } from '@/features/editor/project-schema';
import { inboxKeys } from '@/features/inbox/queries';
import { acceptStatus } from '@/features/inbox/search';

/**
 * Accepting a submission into the backlog.
 *
 * This is the ordinary edit form — the same `FrontMatterForm` and
 * `MarkdownEditor` the item editor is built from — with two differences that
 * matter and one that does not.
 *
 * The two that matter: the status field arrives defaulted to the workflow's
 * initial non-triage status, because "accept" means "into the ordinary
 * backlog"; and saving commits the acceptance itself rather than a plain patch,
 * so a person cannot half-accept an item by editing it and walking away.
 *
 * The one that does not: there is no type picker, and there never will be. An
 * item id encodes its type for life (R-ID-3), so a submission filed as a task
 * that should have been an epic is answered by creating the epic and marking
 * this one a duplicate of it — which is what the Duplicate action is for.
 */
export function InboxAcceptPage() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const id = params.id ?? '';
  const provider = useProvider();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();

  const projectQuery = useProject(projectKey);
  const itemQuery = useQuery({
    queryKey: backlogKeys.detail(projectKey, id),
    queryFn: () => provider.getItem(id),
    enabled: id.length > 0,
  });

  const schema = useMemo(() => readProjectSchema(projectQuery.data), [projectQuery.data]);
  const defaultStatus = useMemo(() => acceptStatus(projectQuery.data), [projectQuery.data]);

  const [base, setBase] = useState<Item | null>(null);
  const [values, setValues] = useState<FrontMatterValues | null>(null);
  const [body, setBody] = useState('');
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);
  const [saveError, setSaveError] = useState<string | null>(null);

  const item = itemQuery.data;

  useEffect(() => {
    if (!item || base?.rev === item.rev) return;
    setBase(item);
    // The one field the form does not simply mirror: the submission is sitting
    // in a triage status, which is precisely the status it must not keep.
    setValues({ ...valuesFromItem(item), status: defaultStatus });
    setBody(item.body);
  }, [item, base?.rev, defaultStatus]);

  const readOnly = !provider.capabilities.write;

  const accept = async (options: { overwrite?: boolean } = {}) => {
    if (!base || !values || saving) return;

    const local = validateValues(values, schema, base.type);
    setDiagnostics(local);
    if (hasErrors(local)) return;

    setSaving(true);
    setSaveError(null);
    try {
      const rev = options.overwrite ? (await provider.getItem(base.id)).rev : base.rev;
      // The acceptance is the write that clears triage and files the item: it
      // owns the status and the parent, so it goes first and carries them.
      const result = await provider.triageInboxItem({
        id: base.id,
        rev,
        action: 'accept',
        ...(values.status === '' ? {} : { status: values.status }),
        // An absent parent leaves the item's own alone, which is what an
        // unfiled submission wants; only a chosen one is sent.
        ...(values.parent ? { parent: values.parent } : {}),
      });

      // Anything else the person changed while they were here — a label, a
      // priority, the body — is the rest of the same save, applied against the
      // revision the acceptance produced.
      const patch = buildPatch(result.item, values, body);
      if (!isEmptyPatch(patch)) {
        await provider.updateItem(result.item.id, patch, result.item.rev);
      }

      void queryClient.invalidateQueries({ queryKey: inboxKeys.project(projectKey) });
      void queryClient.invalidateQueries({ queryKey: backlogKeys.project(projectKey) });
      toast({ title: `${base.id} accepted into the backlog` });
      void navigate({ to: '/p/$project/inbox', params: { project: projectKey } });
    } catch (error) {
      if (error instanceof ProviderError && error.code === 'stale_revision') {
        setConflict(true);
      } else if (error instanceof ProviderError && error.code === 'validation_failed') {
        setDiagnostics([{ code: error.code, severity: 'error', message: error.message }]);
      } else {
        setSaveError(error instanceof Error ? error.message : String(error));
      }
    } finally {
      setSaving(false);
    }
  };

  if (itemQuery.isPending) {
    return <p className="text-sm text-muted-foreground">Loading {id}…</p>;
  }

  if (itemQuery.isError || !base || !values) {
    return (
      <p className="text-sm text-destructive" role="alert">
        {itemQuery.error instanceof Error ? itemQuery.error.message : `Item ${id} not found`}
      </p>
    );
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-mono text-xs text-muted-foreground">{base.id}</p>
          <h1 className="page-title">Accept into the backlog</h1>
          <p className="text-sm text-muted-foreground">
            Pick where it lands. Saving clears the triage state in the same write.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="ghost"
            onClick={() => {
              void navigate({ to: '/p/$project/inbox', params: { project: projectKey } });
            }}
          >
            Cancel
          </Button>
          <Button
            variant="accent"
            disabled={saving || readOnly}
            onClick={() => {
              void accept();
            }}
          >
            {saving ? 'Accepting…' : 'Accept'}
          </Button>
        </div>
      </header>

      {saveError ? (
        <p className="text-sm text-destructive" role="alert">
          {saveError}
        </p>
      ) : null}

      <DiagnosticList diagnostics={diagnostics} title="Validation" />

      <FrontMatterForm
        type={base.type}
        values={values}
        schema={schema}
        projectKey={projectKey}
        diagnostics={diagnostics}
        disabled={readOnly}
        onChange={setValues}
      />

      <section className="space-y-2" aria-label="Body">
        <h2 className="section-label">Body</h2>
        <MarkdownEditor
          label="Item body"
          value={body}
          readOnly={readOnly}
          onChange={setBody}
        />
      </section>

      {conflict ? (
        <ConflictDialog
          itemId={base.id}
          busy={saving}
          onReload={() => {
            void itemQuery.refetch();
            setConflict(false);
          }}
          onOverwrite={() => {
            setConflict(false);
            void accept({ overwrite: true });
          }}
          onCancel={() => {
            setConflict(false);
          }}
        />
      ) : null}
    </div>
  );
}
