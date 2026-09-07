import { useParams } from '@tanstack/react-router';
import { useMemo, useState } from 'react';

import type {
  RetroActionView,
  RetroCategory,
  RetroComment,
  RetroNote,
  RetroState,
  RetroTheme,
  RetroThemeView,
  RetroView,
} from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { useToast } from '@/components/ui/toast';
import { OpenActionList } from '@/features/retros/OpenActionList';
import {
  usePromoteRetroAction,
  useRetro,
  useRetroEvents,
  useUpdateRetro,
} from '@/features/retros/retro-queries';
import { RetroShare } from '@/features/retros/RetroShare';
import { useActiveTeam } from '@/features/workspace/active-team';
import { useIdentity, type Identity } from '@/features/workspace/identity';
import { ParticipantBar } from '@/features/workspace/ParticipantBar';

/** The three collection columns, in the order the file writes them. */
const COLUMNS: { category: RetroCategory; label: string }[] = [
  { category: 'went_well', label: 'Went well' },
  { category: 'to_improve', label: 'To improve' },
  { category: 'puzzle', label: 'Puzzles' },
];

/** The facilitation stages, in the order a session walks them. */
const STATES: RetroState[] = ['collecting', 'voting', 'discussing', 'closed'];

/** What the stage the retro is in lets a participant do (docs/04 §9.1). */
const STAGE_HINT: Record<RetroState, string> = {
  collecting: 'Write what you saw. Voting opens when the facilitator moves the stage on.',
  voting: 'Spend your votes on the notes that matter most. Click a note again to take it back.',
  discussing: 'Talk through the notes, highest voted first, and leave what you agree on as a comment.',
  closed: 'This retro is closed. Its improvement actions live on in the projects they were promoted into.',
};

/**
 * One retro, run and read (docs/05-web-app.md §4, docs/04-team-repository.md §9).
 *
 * Three columns of sticky notes, the notes ranked by the votes they got, and
 * the improvement actions. An action is a trackable item, never free text: it
 * has an owner, a due date and a "promote" button that creates a task in a
 * project repository and writes the reference back here, so the origin of the
 * work is never lost (R-RETRO-2). What the previous retro left open sits at the
 * top, because the point is following through.
 *
 * Several people run it at once from their own browsers, against one companion
 * shared over a tunnel: each names themselves once, the wall refreshes as the
 * others write, and the stage decides what everybody can do — collect, vote,
 * or comment.
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
  const team = useActiveTeam();
  const identity = useIdentity();
  const update = useUpdateRetro();
  const promote = usePromoteRetroAction();
  const { toast } = useToast();
  useRetroEvents(retroId);

  const [notes, setNotes] = useState<Record<string, string>>({});
  const [action, setAction] = useState({ title: '', owner: '', due: '' });

  const view = retro.data;
  const rev = view?.retro.rev;
  const author = identity.handle;
  const projects = (team.team?.projects ?? []).filter((project) => project.cloned);

  // The theme a note was grouped into, and the comments hanging off each card.
  // Voting is per note: a note that nobody grouped becomes a theme of its own
  // the first time somebody votes for it, so the room votes on what it wrote
  // rather than on a grouping it has to build first.
  const themeOfNote = useMemo(() => indexThemesByNote(view), [view]);
  const commentsOf = useMemo(() => indexCommentsByTarget(view), [view]);

  if (retro.isPending) return <p className="text-sm text-muted-foreground">Loading retro…</p>;
  if (!view) return <p className="text-sm text-muted-foreground">No retro {retroId}.</p>;

  const stage = view.retro.state;
  const budget = view.retro.voteBudget;
  const spent = votesCast(view, author);

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

  /**
   * Casts or takes back this browser's vote on one note. The ballot is keyed by
   * theme, so a note nobody grouped is promoted to a theme of its own in the
   * same write — one patch, one file, one line of diff per participant.
   */
  const voteNote = (note: RetroNote) => {
    if (note.id === undefined || author === '') return;
    const patch: Parameters<typeof edit>[0] = {};
    let themeId = themeOfNote.get(note.id)?.id;
    if (themeId === undefined) {
      themeId = `t-${note.id}`.slice(0, 16);
      patch.themes = [...view.themes.map(bareTheme), themeOf(note, themeId)];
    }
    const votes: Record<string, string[]> = {};
    for (const theme of view.themes) votes[theme.id] = [...(theme.voters ?? [])];
    const voters = new Set(votes[themeId] ?? []);
    if (voters.has(author)) voters.delete(author);
    else voters.add(author);
    votes[themeId] = [...voters];
    patch.votes = votes;
    edit(patch, 'The vote could not be saved');
  };

  const addComment = (note: RetroNote, text: string) => {
    if (note.id === undefined || text.trim() === '') return;
    edit(
      {
        addComments: [
          { note: note.id, text: text.trim(), ...(author === '' ? {} : { author }) },
        ],
      },
      'The comment could not be saved',
    );
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="flex flex-wrap items-center gap-2 page-title">
          {view.retro.title}
          <Badge variant="outline" size="sm" className="font-normal">
            {stage}
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
            value={stage}
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
          {stage === 'voting' && identity.known ? (
            <span className="text-xs text-muted-foreground">
              {budget - spent} of {budget} votes left
            </span>
          ) : null}
        </div>
        <p className="text-xs text-muted-foreground">{STAGE_HINT[stage]}</p>
        <RetroShare retroId={view.retro.id} />
      </header>

      <ParticipantBar identity={identity} />

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
                {sortNotes(view, category, themeOfNote, stage).map((note) => (
                  <NoteCard
                    key={note.id ?? note.text}
                    note={note}
                    stage={stage}
                    identity={identity}
                    votes={note.id === undefined ? 0 : (themeOfNote.get(note.id)?.votes ?? 0)}
                    mine={
                      note.id !== undefined &&
                      (themeOfNote.get(note.id)?.voters ?? []).includes(author)
                    }
                    exhausted={spent >= budget}
                    comments={note.id === undefined ? [] : (commentsOf.get(note.id) ?? [])}
                    onVote={() => voteNote(note)}
                    onComment={(text) => addComment(note, text)}
                    onRemove={() =>
                      edit({ removeNotes: [note.id as string] }, 'The note could not be removed')
                    }
                  />
                ))}
              </ul>
              {stage === 'collecting' ? (
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
                    disabled={!identity.known}
                    onChange={(event) =>
                      setNotes((current) => ({ ...current, [category]: event.target.value }))
                    }
                  />
                  <Button type="submit" size="sm" disabled={!identity.known}>
                    Add
                  </Button>
                </form>
              ) : null}
            </CardContent>
          </Card>
        ))}
      </section>

      {view.themes.length === 0 ? null : (
        <Card>
          <CardHeader className="gap-1">
            <CardTitle className="text-base">Ranked</CardTitle>
            <CardDescription>
              What the room voted for, most votes first. {budget} votes each.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="space-y-2 text-sm">
              {view.themes.map((theme) => (
                <li key={theme.id} className="flex flex-wrap items-center gap-2">
                  <Badge variant="outline" size="sm" className="font-normal">
                    {theme.votes} ▲
                  </Badge>
                  <span>{theme.title}</span>
                  {(theme.noteTexts ?? []).length > 1 ? (
                    <span className="text-xs text-muted-foreground">
                      {(theme.noteTexts ?? []).length} notes
                    </span>
                  ) : null}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

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

/** The theme each note was grouped into, by note id. */
function indexThemesByNote(view: RetroView | undefined): Map<string, RetroThemeView> {
  const out = new Map<string, RetroThemeView>();
  for (const theme of view?.themes ?? []) {
    for (const id of theme.notes ?? []) out.set(id, theme);
  }
  return out;
}

/** The comments hanging off each note or theme, by target id. */
function indexCommentsByTarget(view: RetroView | undefined): Map<string, RetroComment[]> {
  const out = new Map<string, RetroComment[]>();
  for (const comment of view?.comments ?? []) {
    const target = comment.note ?? comment.theme;
    if (target === undefined) continue;
    out.set(target, [...(out.get(target) ?? []), comment]);
  }
  return out;
}

/** How many votes a handle has spent across every theme. */
function votesCast(view: RetroView, handle: string): number {
  if (handle === '') return 0;
  return view.themes.filter((theme) => (theme.voters ?? []).includes(handle)).length;
}

/** A theme view stripped back to the fields a patch writes. */
function bareTheme(theme: RetroThemeView): RetroTheme {
  return {
    id: theme.id,
    title: theme.title,
    ...(theme.category === undefined ? {} : { category: theme.category }),
    ...(theme.notes === undefined ? {} : { notes: theme.notes }),
  };
}

/** The theme a note becomes the first time somebody votes for it. */
function themeOf(note: RetroNote, id: string): RetroTheme {
  return { id, title: note.text.slice(0, 120), category: note.category, notes: [note.id as string] };
}

/**
 * The notes of one column. From the voting stage on they are ordered by the
 * votes they got, so the room reads the wall in the order it agreed to discuss
 * it; while collecting they stay in the order they were written.
 */
function sortNotes(
  view: RetroView,
  category: RetroCategory,
  themeOfNote: Map<string, RetroThemeView>,
  stage: RetroState,
): RetroNote[] {
  const notes = view.notes.filter((note) => note.category === category);
  if (stage === 'collecting') return notes;
  const votes = (note: RetroNote) =>
    note.id === undefined ? 0 : (themeOfNote.get(note.id)?.votes ?? 0);
  return [...notes].sort((a, b) => votes(b) - votes(a));
}

/** One sticky note: its text, its votes and the discussion hanging off it. */
function NoteCard({
  note,
  stage,
  identity,
  votes,
  mine,
  exhausted,
  comments,
  onVote,
  onComment,
  onRemove,
}: {
  note: RetroNote;
  stage: RetroState;
  identity: Identity;
  votes: number;
  mine: boolean;
  exhausted: boolean;
  comments: RetroComment[];
  onVote: () => void;
  onComment: (text: string) => void;
  onRemove: () => void;
}) {
  const [draft, setDraft] = useState('');
  // An unidentified visitor may read the wall but not write on it, and a
  // participant who has spent the budget may still take a vote back.
  const canVote = stage === 'voting' && identity.known && note.id !== undefined && (mine || !exhausted);
  const showVotes = stage !== 'collecting' || votes > 0;

  return (
    <li className="rounded border p-2">
      <div className="flex items-start gap-2">
        {showVotes ? (
          <Button
            size="sm"
            variant={mine ? 'default' : 'outline'}
            disabled={!canVote}
            aria-label={mine ? `Take back your vote for ${note.text}` : `Vote for ${note.text}`}
            onClick={onVote}
          >
            {votes} ▲
          </Button>
        ) : null}
        <p className="flex-1">{note.text}</p>
      </div>
      <div className="mt-1 flex items-center gap-2">
        {note.author ? <span className="text-xs text-muted-foreground">{note.author}</span> : null}
        {note.id && stage === 'collecting' ? (
          <button type="button" className="text-xs text-muted-foreground underline" onClick={onRemove}>
            Remove
          </button>
        ) : null}
        {stage === 'voting' && !identity.known ? (
          <span className="text-xs text-muted-foreground">Name yourself above to vote.</span>
        ) : null}
      </div>

      {comments.length === 0 ? null : (
        <ul className="mt-2 space-y-1 border-t pt-2 text-xs">
          {comments.map((comment) => (
            <li key={comment.id}>
              {comment.author ? (
                <span className="font-medium text-foreground">{comment.author}: </span>
              ) : null}
              <span className="text-muted-foreground">{comment.text}</span>
            </li>
          ))}
        </ul>
      )}

      {stage === 'discussing' && note.id !== undefined ? (
        <form
          aria-label={`Discussion of ${note.text}`}
          className="mt-2 flex gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (draft.trim() === '') return;
            onComment(draft);
            setDraft('');
          }}
        >
          <Input
            aria-label={`Comment on ${note.text}`}
            placeholder="What did we agree?"
            className="h-8 text-xs"
            value={draft}
            disabled={!identity.known}
            onChange={(event) => setDraft(event.target.value)}
          />
          <Button type="submit" size="sm" variant="outline" disabled={!identity.known}>
            Comment
          </Button>
        </form>
      ) : null}
    </li>
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
          Promoting needs a project repository: declare one in the team’s Projects settings and
          open its clone in this workspace. Until then the action stays here.
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
