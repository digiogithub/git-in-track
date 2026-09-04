import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { BookOpen, FolderGit2, ListChecks, RefreshCw, ShieldAlert, Trash2 } from 'lucide-react';
import { useState } from 'react';

import type { RepoInfo } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { useAppStore } from '@/app/store';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { FsaVault, getHandleRecord, registerVault, requestPermission } from '@/fs';

import { FolderPickers } from './FolderPickers';

const STATE_LABELS: Record<RepoInfo['state'], string> = {
  ready: 'Ready',
  'needs-permission': 'Needs permission',
  indexing: 'Indexing',
  error: 'Error',
};

const STATE_CLASSES: Record<RepoInfo['state'], string> = {
  ready: 'bg-secondary text-secondary-foreground',
  'needs-permission': 'bg-destructive/10 text-destructive',
  indexing: 'bg-secondary text-secondary-foreground',
  error: 'bg-destructive/10 text-destructive',
};

/**
 * Landing surface (docs/05-web-app.md §3.1): every mounted repository with its
 * state and projects, plus the two ways to open a folder. Nothing here uploads
 * anything: the folder stays on the device.
 */
export function WorkspaceHome() {
  const provider = useProvider();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const setPendingVault = useAppStore((state) => state.setPendingVault);
  // In companion mode the repositories come from the configuration file, so the
  // browser folder picker would be misleading: `gintrack add` is the way in.
  const companion = useAppStore((state) => state.mode) === 'companion';
  const [error, setError] = useState<string | null>(null);

  const repos = useQuery({ queryKey: ['repos'], queryFn: () => provider.listRepos() });

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['repos'] });
    await queryClient.invalidateQueries({ queryKey: ['projects'] });
  };

  const reindex = useMutation({
    mutationFn: (repoId: string) => provider.reindex(repoId),
    onSuccess: invalidate,
    onError: (cause: Error) => {
      setError(cause.message);
    },
  });

  const remove = useMutation({
    mutationFn: (repoId: string) => provider.unmountRepo(repoId),
    onSuccess: invalidate,
    onError: (cause: Error) => {
      setError(cause.message);
    },
  });

  /**
   * Re-grants an expired handle. Chromium allows several `requestPermission`
   * calls inside one user gesture, so "Reconnect folders" repairs every
   * pending repository at once (docs/05-web-app.md §6.1).
   */
  const reconnect = useMutation({
    mutationFn: async (repoIds: string[]) => {
      for (const repoId of repoIds) {
        const record = await getHandleRecord(repoId);
        if (!record) continue;
        const permission = await requestPermission(record.handle, 'readwrite');
        if (permission !== 'granted') {
          throw new Error(`Permission for "${record.name}" was not granted`);
        }
        const vault = new FsaVault(record.handle);
        registerVault(vault, record.id);
        await provider.mountRepo({
          kind: record.kind,
          location: record.id,
          docsFolder: record.docsFolder,
        });
      }
    },
    onSuccess: invalidate,
    onError: (cause: Error) => {
      setError(cause.message);
    },
  });

  const rows = repos.data ?? [];
  const pending = rows.filter((repo) => repo.state === 'needs-permission');

  function startWizard(vaultId: string, name: string) {
    setError(null);
    setPendingVault(vaultId, name);
    void navigate({ to: '/repos/add' });
  }

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Workspace</h1>
        <p className="text-sm text-muted-foreground">
          {companion
            ? 'Repositories registered with the companion. Run `gintrack add <path>` to register another one.'
            : 'Open a project folder from this device. Files are read in your browser and never leave your machine.'}
        </p>
      </header>

      {companion ? null : (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FolderGit2 aria-hidden="true" className="h-4 w-4" />
              Open a project folder
            </CardTitle>
            <CardDescription>
              The next step detects <code>.pmngr/project.yaml</code> and confirms the documentation
              folder.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <FolderPickers onPicked={startWizard} onError={setError} />
            {pending.length > 0 ? (
              <Button
                variant="secondary"
                onClick={() => {
                  reconnect.mutate(pending.map((repo) => repo.id));
                }}
                disabled={reconnect.isPending}
              >
                <ShieldAlert aria-hidden="true" className="h-4 w-4" />
                Reconnect folders ({pending.length})
              </Button>
            ) : null}
          </CardContent>
        </Card>
      )}

      {error ? (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      ) : null}

      <section aria-labelledby="repos-heading" className="space-y-3">
        <h2 id="repos-heading" className="text-lg font-semibold tracking-tight">
          Repositories
        </h2>

        {repos.isPending ? (
          <p className="text-sm text-muted-foreground">Loading repositories…</p>
        ) : null}

        {!repos.isPending && rows.length === 0 ? (
          <Card>
            <CardHeader>
              <CardTitle>No repositories yet</CardTitle>
              <CardDescription>
                Open a folder that contains a <code>.pmngr</code> backlog to index its epics,
                stories, tasks and knowledge base.
              </CardDescription>
            </CardHeader>
          </Card>
        ) : null}

        <ul className="space-y-3">
          {rows.map((repo) => (
            <li key={repo.id}>
              <Card>
                <CardHeader>
                  <CardTitle className="flex flex-wrap items-center gap-2">
                    {repo.name}
                    <span
                      className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${STATE_CLASSES[repo.state]}`}
                    >
                      {STATE_LABELS[repo.state]}
                    </span>
                  </CardTitle>
                  <CardDescription>
                    {repo.kind === 'team' ? 'Team repository' : 'Project repository'} · docs folder:{' '}
                    <code>{repo.docsFolder === '' ? '(repository root)' : repo.docsFolder}</code>
                    {repo.lastIndexedAt ? ` · indexed ${repo.lastIndexedAt.slice(0, 16)}` : ''}
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-3">
                  {repo.state === 'needs-permission' ? (
                    <p className="text-sm text-muted-foreground">
                      This browser has expired the permission for this folder. Reconnect it to keep
                      reading and writing the same files.
                    </p>
                  ) : null}

                  {repo.projects.length > 0 ? (
                    <ul className="flex flex-wrap gap-2">
                      {repo.projects.map((project) => (
                        <li key={project} className="flex items-center gap-1">
                          <Link
                            to="/p/$project/items"
                            params={{ project }}
                            className="inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs hover:bg-secondary"
                          >
                            <ListChecks aria-hidden="true" className="h-3 w-3" />
                            {project} backlog
                          </Link>
                          <Link
                            to="/p/$project/kb/$"
                            params={{ project, _splat: '' }}
                            className="inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs hover:bg-secondary"
                          >
                            <BookOpen aria-hidden="true" className="h-3 w-3" />
                            {project} docs
                          </Link>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="text-sm text-muted-foreground">
                      No project was found in this folder yet.
                    </p>
                  )}

                  <div className="flex flex-wrap gap-2">
                    {repo.state === 'needs-permission' ? (
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => {
                          reconnect.mutate([repo.id]);
                        }}
                        disabled={reconnect.isPending}
                      >
                        <ShieldAlert aria-hidden="true" className="h-4 w-4" />
                        Reconnect
                      </Button>
                    ) : (
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => {
                          reindex.mutate(repo.id);
                        }}
                        disabled={reindex.isPending}
                      >
                        <RefreshCw aria-hidden="true" className="h-4 w-4" />
                        Reindex
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        remove.mutate(repo.id);
                      }}
                      disabled={remove.isPending}
                    >
                      <Trash2 aria-hidden="true" className="h-4 w-4" />
                      Remove
                    </Button>
                  </div>
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
