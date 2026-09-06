import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { CheckCircle2, FolderGit2, Info, Terminal, Users } from 'lucide-react';
import { useState } from 'react';

import type { RepoKind } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { useAppStore } from '@/app/store';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import {
  detectDocsFolders,
  detectTeam,
  getVault,
  normalizeDocsFolder,
  SUPPORT_MATRIX,
  type DocsFolderCandidate,
} from '@/fs';

import { CreateProjectForm, type CreateProjectValues } from './CreateProjectForm';
import { CreateTeamForm, type CreateTeamValues } from './CreateTeamForm';
import { FolderPickers } from './FolderPickers';

const CUSTOM_CHOICE = '__custom__';

/**
 * Add repository wizard (docs/05-web-app.md §3.1): pick a folder, detect the
 * documentation folders that contain `.pmngr/project.yaml`, confirm, mount.
 */
export function AddRepositoryPage() {
  const provider = useProvider();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const pendingVaultId = useAppStore((state) => state.pendingVaultId);
  const pendingVaultName = useAppStore((state) => state.pendingVaultName);
  const setPendingVault = useAppStore((state) => state.setPendingVault);

  const [choice, setChoice] = useState<string | null>(null);
  const [customFolder, setCustomFolder] = useState('docs');
  const [error, setError] = useState<string | null>(null);
  /** The role the user picked; null until they do, when detection decides. */
  const [role, setRole] = useState<RepoKind | null>(null);

  const detection = useQuery({
    queryKey: ['vault-detection', pendingVaultId],
    enabled: pendingVaultId !== null,
    queryFn: async () => {
      const vault = pendingVaultId ? getVault(pendingVaultId) : undefined;
      if (!vault) throw new Error('That folder is no longer available. Choose it again.');
      const files = await vault.readTextFiles();
      return {
        candidates: detectDocsFolders(files),
        team: detectTeam(files),
        fileCount: files.length,
        writable: vault.capabilities.write,
        name: vault.name,
      };
    },
  });

  const finish = async (): Promise<void> => {
    setPendingVault(null);
    await queryClient.invalidateQueries({ queryKey: ['repos'] });
    await queryClient.invalidateQueries({ queryKey: ['projects'] });
    await navigate({ to: '/' });
  };

  const mount = useMutation({
    mutationFn: async (docsFolder: string) => {
      if (!pendingVaultId) throw new Error('No folder is selected');
      return provider.mountRepo({
        kind: 'project',
        location: pendingVaultId,
        docsFolder,
        docsFolders: [docsFolder],
      });
    },
    onSuccess: finish,
    onError: (cause: Error) => {
      setError(cause.message);
    },
  });

  /**
   * A repository with no backlog is mounted first and then written into: the
   * core has to hold the folder before it can scaffold a project in it.
   */
  const create = useMutation({
    mutationFn: async (values: CreateProjectValues) => {
      if (!pendingVaultId) throw new Error('No folder is selected');
      await provider.mountRepo({
        kind: 'project',
        location: pendingVaultId,
        docsFolder: values.docsFolder,
        docsFolders: [values.docsFolder],
      });
      return provider.createProject({
        repoId: pendingVaultId,
        docsFolder: values.docsFolder,
        key: values.key,
        ...(values.name === '' ? {} : { name: values.name }),
      });
    },
    onSuccess: finish,
    onError: (cause: Error) => {
      setError(cause.message);
    },
  });

  /**
   * Mounting a folder that already holds a `team.yaml`. The role is what makes
   * the workspace read boards, sprints and retros out of it, so it is chosen
   * deliberately rather than assumed (story GIT-US-0035).
   */
  const mountTeam = useMutation({
    mutationFn: async () => {
      if (!pendingVaultId) throw new Error('No folder is selected');
      return provider.mountRepo({ kind: 'team', location: pendingVaultId, docsFolder: '' });
    },
    onSuccess: finish,
    onError: (cause: Error) => {
      setError(cause.message);
    },
  });

  /**
   * A folder that is not a team repository yet is mounted first and then
   * written into: the core has to hold the folder before it can scaffold a
   * `team.yaml` in it (story GIT-US-0034).
   */
  const createTeam = useMutation({
    mutationFn: async (values: CreateTeamValues) => {
      if (!pendingVaultId) throw new Error('No folder is selected');
      await provider.mountRepo({ kind: 'team', location: pendingVaultId, docsFolder: '' });
      return provider.createTeam({
        repoId: pendingVaultId,
        key: values.key,
        ...(values.name === '' ? {} : { name: values.name }),
        ...(values.description === '' ? {} : { description: values.description }),
      });
    },
    onSuccess: finish,
    onError: (cause: Error) => {
      setError(cause.message);
    },
  });

  const candidates: DocsFolderCandidate[] = detection.data?.candidates ?? [];
  /** The `team.yaml` detection found at the root, or null. */
  const teamFound = detection.data?.team ?? null;
  /**
   * The role in force: what the user chose, or what the markers imply — a root
   * `team.yaml` means team, anything else means project.
   */
  const kind: RepoKind = role ?? (teamFound ? 'team' : 'project');
  const teamBusy = createTeam.isPending || mountTeam.isPending;
  /** No backlog anywhere: the wizard offers to create one instead of a dead end. */
  const noBacklog = detection.isSuccess && candidates.length === 0;
  /** Folders detection saw, plus the convention, offered as one-click choices. */
  const suggestions = [...new Set(['docs', ...candidates.map((c) => c.docsFolder)])];
  const selected = choice ?? candidates[0]?.docsFolder ?? CUSTOM_CHOICE;
  const docsFolder =
    selected === CUSTOM_CHOICE ? normalizeDocsFolder(customFolder) : normalizeDocsFolder(selected);

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <header className="space-y-1">
        <h1 className="page-title">Add repository</h1>
        <p className="text-sm text-muted-foreground">
          Choose a folder on this device. It is read in the browser: nothing is uploaded.
        </p>
      </header>

      {provider.kind === 'companion' ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Terminal aria-hidden="true" className="h-4 w-4" />
              Register it with the CLI
            </CardTitle>
            <CardDescription>
              With the companion running, the workspace is the configuration file it read at
              startup, and only the CLI writes that file. The web app cannot register a repository
              for you, so here is the command that does.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <pre className="overflow-x-auto rounded-md bg-secondary p-3 text-xs">
              <code>
                gintrack add /path/to/repo --key ACME{'\n'}
                gintrack add /path/to/team-repo --team --key ACME-TEAM
              </code>
            </pre>
            <p className="text-muted-foreground">
              The second form creates the <code>team.yaml</code> when the folder has none. Reload
              this page afterwards.
            </p>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <FolderGit2 aria-hidden="true" className="h-4 w-4" />
            1. Location
          </CardTitle>
          <CardDescription>
            {pendingVaultName
              ? `Selected folder: ${pendingVaultName}`
              : 'Pick the repository folder, not the docs folder inside it.'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FolderPickers
            onPicked={(vaultId, name) => {
              setError(null);
              setChoice(null);
              setPendingVault(vaultId, name);
            }}
            onError={setError}
          />
        </CardContent>
      </Card>

      {pendingVaultId ? (
        <Card>
          <CardHeader>
            <CardTitle>2. What is this repository?</CardTitle>
            <CardDescription>
              A project repository holds a backlog in <code>.pmngr/</code>; a team repository holds
              a <code>team.yaml</code> with the boards, sprints and retros of a team.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {detection.isPending ? (
              <p className="text-sm text-muted-foreground">Scanning the folder…</p>
            ) : null}

            {detection.isError ? (
              <p role="alert" className="text-sm text-destructive">
                {detection.error.message}
              </p>
            ) : null}

            {detection.data ? (
              <p className="text-sm text-muted-foreground">
                {detection.data.fileCount} text files scanned
                {detection.data.writable ? '' : ' · opened read-only'}
              </p>
            ) : null}

            {detection.isSuccess ? (
              <fieldset className="space-y-2">
                <legend className="text-sm font-medium">Role</legend>
                <label className="flex items-start gap-2 text-sm" htmlFor="role-project">
                  <input
                    id="role-project"
                    type="radio"
                    name="repo-role"
                    className="mt-1"
                    value="project"
                    checked={kind === 'project'}
                    onChange={() => {
                      setError(null);
                      setRole('project');
                    }}
                  />
                  <span>
                    Project repository
                    <span className="block text-xs text-muted-foreground">
                      {candidates.length === 0
                        ? 'No .pmngr/project.yaml found — one can be created below.'
                        : `${candidates.length} backlog folder(s) detected.`}
                    </span>
                  </span>
                </label>
                <label className="flex items-start gap-2 text-sm" htmlFor="role-team">
                  <input
                    id="role-team"
                    type="radio"
                    name="repo-role"
                    className="mt-1"
                    value="team"
                    checked={kind === 'team'}
                    onChange={() => {
                      setError(null);
                      setRole('team');
                    }}
                  />
                  <span>
                    Team repository
                    <span className="block text-xs text-muted-foreground">
                      {teamFound
                        ? `team.yaml found${teamFound.teamKey ? ` · ${teamFound.teamKey}` : ''}${
                            teamFound.teamName ? ` — ${teamFound.teamName}` : ''
                          }`
                        : 'No team.yaml found — one can be created below.'}
                    </span>
                  </span>
                </label>
              </fieldset>
            ) : null}

            {kind === 'team' ? (
              <div className="space-y-3">
                {teamFound ? (
                  <p className="flex items-start gap-2 rounded-md bg-secondary p-3 text-sm">
                    <Users aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
                    <span>
                      This folder holds a <code>team.yaml</code>. Mounting it as a team repository
                      is what makes its boards, sprints and retrospectives appear.
                    </span>
                  </p>
                ) : (
                  <>
                    <p className="flex items-start gap-2 rounded-md bg-secondary p-3 text-sm">
                      <Info aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
                      <span>
                        No <code>team.yaml</code> was found. Registering the folder without one
                        would leave every board, sprint and retro failing, so it is created here
                        instead.
                      </span>
                    </p>
                    <CreateTeamForm
                      busy={teamBusy}
                      onSubmit={(values) => {
                        setError(null);
                        createTeam.mutate(values);
                      }}
                    />
                  </>
                )}
                {error ? (
                  <p role="alert" className="text-sm text-destructive">
                    {error}
                  </p>
                ) : null}
              </div>
            ) : null}

            {kind === 'team' ? null : noBacklog ? (
              <div className="space-y-3">
                <p className="flex items-start gap-2 rounded-md bg-secondary p-3 text-sm">
                  <Info aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
                  <span>
                    No <code>.pmngr/project.yaml</code> was found in this repository. Say where the
                    project&rsquo;s Markdown should live and it is created for you.
                  </span>
                </p>
                <CreateProjectForm
                  suggestions={suggestions}
                  busy={create.isPending || mount.isPending}
                  onSubmit={(values) => {
                    setError(null);
                    create.mutate(values);
                  }}
                  onSkip={() => {
                    setError(null);
                    mount.mutate(normalizeDocsFolder(customFolder));
                  }}
                />
                {error ? (
                  <p role="alert" className="text-sm text-destructive">
                    {error}
                  </p>
                ) : null}
              </div>
            ) : null}

            {kind === 'team' || noBacklog ? null : (
              <fieldset className="space-y-2">
                <legend className="text-sm font-medium">Detected folders</legend>
                {candidates.map((candidate) => (
                  <label
                    key={candidate.projectFile}
                    className="flex items-center gap-2 text-sm"
                    htmlFor={`docs-${candidate.projectFile}`}
                  >
                    <input
                      id={`docs-${candidate.projectFile}`}
                      type="radio"
                      name="docs-folder"
                      value={candidate.docsFolder}
                      checked={selected === candidate.docsFolder}
                      onChange={() => {
                        setChoice(candidate.docsFolder);
                      }}
                    />
                    <span>
                      <code>
                        {candidate.docsFolder === '' ? '(repository root)' : candidate.docsFolder}
                      </code>
                      {candidate.projectKey ? ` · ${candidate.projectKey}` : ''}
                      {candidate.projectName ? ` — ${candidate.projectName}` : ''}
                      {candidate.declarationNeeded ? (
                        <em className="ml-1 text-xs not-italic text-muted-foreground">
                          (nested: indexed only if you choose it here)
                        </em>
                      ) : null}
                    </span>
                  </label>
                ))}

                <label className="flex items-center gap-2 text-sm" htmlFor="docs-custom">
                  <input
                    id="docs-custom"
                    type="radio"
                    name="docs-folder"
                    value={CUSTOM_CHOICE}
                    checked={selected === CUSTOM_CHOICE}
                    onChange={() => {
                      setChoice(CUSTOM_CHOICE);
                    }}
                  />
                  <span>Another folder</span>
                </label>

                {selected === CUSTOM_CHOICE ? (
                  <label
                    className="block space-y-1.5 text-sm font-medium"
                    htmlFor="docs-folder-path"
                  >
                    Documentation folder
                    <Input
                      id="docs-folder-path"
                      value={customFolder}
                      placeholder="docs"
                      onChange={(event) => {
                        setCustomFolder(event.target.value);
                      }}
                    />
                  </label>
                ) : null}
              </fieldset>
            )}
          </CardContent>
        </Card>
      ) : null}

      {pendingVaultId && kind === 'team' && teamFound ? (
        <Card>
          <CardHeader>
            <CardTitle>3. Confirm</CardTitle>
            <CardDescription>
              Mounting indexes the team repository and stores the folder handle in this browser so
              it reopens next time.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <p className="text-sm">
              Team: <code>{teamFound.teamKey ?? 'team.yaml'}</code>
              {teamFound.teamName ? ` — ${teamFound.teamName}` : ''}
            </p>
            <Button
              onClick={() => {
                setError(null);
                mountTeam.mutate();
              }}
              disabled={teamBusy || detection.isPending}
            >
              <CheckCircle2 aria-hidden="true" className="h-4 w-4" />
              {mountTeam.isPending ? 'Mounting…' : 'Mount team repository'}
            </Button>
            {error ? (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {pendingVaultId && kind === 'project' && !noBacklog ? (
        <Card>
          <CardHeader>
            <CardTitle>3. Confirm</CardTitle>
            <CardDescription>
              Mounting indexes the folder with the built-in core and stores the folder handle in
              this browser so it reopens next time.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <p className="text-sm">
              Documentation folder:{' '}
              <code>{docsFolder === '' ? '(repository root)' : docsFolder}</code>
            </p>
            <Button
              onClick={() => {
                setError(null);
                mount.mutate(docsFolder);
              }}
              disabled={mount.isPending || detection.isPending}
            >
              <CheckCircle2 aria-hidden="true" className="h-4 w-4" />
              {mount.isPending ? 'Mounting…' : 'Mount repository'}
            </Button>
            {error ? (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Browser support</CardTitle>
          <CardDescription>
            Writing files back needs the File System Access API. Everything else works everywhere.
          </CardDescription>
        </CardHeader>
        <CardContent className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase text-muted-foreground">
              <tr>
                <th scope="col" className="py-1 pr-3">
                  Browser
                </th>
                <th scope="col" className="py-1 pr-3">
                  Folder picker
                </th>
                <th scope="col" className="py-1 pr-3">
                  Read
                </th>
                <th scope="col" className="py-1 pr-3">
                  Write
                </th>
                <th scope="col" className="py-1">
                  Notes
                </th>
              </tr>
            </thead>
            <tbody>
              {SUPPORT_MATRIX.map((row) => (
                <tr key={row.browser} className="border-t border-border">
                  <th scope="row" className="py-1 pr-3 font-medium">
                    {row.browser}
                  </th>
                  <td className="py-1 pr-3">{row.picker}</td>
                  <td className="py-1 pr-3">{row.read}</td>
                  <td className="py-1 pr-3">{row.write}</td>
                  <td className="py-1 text-muted-foreground">{row.notes}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
