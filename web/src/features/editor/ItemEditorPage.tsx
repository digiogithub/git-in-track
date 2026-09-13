import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useBlocker, useNavigate, useParams } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import type { Diagnostic, Item } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { useAppStore } from '@/app/store';
import { DiagnosticList } from '@/components/editor/DiagnosticList';
import { FrontMatterForm } from '@/components/editor/FrontMatterForm';
import { MarkdownEditor } from '@/components/editor/MarkdownEditor';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { useToast } from '@/components/ui/toast';
import { backlogKeys } from '@/features/backlog/queries';
import { ConflictDialog } from '@/features/editor/ConflictDialog';
import { clearDraft, draftKey, readDraft, writeDraft } from '@/features/editor/drafts';
import type { FrontMatterValues } from '@/features/editor/front-matter';
import {
  buildPatch,
  hasErrors,
  isEmptyPatch,
  serializeItem,
  validateValues,
  valuesFromItem,
} from '@/features/editor/front-matter';
import { readProjectSchema } from '@/features/editor/project-schema';
import { inboxKeys } from '@/features/inbox/queries';
import { acceptStatus } from '@/features/inbox/search';

/**
 * Query keys follow the backlog convention documented in
 * `features/backlog/queries.ts`: `['items', <projectKey>, …]`, so a save
 * invalidates exactly the project subtree the list and detail views read.
 */
const detailKey = (project: string, id: string) => ['items', project, 'detail', id] as const;

const autosaveDelayMs = 2_000;

/** "14:32 on 6 September" — when a recovered draft was last typed into. */
function formatDraftTime(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? 'an earlier session' : at.toLocaleString();
}

/**
 * How the page is being used. The form is the same either way — that is the
 * point of the prop.
 *
 * `edit` is the ordinary editor: a rev-checked patch, autosave, drafts and a
 * guard on leaving with unsaved changes. `accept` is triage taking a submission
 * into the backlog (ADR-033), which is the same form with a different verb:
 * the status arrives defaulted to the workflow's initial non-triage status, and
 * saving commits the acceptance itself rather than a plain patch, so a person
 * cannot half-accept an item by editing it and walking away.
 *
 * Neither mode offers a type picker, and neither ever will: an item id encodes
 * its type for life (R-ID-3), so a submission filed as a task that should have
 * been an epic is answered by creating the epic and marking this one a
 * duplicate of it.
 */
export type ItemEditorMode = 'edit' | 'accept';

/** Item editor: front matter form + CodeMirror body, rev-checked saves. */
export function ItemEditorPage({ mode = 'edit' }: { mode?: ItemEditorMode } = {}) {
  const accepting = mode === 'accept';
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const id = params.id ?? '';
  const provider = useProvider();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const companionUrl = useAppStore((state) => state.companionUrl);
  const storageKey = draftKey(companionUrl, projectKey, id);

  const projectsQuery = useQuery({
    queryKey: ['projects'],
    queryFn: () => provider.listProjects(),
  });
  const itemQuery = useQuery({
    queryKey: detailKey(projectKey, id),
    queryFn: () => provider.getItem(id),
    enabled: id.length > 0,
  });
  const referencesQuery = useQuery({
    queryKey: ['items', projectKey, 'list', 'references'],
    queryFn: () => provider.listItems({ project: projectKey, limit: 200, sort: 'id' }),
    enabled: projectKey.length > 0,
  });

  const [base, setBase] = useState<Item | null>(null);
  const [values, setValues] = useState<FrontMatterValues | null>(null);
  const [body, setBody] = useState('');
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [autosave, setAutosave] = useState(false);
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [savedRev, setSavedRev] = useState<string | null>(null);
  const loadedRev = useRef<string | null>(null);
  // The draft found in storage when the editor opened, until the user has said
  // what to do with it. It is read once, on mount: a draft that arrives later
  // would be this tab's own writing.
  const [offered, setOffered] = useState(() => (accepting ? null : readDraft(storageKey)));

  const item = itemQuery.data;
  const project = useMemo(
    () => projectsQuery.data?.find((p) => p.key === projectKey),
    [projectsQuery.data, projectKey],
  );
  const schema = useMemo(() => readProjectSchema(project), [project]);
  // The one field accepting does not simply mirror: the submission is sitting
  // in a triage status, which is precisely the status it must not keep.
  const defaultStatus = useMemo(
    () => (accepting ? acceptStatus(project) : ''),
    [accepting, project],
  );
  const references = useMemo(
    () => (referencesQuery.data?.items ?? []).map((i) => ({ id: i.id, title: i.title })),
    [referencesQuery.data],
  );

  // What "already loaded" means. In accept mode the default status is part of
  // it: the project list can arrive after the item, and the form must still end
  // up on the workflow status rather than on the triage one it was read with.
  const loadKey = useCallback(
    (rev: string) => `${rev}|${defaultStatus}`,
    [defaultStatus],
  );

  useEffect(() => {
    if (!item || dirty) return;
    if (loadedRev.current === loadKey(item.rev)) return;
    loadedRev.current = loadKey(item.rev);
    setBase(item);
    setValues(
      accepting
        ? { ...valuesFromItem(item), status: defaultStatus }
        : valuesFromItem(item),
    );
    setBody(item.body);
  }, [item, dirty, accepting, defaultStatus, loadKey]);

  // Every keystroke of an unsaved edit reaches storage, so a reload, a crash or
  // a closed tab finds the draft again. It is per-viewer convenience state and
  // never a source of truth: the file on disk is (docs/05-web-app.md §8.3).
  useEffect(() => {
    // Accepting is a one-shot form reached from the queue, never the surface a
    // half-written edit is left open on, so it keeps no draft.
    if (accepting || !dirty || !base || !values) return;
    writeDraft(storageKey, { rev: base.rev, values, body });
  }, [accepting, dirty, base, values, body, storageKey]);

  const localDiagnostics = useMemo(
    () => (values && base ? validateValues(values, schema, base.type) : []),
    [values, base, schema],
  );

  /**
   * Taking a submission into the backlog, as one save.
   *
   * The acceptance goes first and carries the two fields it owns — the status
   * that clears triage, and the parent that files the item — because those are
   * the write that makes the item ordinary. Anything else the person changed
   * while they were here is the rest of the same save, applied against the
   * revision the acceptance produced.
   */
  const acceptIntoBacklog = useCallback(
    async (item_: Item, rev: string, values_: FrontMatterValues, body_: string) => {
      const result = await provider.triageInboxItem({
        id: item_.id,
        rev,
        action: 'accept',
        ...(values_.status === '' ? {} : { status: values_.status }),
        // An absent parent leaves the item's own alone, which is what an
        // unfiled submission wants; only a chosen one is sent.
        ...(values_.parent ? { parent: values_.parent } : {}),
      });

      const rest = buildPatch(result.item, values_, body_);
      if (!isEmptyPatch(rest)) {
        await provider.updateItem(result.item.id, rest, result.item.rev);
      }

      setDirty(false);
      setConflict(false);
      void queryClient.invalidateQueries({ queryKey: inboxKeys.project(projectKey) });
      void queryClient.invalidateQueries({ queryKey: backlogKeys.project(projectKey) });
      toast({ title: `${item_.id} accepted into the backlog` });
      void navigate({ to: '/p/$project/inbox', params: { project: projectKey } });
    },
    [provider, queryClient, projectKey, toast, navigate],
  );

  const save = useCallback(
    async (options: { overwrite?: boolean } = {}) => {
      if (!base || !values || saving) return;

      const local = validateValues(values, schema, base.type);
      let all = local;
      if (!accepting) {
        try {
          const remote = await provider.validateItem({ text: serializeItem(base, values, body) });
          all = [...local, ...remote];
        } catch {
          // A provider without text validation is fine; local rules still apply.
        }
      }
      setDiagnostics(all);
      if (hasErrors(all)) return;

      // An acceptance is always a write, even when the form was not touched:
      // clearing the triage state is the point of pressing the button.
      const patch = buildPatch(base, values, body);
      if (!accepting && isEmptyPatch(patch)) {
        setDirty(false);
        return;
      }

      setSaving(true);
      setSaveError(null);
      try {
        let rev = base.rev;
        if (options.overwrite) {
          rev = (await provider.getItem(base.id)).rev;
        }
        if (accepting) {
          await acceptIntoBacklog(base, rev, values, body);
          return;
        }
        const saved = await provider.updateItem(base.id, patch, rev);
        // The edit is on disk; the draft has nothing left to protect.
        clearDraft(storageKey);
        setOffered(null);
        loadedRev.current = loadKey(saved.rev);
        setBase(saved);
        setSavedRev(saved.rev);
        setDirty(false);
        setConflict(false);
        queryClient.setQueryData(detailKey(projectKey, saved.id), saved);
        void queryClient.invalidateQueries({ queryKey: ['items', projectKey] });
      } catch (err) {
        if (err instanceof ProviderError && err.code === 'stale_revision') {
          setConflict(true);
        } else if (err instanceof ProviderError && err.code === 'validation_failed') {
          setDiagnostics([{ code: err.code, severity: 'error', message: err.message }]);
        } else {
          setSaveError(err instanceof Error ? err.message : String(err));
        }
      } finally {
        setSaving(false);
      }
    },
    [
      base,
      values,
      body,
      saving,
      schema,
      accepting,
      acceptIntoBacklog,
      provider,
      queryClient,
      projectKey,
      storageKey,
      loadKey,
    ],
  );

  useEffect(() => {
    if (!autosave || !dirty || conflict || saving) return undefined;
    const timer = setTimeout(() => {
      void save();
    }, autosaveDelayMs);
    return () => {
      clearTimeout(timer);
    };
  }, [autosave, dirty, conflict, saving, values, body, save]);

  // Accepting ends in a navigation of its own, and there is no draft behind it
  // to protect, so the guard belongs to the editor alone.
  const blocker = useBlocker({
    shouldBlockFn: () => !accepting && dirty,
    enableBeforeUnload: () => !accepting && dirty,
    withResolver: true,
  });

  const reloadTheirs = useCallback(() => {
    void (async () => {
      const fresh = await provider.getItem(id);
      // Taking their version is an explicit decision to drop the local edit,
      // draft included; keeping it would offer the discarded text back.
      clearDraft(storageKey);
      setOffered(null);
      loadedRev.current = loadKey(fresh.rev);
      setBase(fresh);
      setValues(
        accepting ? { ...valuesFromItem(fresh), status: defaultStatus } : valuesFromItem(fresh),
      );
      setBody(fresh.body);
      setDirty(false);
      setConflict(false);
      setDiagnostics([]);
      queryClient.setQueryData(detailKey(projectKey, fresh.id), fresh);
    })();
  }, [
    provider,
    id,
    projectKey,
    queryClient,
    storageKey,
    accepting,
    defaultStatus,
    loadKey,
  ]);

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

  const readOnly = !provider.capabilities.write;
  const shown = diagnostics.length > 0 ? diagnostics : localDiagnostics;

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-mono text-xs text-muted-foreground">{base.id}</p>
          <h1 className="page-title">{accepting ? 'Accept into the backlog' : `Edit ${base.title}`}</h1>
          {accepting ? (
            <p className="text-sm text-muted-foreground">
              Pick where it lands. Saving clears the triage state in the same write.
            </p>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {accepting ? null : (
            <div className="flex items-center gap-2">
              <Label htmlFor="autosave-toggle">Autosave</Label>
              <Switch
                id="autosave-toggle"
                aria-label="Autosave"
                checked={autosave}
                disabled={readOnly}
                onCheckedChange={setAutosave}
              />
            </div>
          )}
          <Button
            variant="ghost"
            onClick={() => {
              void (accepting
                ? navigate({ to: '/p/$project/inbox', params: { project: projectKey } })
                : navigate({
                    to: '/p/$project/items/$id',
                    params: { project: projectKey, id: base.id },
                  }));
            }}
          >
            Cancel
          </Button>
          <Button
            variant={accepting ? 'accent' : 'default'}
            disabled={saving || readOnly}
            onClick={() => {
              void save();
            }}
          >
            {accepting
              ? saving
                ? 'Accepting…'
                : 'Accept'
              : saving
                ? 'Saving…'
                : 'Save'}
          </Button>
        </div>
      </header>

      {!accepting && offered ? (
        <div
          role="alertdialog"
          aria-label="Recovered draft"
          className="space-y-2 rounded-md border border-accent bg-secondary p-4"
        >
          <p className="text-sm">
            Unsaved changes from {formatDraftTime(offered.savedAt)} were recovered from this
            browser. They were never written to the repository.
            {offered.rev === base.rev
              ? ''
              : ' The file has changed on disk since, so review them before saving.'}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              disabled={readOnly}
              onClick={() => {
                setValues(offered.values);
                setBody(offered.body);
                setDirty(true);
                setOffered(null);
              }}
            >
              Restore draft
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                clearDraft(storageKey);
                setOffered(null);
              }}
            >
              Discard draft
            </Button>
          </div>
        </div>
      ) : null}

      {accepting ? (
        readOnly ? (
          <p className="text-sm text-muted-foreground" role="status">
            This workspace is read-only.
          </p>
        ) : null
      ) : (
        <p className="text-sm text-muted-foreground" role="status">
          {readOnly
            ? 'This workspace is read-only.'
            : dirty
              ? 'Unsaved changes — kept in this browser until they are saved'
              : savedRev
                ? 'Saved'
                : 'No changes'}
        </p>
      )}

      {saveError ? (
        <p className="text-sm text-destructive" role="alert">
          {saveError}
        </p>
      ) : null}

      <DiagnosticList diagnostics={shown} title="Validation" />

      <FrontMatterForm
        type={base.type}
        values={values}
        schema={schema}
        projectKey={projectKey}
        diagnostics={shown}
        disabled={readOnly}
        onChange={(next) => {
          setValues(next);
          setDirty(true);
        }}
      />

      <section className="space-y-2" aria-label="Body">
        <h2 className="section-label">Body</h2>
        <MarkdownEditor
          label="Item body"
          value={body}
          readOnly={readOnly}
          references={references}
          onChange={(next) => {
            setBody(next);
            setDirty(true);
          }}
        />
      </section>

      {conflict ? (
        <ConflictDialog
          itemId={base.id}
          busy={saving}
          onReload={reloadTheirs}
          onOverwrite={() => {
            void save({ overwrite: true });
          }}
          onCancel={() => {
            setConflict(false);
          }}
        />
      ) : null}

      {blocker.status === 'blocked' ? (
        <div
          role="alertdialog"
          aria-label="Unsaved changes"
          className="fixed inset-x-0 bottom-0 z-40 flex flex-wrap items-center justify-between gap-3 border-t border-border bg-card p-4 shadow-overlay"
        >
          <p className="text-sm">Leave the editor? Your unsaved changes will be lost.</p>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={blocker.reset}>
              Stay
            </Button>
            <Button variant="destructive" onClick={blocker.proceed}>
              Leave
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
