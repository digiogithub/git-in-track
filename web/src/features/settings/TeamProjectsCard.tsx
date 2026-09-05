/**
 * Settings — Team projects (story GIT-US-0037, docs/04-team-repository.md §3.9).
 *
 * The `projects:` list of `team.yaml` is the routing table of the whole product:
 * it decides which project a board may show, whether a card renders live from a
 * clone or read-only from a committed index snapshot, and where a remote blob
 * link points. Until this card it could only be hand-edited.
 *
 * The team it acts on is the active team of GIT-US-0036 — the same choice the
 * boards, sprints and retros follow — so the card carries that selector and
 * builds no second mechanism.
 *
 * The link between a repository the user has registered and an entry here is
 * **the project key alone**, never a path and never a remote URL (docs/04 §7.1).
 * That is what the candidate list makes concrete: it offers the projects this
 * machine has indexed, pre-fills what is already known about each, and the entry
 * it writes is cloned from then on because some open repository serves that key.
 */

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CloudOff, FolderGit2, Plus, Trash2, TriangleAlert } from 'lucide-react';
import { useId, useMemo, useState } from 'react';

import type { ProjectSummary, TeamProjectDraft, TeamProjectSummary } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { teamKeys, useActiveTeam } from '@/features/workspace/active-team';
import { TeamSelector } from '@/features/workspace/TeamSelector';

/** The grammar a project key must match (docs/03 §3.1). */
const PROJECT_KEY = /^[A-Z][A-Z0-9]{1,9}$/;

/** The form the "add a project" half is filled from. */
type Draft = {
  key: string;
  name: string;
  repo: string;
  defaultBranch: string;
  docsPath: string;
};

const EMPTY_DRAFT: Draft = { key: '', name: '', repo: '', defaultBranch: '', docsPath: '' };

/** One locally indexed project, with what is known about its clone. */
type Candidate = {
  project: ProjectSummary;
  repo: string;
  branch: string;
};

/**
 * The card is mounted only once a data provider exists: the active team is read
 * through TanStack Query against that provider, and Settings renders in tests
 * and in the very first frame of a session without one.
 */
export function TeamProjectsCard() {
  const provider = useOptionalProvider();
  if (!provider) return null;
  return <TeamProjectsGate />;
}

function TeamProjectsGate() {
  const { team, isPending } = useActiveTeam();

  if (isPending) return null;

  if (team === undefined) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Team projects</CardTitle>
          <CardDescription>
            No team repository is open, so there is no project list to manage. Create or mount one
            from the workspace page first.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  // Remounting on a team switch resets the draft: a form pre-filled for one
  // team is misleading once another one is showing.
  return <TeamProjects key={team.key} />;
}

function TeamProjects() {
  const provider = useOptionalProvider();
  const queryClient = useQueryClient();
  const { team, teamKey } = useActiveTeam();
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT);
  const [error, setError] = useState<string | null>(null);
  /** The project a refused removal is waiting for confirmation on. */
  const [breakable, setBreakable] = useState<string | null>(null);
  const selectId = useId();

  const projects = useQuery({
    queryKey: ['projects'],
    queryFn: () => provider?.listProjects() ?? Promise.resolve([]),
    enabled: provider !== null,
  });
  // The remote URL and the branch of a clone come from the sync status, which
  // is the only place the product knows them. A runtime that cannot report them
  // simply offers no pre-fill, and the user types the URL.
  const sync = useQuery({
    queryKey: ['sync', 'status'],
    queryFn: async () => {
      try {
        return (await provider?.getSyncStatus()) ?? [];
      } catch {
        return [];
      }
    },
    enabled: provider !== null,
  });

  const declared = useMemo(() => new Set((team?.projects ?? []).map((p) => p.key)), [team]);
  const candidates: Candidate[] = useMemo(() => {
    const remotes = new Map<string, { repo: string; branch: string }>();
    for (const status of sync.data ?? []) {
      remotes.set(status.repo, {
        repo: status.status?.remoteUrl ?? '',
        branch: status.status?.branch ?? '',
      });
    }
    return (projects.data ?? [])
      .filter((project) => !declared.has(project.key))
      .map((project) => {
        const remote = project.vaultId === undefined ? undefined : remotes.get(project.vaultId);
        return {
          project,
          repo: remote?.repo ?? '',
          branch: remote?.branch ?? '',
        };
      });
  }, [projects.data, sync.data, declared]);

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: teamKeys.all() });
    await queryClient.invalidateQueries({ queryKey: ['projects'] });
  };

  const add = useMutation({
    mutationFn: (entry: TeamProjectDraft) => {
      if (!provider) throw new ProviderError('read_only', 'No data provider is available.');
      return provider.addTeamProject(entry, teamKey);
    },
    onSuccess: async () => {
      setDraft(EMPTY_DRAFT);
      setError(null);
      await invalidate();
    },
    onError: (cause: unknown) => {
      setError(messageOf(cause));
    },
  });

  const remove = useMutation({
    mutationFn: ({ key, force }: { key: string; force: boolean }) => {
      if (!provider) throw new ProviderError('read_only', 'No data provider is available.');
      return provider.removeTeamProject(key, { force }, teamKey);
    },
    onSuccess: async () => {
      setBreakable(null);
      setError(null);
      await invalidate();
    },
    onError: (cause: unknown, variables) => {
      if (cause instanceof ProviderError && cause.code === 'team_project_referenced') {
        setBreakable(variables.key);
      }
      setError(messageOf(cause));
    },
  });

  if (!team) return null;

  const busy = add.isPending || remove.isPending;
  const invalidKey = keyError(draft.key, declared);
  const incomplete =
    invalidKey !== null || draft.repo.trim() === '' || draft.docsPath.trim() === '';

  /** Fills the form from a locally indexed project. */
  const prefill = (key: string) => {
    const candidate = candidates.find((c) => c.project.key === key);
    if (!candidate) {
      setDraft(EMPTY_DRAFT);
      return;
    }
    setDraft({
      key: candidate.project.key,
      name: candidate.project.name,
      repo: candidate.repo,
      defaultBranch: candidate.branch,
      docsPath: candidate.project.docsPath,
    });
  };

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
        <div className="space-y-1.5">
          <CardTitle>Team projects</CardTitle>
          <CardDescription>
            The repositories team <strong>{team.name}</strong> owns. A board can only show cards
            from a project declared here, and a project this machine has not cloned renders from its
            committed index snapshot.
          </CardDescription>
        </div>
        <TeamSelector />
      </CardHeader>

      <CardContent className="space-y-6 text-sm">
        {error === null ? null : (
          <p role="alert" className="rounded-md border border-destructive/40 p-3 text-destructive">
            {error}
          </p>
        )}

        <ul className="space-y-2" aria-label="Declared projects">
          {team.projects.length === 0 ? (
            <li className="rounded-md border border-dashed border-border p-3 text-muted-foreground">
              This team declares no project yet, so its boards have nothing to pull cards from.
            </li>
          ) : null}
          {team.projects.map((entry) => (
            <ProjectRow
              key={entry.key}
              entry={entry}
              busy={busy}
              confirming={breakable === entry.key}
              onRemove={(force) => {
                remove.mutate({ key: entry.key, force });
              }}
              onCancel={() => {
                setBreakable(null);
                setError(null);
              }}
            />
          ))}
        </ul>

        <form
          className="space-y-3 border-t border-border pt-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (incomplete) return;
            add.mutate({
              key: draft.key.trim(),
              ...(draft.name.trim() === '' ? {} : { name: draft.name.trim() }),
              repo: draft.repo.trim(),
              ...(draft.defaultBranch.trim() === ''
                ? {}
                : { defaultBranch: draft.defaultBranch.trim() }),
              docsPath: draft.docsPath.trim(),
            });
          }}
        >
          <h3 className="font-medium">Add a project</h3>

          <label className="block space-y-1.5 font-medium" htmlFor={selectId}>
            Registered repository
            <Select
              id={selectId}
              value={candidates.some((c) => c.project.key === draft.key) ? draft.key : ''}
              onChange={(event) => {
                prefill(event.target.value);
              }}
            >
              <option value="">Type the details myself</option>
              {candidates.map((candidate) => (
                <option key={candidate.project.key} value={candidate.project.key}>
                  {candidate.project.name} ({candidate.project.key})
                </option>
              ))}
            </Select>
            <span className="block text-xs font-normal text-muted-foreground">
              {candidates.length === 0
                ? 'Every project indexed on this machine is already declared. A project nobody here has cloned is declared by typing its details.'
                : 'Picking one fills in its key, name, docs folder and remote. A project this machine has not cloned is declared by hand.'}
            </span>
          </label>

          <div className="grid gap-3 sm:grid-cols-2">
            <Field
              label="Project key"
              value={draft.key}
              placeholder="ACME"
              onChange={(value) => {
                setDraft({ ...draft, key: value.toUpperCase() });
              }}
              hint="Must equal the key: of that repository's own project.yaml."
            />
            <Field
              label="Name"
              value={draft.name}
              placeholder="ACME Platform"
              onChange={(value) => {
                setDraft({ ...draft, name: value });
              }}
              hint="Defaults to the key."
            />
            <Field
              label="Repository URL"
              value={draft.repo}
              placeholder="https://github.com/acme/platform.git"
              onChange={(value) => {
                setDraft({ ...draft, repo: value });
              }}
              hint="The canonical remote; https is preferred."
            />
            <Field
              label="Default branch"
              value={draft.defaultBranch}
              placeholder="main"
              onChange={(value) => {
                setDraft({ ...draft, defaultBranch: value });
              }}
              hint="Snapshot links and blob URLs are built against it."
            />
            <Field
              label="Docs folder"
              value={draft.docsPath}
              placeholder="docs"
              onChange={(value) => {
                setDraft({ ...draft, docsPath: value });
              }}
              hint="The folder holding .pmngr/ inside that repository."
            />
          </div>

          {invalidKey === null || draft.key === '' ? null : (
            <p role="alert" className="text-destructive">
              {invalidKey}
            </p>
          )}

          <Button type="submit" disabled={incomplete || busy}>
            <Plus aria-hidden="true" className="mr-1 h-4 w-4" />
            {add.isPending ? 'Adding…' : 'Add project'}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

/** One declared project: how it renders today, and the way to disconnect it. */
function ProjectRow({
  entry,
  busy,
  confirming,
  onRemove,
  onCancel,
}: {
  entry: TeamProjectSummary;
  busy: boolean;
  confirming: boolean;
  onRemove: (force: boolean) => void;
  onCancel: () => void;
}) {
  // W-TEAM-KEY-MISMATCH is the failure mode this card produces: the entry is
  // linked to a clone by key, so a clone declaring another key is connected to
  // nothing at all.
  const mismatch = (entry.diagnostics ?? []).filter((d) => d.code === 'W-TEAM-KEY-MISMATCH');

  return (
    <li className="rounded-md border border-border p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0 space-y-1">
          <p className="flex flex-wrap items-center gap-2 font-medium">
            {entry.name}
            <Badge variant="outline">{entry.key}</Badge>
            {entry.cloned ? (
              <Badge variant="accent">
                <FolderGit2 aria-hidden="true" className="mr-1 h-3 w-3" />
                Cloned
              </Badge>
            ) : (
              <Badge variant="outline" title="Cards render from the committed index snapshot">
                <CloudOff aria-hidden="true" className="mr-1 h-3 w-3" />
                {entry.snapshot.present ? 'From snapshot' : 'Not cloned, no snapshot'}
              </Badge>
            )}
          </p>
          <p className="break-all text-xs text-muted-foreground">
            <code>{entry.repo}</code> · {entry.defaultBranch ?? 'main'} · docs:{' '}
            <code>{entry.docsPath}</code>
          </p>
        </div>

        {confirming ? (
          <span className="flex items-center gap-2">
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={busy}
              onClick={() => {
                onRemove(true);
              }}
            >
              Remove anyway
            </Button>
            <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onCancel}>
              Keep it
            </Button>
          </span>
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            aria-label={`Remove ${entry.key}`}
            onClick={() => {
              onRemove(false);
            }}
          >
            <Trash2 aria-hidden="true" className="mr-1 h-4 w-4" />
            Remove
          </Button>
        )}
      </div>

      {mismatch.map((diagnostic) => (
        <p
          key={diagnostic.message}
          role="alert"
          className="mt-2 flex items-start gap-2 text-xs text-destructive"
        >
          <TriangleAlert aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0" />
          <span>
            <code>{diagnostic.code}</code> {diagnostic.message}
          </span>
        </p>
      ))}
    </li>
  );
}

/** One labelled text input of the draft form. */
function Field({
  label,
  value,
  placeholder,
  hint,
  onChange,
}: {
  label: string;
  value: string;
  placeholder: string;
  hint: string;
  onChange: (value: string) => void;
}) {
  const id = useId();
  return (
    <label className="block space-y-1.5 font-medium" htmlFor={id}>
      {label}
      <Input
        id={id}
        value={value}
        placeholder={placeholder}
        onChange={(event) => {
          onChange(event.target.value);
        }}
      />
      <span className="block text-xs font-normal text-muted-foreground">{hint}</span>
    </label>
  );
}

/** Says why a key is refused, or null when it is fine. */
function keyError(key: string, declared: Set<string>): string | null {
  const trimmed = key.trim();
  if (trimmed === '') return 'A project key is required.';
  if (!PROJECT_KEY.test(trimmed)) {
    return 'A key is 2 to 10 characters: an uppercase letter, then uppercase letters or digits.';
  }
  if (declared.has(trimmed)) return `This team already declares ${trimmed}.`;
  return null;
}

/** The message a failure is shown with. */
function messageOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}
