import { useQuery } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { useState } from 'react';

import type { RetroActionView, RetroCategory, RetroState, RetroThemeView } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { useToast } from '@/components/ui/toast';
import { OpenActionList } from '@/features/retros/OpenActionList';
import { usePromoteRetroAction, useRetro, useUpdateRetro } from '@/features/retros/retro-queries';

/** The three collection columns, in the order the file writes them. */
const COLUMNS: { category: RetroCategory; label: string }[] = [
  { category: 'went_well', label: 'Went well' },
  { category: 'to_improve', label: 'To improve' },
  { category: 'puzzle', label: 'Puzzles' },
];

/** The facilitation stages, in the order a session walks them. */
const STATES: RetroState[] = ['collecting', 'voting', 'discussing', 'closed'];

/**
 * One retro, run and read (docs/05-web-app.md §4, docs/04-team-repository.md §9).
 *
 * Three columns of sticky notes, the themes ranked by the votes they got, and
 * the improvement actions. An action is a trackable item, never free text: it
 * has an owner, a due date and a "promote" button that creates a task in a
 * project repository and writes the reference back here, so the origin of the
 * work is never lost (R-RETRO-2). What the previous retro left open sits at the
 * top, because the point is following through.
 */
export function RetroBoard() {
  const { retroId } = useParams({ from: '/retros/$retroId' });
  return <RetroCanvas retroId={retroId} />;
}

/**
 * The retro itself, addressed by id rather than by route, so that a test — and
 * any other caller — can render one without a router around it.
 */
export function RetroCanvas({ retroId }: { retroId: string }) {
  const retro = useRetro(retroId);
  const provider = useProvider();
  const team = useQuery({ queryKey: ['team'], queryFn: () => provider.getTeam() });
  const update = useUpdateRetro();
  const promote = usePromoteRetroAction();
  const { toast } = useToast();

  const [notes, setNotes] = useState<Record<string, string>>({});
  const [action, setAction] = useState({ title: '', owner: '', due: '' });

  const view = retro.data;
  const rev = view?.retro.rev;
  const author = team.data?.members[0]?.handle ?? '';
  const projects = (team.data?.projects ?? []).filter((project) => project.cloned);

  if (retro.isPending) return <p className="text-sm text-muted-foreground">Loading retro…</p>;
  if (!view) return <p className="text-sm text-muted-foreground">No retro {retroId}.</p>;

  const refuse = (title: string) => (error: Error) =>
    toast({ variant: 'destructive', title, description: error.message });

  const edit = (patch: Parameters<typeof update.mutate>[0]['patch'], title: string) =>
    update.mutate({ id: view.retro.id, patch, rev }, { onError: refuse(title) });

  const addNote = (category: RetroCategory) => {
    const text = (notes[category] ?? '').trim();
    if (text === '') return;
    edit(
      { addNotes: [{ category, text, ...(author === '' ? {} : { author }) }] },
      'The note could not be saved',
    );
    setNotes((current) => ({ ...current, [category]: '' }));
  };

  const vote = (theme: RetroThemeView) => {
    const voters = new Set(theme.voters ?? []);
    if (voters.has(author)) voters.delete(author);
    else voters.add(author);
    const votes: Record<string, string[]> = {};
    for (const each of view.themes) {
      votes[each.id] = each.id === theme.id ? [...voters] : [...(each.voters ?? [])];
    }
    edit({ votes }, 'The vote could not be saved');
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="flex flex-wrap items-center gap-2 text-2xl font-semibold tracking-tight">
          {view.retro.title}
          <Badge variant="outline" size="sm" className="font-normal">
            {view.retro.state}
          </Badge>
        </h1>
        <p className="text-sm text-muted-foreground">
          {view.retro.date}
          {view.retro.sprint ? ` · ${view.retro.sprint}` : ''}
          {view.retro.participants?.length ? ` · ${view.retro.participants.join(', ')}` : ''}
        </p>
        <div className="flex flex-wrap items-center gap-2 pt-1">
          <label className="text-xs text-muted-foreground" htmlFor="retro-state">
            Stage
          </label>
          <Select
            id="retro-state"
            aria-label="Stage"
            value={view.retro.state}
            onChange={(event) =>
              edit({ state: event.target.value as RetroState }, 'The stage could not be changed')
            }
          >
            {STATES.map((state) => (
              <option key={state} value={state}>
                {state}
              </option>
            ))}
          </Select>
        </div>
      </header>

      <OpenActionList
        actions={view.carried}
        title="Carried from the previous retro"
        empty="Nothing was left open by the retro before this one."
      />

      <section className="grid gap-3 md:grid-cols-3" data-testid="retro-columns">
        {COLUMNS.map(({ category, label }) => (
          <Card key={category}>
            <CardHeader className="gap-1">
              <CardTitle className="text-base">{label}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <ul className="space-y-2 text-sm">
                {view.notes
                  .filter((note) => note.category === category)
                  .map((note) => (
                    <li key={note.id ?? note.text} className="rounded border p-2">
                      <p>{note.text}</p>
                      <div className="mt-1 flex items-center gap-2">
                        {note.author ? (
                          <span className="text-xs text-muted-foreground">{note.author}</span>
                        ) : null}
                        {note.id ? (
                          <button
                            type="button"
                            className="text-xs text-muted-foreground underline"
                            onClick={() =>
                              edit(
                                { removeNotes: [note.id as string] },
                                'The note could not be removed',
                              )
                            }
                          >
                            Remove
                          </button>
                        ) : null}
                      </div>
                    </li>
                  ))}
              </ul>
              <form
                aria-label={label}
                className="flex gap-2"
                onSubmit={(event) => {
                  event.preventDefault();
                  addNote(category);
                }}
              >
                <Input
                  aria-label={`Add a note to ${label}`}
                  value={notes[category] ?? ''}
                  onChange={(event) =>
                    setNotes((current) => ({ ...current, [category]: event.target.value }))
                  }
                />
                <Button type="submit" size="sm">
                  Add
                </Button>
              </form>
            </CardContent>
          </Card>
        ))}
      </section>

      <Card>
        <CardHeader className="gap-1">
          <CardTitle className="text-base">Themes</CardTitle>
          <CardDescription>
            {view.themes.length === 0
              ? 'Group duplicate notes into themes to vote on them.'
              : `${view.retro.voteBudget} votes each`}
          </CardDescription>
        </CardHeader>
        {view.themes.length === 0 ? null : (
          <CardContent>
            <ul className="space-y-2 text-sm">
              {view.themes.map((theme) => (
                <li key={theme.id} className="flex flex-wrap items-center gap-2">
                  <Button size="sm" variant="outline" onClick={() => vote(theme)}>
                    {theme.votes} ▲
                  </Button>
                  <span>{theme.title}</span>
                  <span className="text-xs text-muted-foreground">
                    {(theme.noteTexts ?? []).length} notes
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        )}
      </Card>

      <Card data-testid="retro-actions">
        <CardHeader className="gap-1">
          <CardTitle className="text-base">Improvement actions</CardTitle>
          <CardDescription>
            Every action needs an owner. Promoting one creates a task in a project repository and
            links it back here.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <ul className="space-y-3 text-sm">
            {view.actions.map((item) => (
              <ActionRow
                key={item.id}
                action={item}
                projects={projects.map((project) => project.key)}
                onToggleDone={() =>
                  edit(
                    { updateActions: [{ id: item.id, status: item.done ? 'proposed' : 'done' }] },
                    'The action could not be changed',
                  )
                }
                onPromote={(project) =>
                  promote.mutate(
                    {
                      retro: view.retro.id,
                      action: item.id,
                      project,
                      ...(rev === undefined ? {} : { rev }),
                    },
                    { onError: refuse('The action could not be promoted') },
                  )
                }
              />
            ))}
          </ul>

          <form
            aria-label="Add an improvement action"
            className="flex flex-wrap gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              if (action.title.trim() === '') return;
              edit(
                {
                  addActions: [
                    {
                      title: action.title.trim(),
                      ...(action.owner === '' ? {} : { owner: action.owner }),
                      ...(action.due === '' ? {} : { due: action.due }),
                    },
                  ],
                },
                'The action could not be saved',
              );
              setAction({ title: '', owner: '', due: '' });
            }}
          >
            <Input
              aria-label="Action"
              placeholder="What will we change?"
              className="min-w-48 flex-1"
              value={action.title}
              onChange={(event) => setAction({ ...action, title: event.target.value })}
            />
            <Input
              aria-label="Owner"
              placeholder="owner"
              className="w-32"
              value={action.owner}
              onChange={(event) => setAction({ ...action, owner: event.target.value })}
            />
            <Input
              aria-label="Due"
              type="date"
              className="w-40"
              value={action.due}
              onChange={(event) => setAction({ ...action, due: event.target.value })}
            />
            <Button type="submit" size="sm">
              Add action
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

/** One improvement action, with the live state of the task it became. */
function ActionRow({
  action,
  projects,
  onToggleDone,
  onPromote,
}: {
  action: RetroActionView;
  projects: string[];
  onToggleDone: () => void;
  onPromote: (project: string) => void;
}) {
  const [project, setProject] = useState(projects[0] ?? '');

  return (
    <li className="flex flex-wrap items-center gap-2 rounded border p-2">
      <input
        type="checkbox"
        aria-label={`${action.title} is done`}
        checked={action.done}
        disabled={Boolean(action.task)}
        onChange={onToggleDone}
      />
      <span className={action.done ? 'line-through' : undefined}>{action.title}</span>
      {action.owner ? (
        <Badge variant="outline" size="sm" className="font-normal">
          {action.owner}
        </Badge>
      ) : (
        <Badge variant="destructive" size="sm" className="font-normal">
          no owner
        </Badge>
      )}
      {action.due ? <span className="text-xs text-muted-foreground">due {action.due}</span> : null}
      {action.task ? (
        <span className="text-xs text-muted-foreground">
          {action.task}
          {action.card?.status ? ` · ${action.card.status}` : ''}
        </span>
      ) : projects.length === 0 ? (
        <span className="text-xs text-muted-foreground">
          Clone a project to promote this action.
        </span>
      ) : (
        <>
          <Select
            aria-label={`Project for ${action.title}`}
            value={project}
            onChange={(event) => setProject(event.target.value)}
            className="w-28"
          >
            {projects.map((key) => (
              <option key={key} value={key}>
                {key}
              </option>
            ))}
          </Select>
          <Button size="sm" variant="outline" onClick={() => onPromote(project)}>
            Promote to task
          </Button>
        </>
      )}
    </li>
  );
}
