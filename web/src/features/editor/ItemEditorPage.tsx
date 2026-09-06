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

/** Item editor: front matter form + CodeMirror body, rev-checked saves. */
export function ItemEditorPage() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const id = params.id ?? '';
  const provider = useProvider();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
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
  const [offered, setOffered] = useState(() => readDraft(storageKey));

  const item = itemQuery.data;
  const schema = useMemo(
    () => readProjectSchema(projectsQuery.data?.find((p) => p.key === projectKey)),
    [projectsQuery.data, projectKey],
  );
  const references = useMemo(
    () => (referencesQuery.data?.items ?? []).map((i) => ({ id: i.id, title: i.title })),
    [referencesQuery.data],
  );

  useEffect(() => {
    if (!item || dirty) return;
    if (loadedRev.current === item.rev) return;
    loadedRev.current = item.rev;
    setBase(item);
    setValues(valuesFromItem(item));
    setBody(item.body);
  }, [item, dirty]);

  // Every keystroke of an unsaved edit reaches storage, so a reload, a crash or
  // a closed tab finds the draft again. It is per-viewer convenience state and
  // never a source of truth: the file on disk is (docs/05-web-app.md §8.3).
  useEffect(() => {
    if (!dirty || !base || !values) return;
    writeDraft(storageKey, { rev: base.rev, values, body });
  }, [dirty, base, values, body, storageKey]);

  const localDiagnostics = useMemo(
    () => (values && base ? validateValues(values, schema, base.type) : []),
    [values, base, schema],
  );

  const save = useCallback(
    async (options: { overwrite?: boolean } = {}) => {
      if (!base || !values || saving) return;

      const local = validateValues(values, schema, base.type);
      let all = local;
      try {
        const remote = await provider.validateItem({ text: serializeItem(base, values, body) });
        all = [...local, ...remote];
      } catch {
        // A provider without text validation is fine; local rules still apply.
      }
      setDiagnostics(all);
      if (hasErrors(all)) return;

      const patch = buildPatch(base, values, body);
      if (isEmptyPatch(patch)) {
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
        const saved = await provider.updateItem(base.id, patch, rev);
        // The edit is on disk; the draft has nothing left to protect.
        clearDraft(storageKey);
        setOffered(null);
        loadedRev.current = saved.rev;
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
    [base, values, body, saving, schema, provider, queryClient, projectKey, storageKey],
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

  const blocker = useBlocker({
    shouldBlockFn: () => dirty,
    enableBeforeUnload: () => dirty,
    withResolver: true,
  });

  const reloadTheirs = useCallback(() => {
    void (async () => {
      const fresh = await provider.getItem(id);
      // Taking their version is an explicit decision to drop the local edit,
      // draft included; keeping it would offer the discarded text back.
      clearDraft(storageKey);
      setOffered(null);
      loadedRev.current = fresh.rev;
      setBase(fresh);
      setValues(valuesFromItem(fresh));
      setBody(fresh.body);
      setDirty(false);
      setConflict(false);
      setDiagnostics([]);
      queryClient.setQueryData(detailKey(projectKey, fresh.id), fresh);
    })();
  }, [provider, id, projectKey, queryClient, storageKey]);

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
          <h1 className="page-title">Edit {base.title}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-3">
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
          <Button
            variant="ghost"
            onClick={() => {
              void navigate({
                to: '/p/$project/items/$id',
                params: { project: projectKey, id: base.id },
              });
            }}
          >
            Cancel
          </Button>
          <Button
            disabled={saving || readOnly}
            onClick={() => {
              void save();
            }}
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </header>

      {offered ? (
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

      <p className="text-sm text-muted-foreground" role="status">
        {readOnly
          ? 'This workspace is read-only.'
          : dirty
            ? 'Unsaved changes — kept in this browser until they are saved'
            : savedRev
              ? 'Saved'
              : 'No changes'}
      </p>

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
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
          Body
        </h2>
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
