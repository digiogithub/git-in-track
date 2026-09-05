import { Link } from '@tanstack/react-router';
import { NotebookPen } from 'lucide-react';
import { useState } from 'react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { useToast } from '@/components/ui/toast';
import { useSprints } from '@/features/boards/sprint-queries';
import { OpenActionList } from '@/features/retros/OpenActionList';
import { useCreateRetro, useRetros } from '@/features/retros/retro-queries';
import { useActiveTeam } from '@/features/workspace/active-team';
import { TeamSelector } from '@/features/workspace/TeamSelector';

/**
 * Retro index (docs/05-web-app.md §4, docs/04-team-repository.md §9).
 *
 * The open actions of every past retro sit above the list, not below it: the
 * point of writing a retro down is following through, so what the team still
 * owes itself is the first thing it reads. Starting a retro for a closed sprint
 * is one click, because the friction of starting one is what kills the habit.
 */
export function RetroList() {
  const retros = useRetros();
  const sprints = useSprints();
  const create = useCreateRetro();
  const team = useActiveTeam();
  const { toast } = useToast();
  const [title, setTitle] = useState('');

  /**
   * Why a retro cannot be started, or null when it can. Retros live in a team
   * repository: with none open the call used to fail with a raw `not_found`
   * from the workspace, so the buttons are disabled with the reason instead
   * (story GIT-US-0035).
   */
  const blocked =
    team.isPending ? 'Loading the team repositories…'
    : team.team === undefined ? 'Retros live in a team repository, and none is open.'
    : null;

  const listing = retros.data;
  const covered = new Set((listing?.retros ?? []).map((retro) => retro.sprint).filter(Boolean));
  const closable = (sprints.data ?? []).filter(
    (sprint) => sprint.state === 'closed' && !covered.has(sprint.id),
  );

  const start = (sprint?: string) => {
    create.mutate(
      {
        ...(sprint === undefined ? {} : { sprint }),
        ...(title.trim() === '' ? {} : { title: title.trim() }),
      },
      {
        onError: (error: Error) =>
          toast({
            variant: 'destructive',
            title: 'The retro could not be started',
            description: error.message,
          }),
        onSuccess: () => setTitle(''),
      },
    );
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="page-title">Retrospectives</h1>
          <TeamSelector />
        </div>
        <p className="text-sm text-muted-foreground">
          Retros live in the team repository, next to the work they are about.
        </p>
      </header>

      <OpenActionList
        actions={listing?.carried ?? []}
        title="Open improvement actions"
        empty="Every improvement action from a past retro is done or dropped."
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Start a retro</CardTitle>
          <CardDescription>
            A retro for a closed sprint inherits its board, its title and its participants.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {blocked ? <p className="text-sm text-muted-foreground">{blocked}</p> : null}
          <Input
            aria-label="Retro title"
            placeholder="Title (optional)"
            value={title}
            disabled={blocked !== null}
            onChange={(event) => setTitle(event.target.value)}
          />
          <div className="flex flex-wrap gap-2">
            {closable.map((sprint) => (
              <Button
                key={sprint.id}
                size="sm"
                disabled={create.isPending || blocked !== null}
                title={blocked ?? undefined}
                onClick={() => start(sprint.id)}
              >
                Retro for {sprint.title}
              </Button>
            ))}
            <Button
              size="sm"
              variant="outline"
              disabled={create.isPending || blocked !== null}
              title={blocked ?? undefined}
              onClick={() => start()}
            >
              Retro without a sprint
            </Button>
          </div>
        </CardContent>
      </Card>

      {retros.isPending ? <p className="text-sm text-muted-foreground">Loading retros…</p> : null}

      {!retros.isPending && (listing?.retros ?? []).length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>{team.team ? 'No retro yet' : 'No team repository is open'}</CardTitle>
            <CardDescription>
              {team.team
                ? 'Start one above; a retro for a closed sprint inherits its board and participants.'
                : 'Retrospectives live in a team repository — a folder holding a team.yaml.'}
            </CardDescription>
          </CardHeader>
          {team.team ? null : (
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <p>Add one from a folder on this device, or create one where there is none yet.</p>
              <Link
                to="/repos/add"
                className="inline-flex items-center gap-1 rounded-full border border-border px-3 py-1 text-xs hover:bg-secondary"
              >
                Add or create a team repository
              </Link>
            </CardContent>
          )}
        </Card>
      ) : null}

      <ul className="grid gap-3 sm:grid-cols-2">
        {(listing?.retros ?? []).map((retro) => (
          <li key={retro.id}>
            <Link to="/retros/$retroId" params={{ retroId: retro.id }} className="block">
              <Card className="h-full transition-colors hover:border-primary">
                <CardHeader className="gap-1">
                  <CardTitle className="flex items-center gap-2 text-base">
                    <NotebookPen aria-hidden="true" className="h-4 w-4" />
                    {retro.title}
                    <Badge variant="outline" size="sm" className="font-normal">
                      {retro.state}
                    </Badge>
                  </CardTitle>
                  <CardDescription>
                    {retro.date}
                    {retro.sprint ? ` · ${retro.sprint}` : ''}
                  </CardDescription>
                </CardHeader>
                <CardContent className="text-sm text-muted-foreground">
                  {retro.metrics.actions === 0
                    ? 'No improvement action yet.'
                    : `${retro.metrics.open} of ${retro.metrics.actions} actions still open`}
                  {retro.metrics.noOwner > 0 ? ` · ${retro.metrics.noOwner} without an owner` : ''}
                </CardContent>
              </Card>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
