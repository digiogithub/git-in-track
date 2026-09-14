/**
 * Settings — YouTrack connection (story GIT-US-0055, epic GIT-EP-0011).
 *
 * Connecting a project is three facts — an instance, a credential and a remote
 * project — and the whole point of this card is that the user learns whether
 * they are right *before* anything is written: "Test connection" probes an
 * unsaved URL and token and names the YouTrack user it resolved.
 *
 * The credential is the part that decides the shape of this screen. The API is
 * write-only about it: `GET` never returns a token, only `hasToken` and
 * `tokenSource`. So the input is never pre-filled — not even with a masked
 * placeholder value, which a save would write back as a literal string — and
 * the card says instead that a token is stored and where it came from. A token
 * that arrived in the environment or on the command line belongs to whoever
 * started the companion: this screen reports it and refuses to pretend it can
 * clear it.
 *
 * Everything the instance says about itself comes back as a problem code, and
 * each one is a different thing for the user to do: a rejected token, a token
 * without the permission, a base URL missing its context path, an unreachable
 * host. They are rendered apart, never as one "failed" line. The companion
 * answers `502` for all of them on purpose, so a browser never mistakes
 * YouTrack refusing a token for its own session expiring.
 *
 * Every one of those calls is *per project*: the companion may serve several
 * repositories at once, and `/api/v1/youtrack/*` refuses to guess which one a
 * connection belongs to unless exactly one is mounted. So the card picks a
 * git-in-track project first and scopes every read and write to it; the picker
 * is shown only where there is a choice to make.
 */

import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { TriangleAlert } from 'lucide-react';
import { useCallback, useEffect, useId, useState } from 'react';

import type {
  YouTrackKbSync,
  YouTrackKbSyncDirection,
  YouTrackProject,
  YouTrackPushComments,
  YouTrackScope,
  YouTrackSettings,
  YouTrackSettingsPatch,
  YouTrackTestResult,
  YouTrackTokenSource,
} from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Combobox } from '@/components/ui/combobox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select } from '@/components/ui/select';
import { useToast } from '@/components/ui/toast';
import { youtrackMessage } from '@/features/settings/youtrack-messages';
import { YouTrackFieldMap } from '@/features/settings/YouTrackFieldMap';

/** How the stored credential is described, by where it came from. */
const TOKEN_SOURCE_LABELS: Record<YouTrackTokenSource, string> = {
  '': 'No token is stored.',
  none: 'No token is stored.',
  file: 'A token is stored in the companion’s configuration file.',
  config: 'A token is stored in the companion’s configuration file.',
  env: 'A token comes from the environment of the running companion.',
  flag: 'A token was given to the companion on its command line.',
};

/** Sources this screen may not clear: they belong to whoever started the process. */
function isExternalToken(source: YouTrackTokenSource): boolean {
  return source === 'env' || source === 'flag';
}

/** The editable half of the card, so a reload is a state reset and nothing else. */
type Draft = {
  url: string;
  project: string;
  /** The chosen project's entity id; '' when the short name was typed by hand. */
  projectId: string;
  token: string;
  pushComments: YouTrackPushComments;
  kbSync: YouTrackKbSync;
  kbSyncDirection: YouTrackKbSyncDirection;
};

function draftOf(settings: YouTrackSettings): Draft {
  return {
    url: settings.url,
    project: settings.project,
    projectId: settings.projectId,
    // Never seeded: the API does not return the token and a placeholder value
    // here would be saved back as if the user had typed it.
    token: '',
    pushComments: settings.pushComments === '' ? 'manual' : settings.pushComments,
    kbSync: settings.kbSync === '' ? 'manual' : settings.kbSync,
    kbSyncDirection: settings.kbSyncDirection === '' ? 'push' : settings.kbSyncDirection,
  };
}

/**
 * Gate: the card exists only where the runtime can talk to YouTrack at all.
 *
 * It is `youtrackSupported` and not `youtrack`, deliberately — a card that
 * appeared only once a project was connected could never connect the first one.
 */
export function YouTrackCard() {
  const provider = useOptionalProvider();
  if (!provider?.capabilities.youtrackSupported) return null;
  return <YouTrackConnection />;
}

function YouTrackConnection() {
  const provider = useOptionalProvider();
  const { toast } = useToast();
  const [settings, setSettings] = useState<YouTrackSettings | null>(null);
  const [draft, setDraft] = useState<Draft>({
    url: '',
    project: '',
    projectId: '',
    token: '',
    pushComments: 'manual',
    kbSync: 'manual',
    kbSyncDirection: 'push',
  });
  const [error, setError] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);
  const [tested, setTested] = useState<YouTrackTestResult | null>(null);
  const [testError, setTestError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [search, setSearch] = useState('');
  const [pickerOpen, setPickerOpen] = useState(false);
  // '' means "whatever the list settles on": the picker only overrides the
  // default once someone chooses, so a workspace with one project never has to.
  const [chosenKey, setChosenKey] = useState('');
  const localProjectId = useId();
  const urlId = useId();
  const tokenId = useId();
  const projectId = useId();
  const pushId = useId();
  const kbSyncId = useId();
  const kbDirectionId = useId();

  // The git-in-track projects this companion serves. Every YouTrack call is
  // scoped to one of them, and the companion refuses to guess when it holds
  // more than one — so the list has to arrive before the first read goes out.
  const localProjects = useQuery({
    queryKey: ['projects'],
    queryFn: () => provider?.listProjects() ?? Promise.resolve([]),
    enabled: provider !== null,
  });
  const projectKeys = (localProjects.data ?? []).map((project) => project.key);
  const projectKey =
    chosenKey !== '' && projectKeys.includes(chosenKey) ? chosenKey : (projectKeys[0] ?? '');
  const scope: YouTrackScope = projectKey === '' ? {} : { projectKey };
  const projectsSettled = !localProjects.isPending;

  const load = useCallback(async () => {
    if (!provider || !projectsSettled) return;
    const loaded = await provider.getYouTrackSettings(projectKey === '' ? {} : { projectKey });
    setSettings(loaded);
    setDraft(draftOf(loaded));
    setTested(null);
    setTestError(null);
  }, [provider, projectsSettled, projectKey]);

  useEffect(() => {
    setError(null);
    void load().catch((cause: unknown) => {
      setError(youtrackMessage(cause));
    });
  }, [load]);

  // The autosuggest asks the instance, so it can only run once a credential
  // resolves. A token the user has just typed counts: choosing the remote
  // project is part of connecting, so the list is read with the unsaved URL and
  // token rather than making somebody save a connection to find out what to
  // connect to. Only when neither resolves is the field a plain text input.
  const typedToken = draft.token.trim();
  const probe = typedToken === '' ? undefined : { url: draft.url.trim(), token: typedToken };
  const canQuery = draft.url.trim() !== '' && (settings?.hasToken === true || probe !== undefined);
  const projects = useQuery({
    // The token is part of the key only as "there is one": a credential must
    // not end up in a cache key, and the URL already separates instances.
    queryKey: ['youtrack', 'projects', projectKey, draft.url.trim(), probe !== undefined, search],
    queryFn: () => provider?.listYouTrackProjects(search, scope, probe) ?? Promise.resolve([]),
    enabled: pickerOpen && canQuery,
    // Every keystroke settles into a new search, and therefore a new query key.
    // Without this the list empties while the next answer is in flight, so a
    // suggestion the reader is already reaching for unmounts under the pointer
    // and the click lands on nothing. Keeping the previous answer visible until
    // the new one arrives is what makes the list clickable while typing.
    placeholderData: keepPreviousData,
  });

  if (!provider) return null;

  const apply = async (patch: YouTrackSettingsPatch, title: string) => {
    setSaving(true);
    setError(null);
    try {
      const next = await provider.updateYouTrackSettings(patch, scope);
      setSettings(next);
      setDraft(draftOf(next));
      toast({
        title,
        description: next.persisted
          ? `Written to ${next.projectPath === '' ? 'the project configuration' : next.projectPath}.`
          : 'Applied to the running companion only — it was not written to disk, so a restart loses it.',
      });
    } catch (cause: unknown) {
      setError(youtrackMessage(cause));
      toast({ title: 'The change was not saved', variant: 'destructive' });
    } finally {
      setSaving(false);
    }
  };

  const save = () => {
    const patch: YouTrackSettingsPatch = {
      url: draft.url.trim(),
      project: draft.project.trim(),
      // Sent even when empty: a short name typed by hand has no id, and the
      // companion resolves one when a job needs it rather than writing a
      // guess here.
      projectId: draft.projectId.trim(),
      pushComments: draft.pushComments,
      kbSync: draft.kbSync,
      kbSyncDirection: draft.kbSyncDirection,
      // Only a token the user typed is sent. An untouched field means "leave
      // whatever is stored alone", never "clear it".
      ...(draft.token === '' ? {} : { token: draft.token }),
    };
    void apply(patch, 'YouTrack connection saved');
  };

  const forgetToken = () => {
    void apply({ token: '' }, 'The stored token was forgotten');
  };

  const test = () => {
    setTesting(true);
    setTested(null);
    setTestError(null);
    const probe: { url?: string; token?: string } = {
      ...(draft.url.trim() === '' ? {} : { url: draft.url.trim() }),
      ...(draft.token === '' ? {} : { token: draft.token }),
    };
    void provider
      .testYouTrackConnection(probe, scope)
      .then(setTested)
      .catch((cause: unknown) => {
        setTestError(youtrackMessage(cause));
      })
      .finally(() => {
        setTesting(false);
      });
  };

  const tokenSource = settings?.tokenSource ?? '';
  const external = isExternalToken(tokenSource);

  // Nothing on this card can be addressed to a project before the companion has
  // said which projects it serves and answered for one of them. A form rendered
  // ahead of that answer is a form whose values are reset under whoever started
  // typing into it, so the card waits instead.
  if (settings === null && error === null) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>YouTrack</CardTitle>
          <CardDescription>
            Connect this project to a YouTrack project to import issues, push comments and
            synchronize knowledge-base pages.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">Loading…</CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between space-y-0">
        <div className="space-y-1">
          <CardTitle>YouTrack</CardTitle>
          <CardDescription>
            Connect this project to a YouTrack project to import issues, push comments and
            synchronize knowledge-base pages.
          </CardDescription>
        </div>
        <Badge variant={settings?.configured === true ? 'accent' : 'outline'}>
          {settings?.configured === true ? 'Connected' : 'Not connected'}
        </Badge>
      </CardHeader>

      <CardContent className="space-y-5 text-sm">
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            save();
          }}
        >
          {projectKeys.length > 1 ? (
            <div className="space-y-1">
              <Label htmlFor={localProjectId}>git-in-track project</Label>
              <Select
                id={localProjectId}
                value={projectKey}
                onChange={(event) => {
                  setChosenKey(event.target.value);
                }}
              >
                {(localProjects.data ?? []).map((project) => (
                  <option key={project.key} value={project.key}>
                    {project.key} — {project.name}
                  </option>
                ))}
              </Select>
              <p className="text-muted-foreground">
                This companion serves several projects, and a YouTrack connection belongs to one of
                them. Everything below — the instance, the token and the field map — is saved for
                the project chosen here.
              </p>
            </div>
          ) : null}

          <div className="space-y-1">
            <Label htmlFor={urlId}>Instance URL</Label>
            <Input
              id={urlId}
              value={draft.url}
              spellCheck={false}
              placeholder="https://yourteam.youtrack.cloud"
              onChange={(event) => {
                setDraft((current) => ({ ...current, url: event.target.value }));
              }}
            />
            <p className="text-muted-foreground">
              Include the context path when the instance has one, as in{' '}
              <code>https://yt.example.com/youtrack</code>.
            </p>
          </div>

          <div className="space-y-1">
            <Label htmlFor={tokenId}>Permanent token</Label>
            <Input
              id={tokenId}
              type="password"
              value={draft.token}
              autoComplete="off"
              spellCheck={false}
              disabled={external}
              // A placeholder, never a value: the API never returns the token,
              // and anything shown here could be saved back as a literal.
              placeholder={
                settings?.hasToken === true ? 'Stored — type a new one to replace it' : 'perm:…'
              }
              onChange={(event) => {
                setDraft((current) => ({ ...current, token: event.target.value }));
              }}
            />
            <p className="text-muted-foreground" data-testid="youtrack-token-state">
              {TOKEN_SOURCE_LABELS[tokenSource]}{' '}
              {external
                ? 'It was given to the companion when it started, so it cannot be changed or cleared from here.'
                : 'The token is stored by the companion and never sent back to this page.'}
            </p>
            {settings?.hasToken === true && !external ? (
              <Button type="button" variant="ghost" size="sm" onClick={forgetToken}>
                Forget the stored token
              </Button>
            ) : null}
          </div>

          <div className="space-y-1">
            <Label htmlFor={projectId}>YouTrack project</Label>
            <Combobox<YouTrackProject>
              id={projectId}
              label="YouTrack project"
              value={draft.project}
              onValueChange={(value) => {
                // Typed by hand: whatever id was chosen belonged to another
                // project, so it goes with it.
                setDraft((current) => ({ ...current, project: value, projectId: '' }));
              }}
              onSearchChange={setSearch}
              onOpenChange={setPickerOpen}
              options={canQuery ? (projects.data ?? []) : []}
              getOptionKey={(option) => option.id}
              isOptionSelected={(option) => option.shortName === draft.project}
              renderOption={(option) => (
                <span className="flex items-center gap-2">
                  <span className="font-mono text-xs text-muted-foreground">
                    {option.shortName}
                  </span>
                  <span>{option.name}</span>
                  {option.archived ? (
                    <Badge variant="outline" size="sm">
                      archived
                    </Badge>
                  ) : null}
                </span>
              )}
              onSelect={(option) => {
                // The id is the half that matters to YouTrack: every write
                // addresses a project by it, and it cannot be typed.
                setDraft((current) => ({
                  ...current,
                  project: option.shortName,
                  projectId: option.id,
                }));
              }}
              placeholder="ACME"
              loading={projects.isFetching}
              {...(canQuery ? { emptyLabel: 'No project matches.' } : {})}
            />
            <p className="text-muted-foreground">
              {canQuery
                ? 'The short name, the “ACME” of ACME-42. Type to search the instance.'
                : 'The short name, the “ACME” of ACME-42. Fill in the URL and a token to search the instance for it.'}{' '}
              {draft.projectId === ''
                ? 'Choosing one from the list also records the id YouTrack needs to create articles.'
                : `Linked to ${draft.projectId}, the id YouTrack creates articles with.`}
            </p>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1">
              <Label htmlFor={pushId}>Push comments</Label>
              <Select
                id={pushId}
                value={draft.pushComments}
                onChange={(event) => {
                  setDraft((current) => ({
                    ...current,
                    pushComments: event.target.value as YouTrackPushComments,
                  }));
                }}
              >
                <option value="manual">Only when asked</option>
                <option value="auto">On every comment</option>
              </Select>
              <p className="text-muted-foreground">
                {draft.pushComments === 'auto'
                  ? 'Every comment written here from now on — by a person, by the CLI or by an agent — is queued for the linked issue, with no further confirmation. Comments already written are not sent retroactively.'
                  : 'Nothing leaves this repository until someone uses “Send to YouTrack” on a comment.'}
              </p>
            </div>
            <div className="space-y-1">
              <Label htmlFor={kbSyncId}>Knowledge base sync</Label>
              <Select
                id={kbSyncId}
                value={draft.kbSync}
                onChange={(event) => {
                  setDraft((current) => ({
                    ...current,
                    kbSync: event.target.value as YouTrackKbSync,
                  }));
                }}
              >
                <option value="manual">Only when asked</option>
                <option value="on_write">On every write</option>
              </Select>
              <p className="text-muted-foreground">
                {draft.kbSync === 'on_write'
                  ? 'Saving a knowledge-base page enqueues a publish of that page, every time. A busy afternoon of edits is a busy afternoon of background jobs.'
                  : 'Pages are published and pulled only from the knowledge-base toolbar.'}
              </p>
            </div>
            <div className="space-y-1">
              <Label htmlFor={kbDirectionId}>Sync direction</Label>
              <Select
                id={kbDirectionId}
                value={draft.kbSyncDirection}
                onChange={(event) => {
                  setDraft((current) => ({
                    ...current,
                    kbSyncDirection: event.target.value as YouTrackKbSyncDirection,
                  }));
                }}
              >
                <option value="push">Push to YouTrack</option>
                <option value="pull">Pull from YouTrack</option>
                <option value="both">Both ways</option>
              </Select>
              <p className="text-muted-foreground">
                Which way pages travel. When both sides changed since the last sync, neither wins:
                the page is left untouched and the incoming text is written beside it as{' '}
                <code>&lt;page&gt;.conflict.md</code>.
              </p>
            </div>
            <div className="space-y-1 sm:col-span-2">
              <p className="text-muted-foreground">
                The <code>## Feedback</code> block of a page is never published: reader notes stay
                in this repository whichever direction is chosen.
              </p>
            </div>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={saving}>
              {saving ? 'Saving…' : 'Save connection'}
            </Button>
            <Button type="button" variant="outline" disabled={testing} onClick={test}>
              {testing ? 'Testing…' : 'Test connection'}
            </Button>
          </div>
        </form>

        {tested === null ? null : (
          <p role="status" className="rounded-md border border-border bg-secondary/50 p-3">
            Connected to <code>{tested.baseUrl}</code> as{' '}
            <strong className="font-medium">
              {tested.fullName === '' ? tested.login : tested.fullName}
            </strong>{' '}
            ({tested.login}
            {tested.email === '' ? '' : `, ${tested.email}`})
            {tested.project === '' ? '.' : `, with access to ${tested.project}.`}
          </p>
        )}

        {testError === null ? null : (
          <p role="alert" className="flex gap-2 text-destructive">
            <TriangleAlert aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
            <span>{testError}</span>
          </p>
        )}

        {error === null ? null : (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}

        {settings === null ? null : (
          <YouTrackFieldMap
            settings={settings}
            project={draft.project.trim()}
            saving={saving}
            onSave={(fieldMap) => {
              void apply({ fieldMap }, 'Field map saved');
            }}
          />
        )}

        {settings?.projectPath === '' ? null : (
          <p className="text-muted-foreground">
            The connection lives in <code>{settings?.projectPath}</code> and is committed with the
            project; the token is not.
          </p>
        )}
      </CardContent>
    </Card>
  );
}
