import { Link } from '@tanstack/react-router';
import { useMemo, useState } from 'react';

import type { BoardCard, BoardView, SprintCarry, SprintCarryAction } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Select } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { useToast } from '@/components/ui/toast';
import { NewSprintDialog } from '@/features/boards/NewSprintDialog';
import {
  useCloseSprint,
  useSprint,
  useStartSprint,
  useUpdateSprint,
} from '@/features/boards/sprint-queries';

/**
 * The scrum half of a board (docs/05-web-app.md §9, story GIT-US-0018).
 *
 * The header shows the goal, the dates, the days left and committed against
 * completed points. The planning dialog moves references in and out of the
 * sprint — one write to the sprint file in the team repository, so it works for
 * an item whose project nobody cloned. Starting a sprint freezes its
 * commitment; closing one reports what was finished and asks, per unfinished
 * item, whether to leave it, carry it into another sprint or send it back to
 * the backlog (docs/04 R-SPR-3).
 */
export function SprintPanel({ view }: { view: BoardView }) {
  const info = view.sprintInfo;
  const sprint = useSprint(info?.id);
  const update = useUpdateSprint();
  const start = useStartSprint();
  const close = useCloseSprint();
  const { toast } = useToast();

  const [planning, setPlanning] = useState(false);
  const [closing, setClosing] = useState(false);
  const [creating, setCreating] = useState(false);
  const [editingGoal, setEditingGoal] = useState(false);
  const [goal, setGoal] = useState(info?.goal ?? '');
  const [decisions, setDecisions] = useState<Record<string, SprintCarryAction>>({});

  const detail = sprint.data;
  const rev = detail?.sprint.rev ?? info?.rev;

  const incomplete = useMemo(
    () => (detail?.cards ?? []).filter((card) => !isDone(card)),
    [detail?.cards],
  );
  const completed = useMemo(
    () => (detail?.cards ?? []).filter((card) => isDone(card)),
    [detail?.cards],
  );

  if (!info) return null;

  /** Reports a refused write without swallowing the reason. */
  const refuse = (title: string) => (error: Error) => {
    toast({ variant: 'destructive', title, description: error.message });
  };

  const edit = (patch: Parameters<typeof update.mutate>[0]['patch'], title: string) => {
    update.mutate({ id: info.id, patch, rev }, { onError: refuse(title) });
  };

  return (
    <Card data-testid="sprint-panel">
      <CardHeader className="gap-1">
        <CardTitle className="flex flex-wrap items-center gap-2 text-base">
          <span>{info.title}</span>
          <Badge variant="outline" size="sm" className="font-normal">
            {info.state}
          </Badge>
          <span className="text-xs font-normal text-muted-foreground">
            {info.start} → {info.end}
          </span>
          <span className="text-xs font-normal text-muted-foreground">
            {info.state === 'closed'
              ? 'Sprint closed'
              : `${info.remainingDays} of ${info.totalDays} days left`}
          </span>
          <Link
            to="/metrics/$sprintId"
            params={{ sprintId: info.id }}
            className="text-xs font-normal text-muted-foreground underline underline-offset-2 hover:text-foreground"
          >
            Metrics
          </Link>
        </CardTitle>
        <p className="text-sm text-muted-foreground">
          {editingGoal ? null : (info.goal ?? 'No sprint goal yet.')}
        </p>
      </CardHeader>
      <CardContent className="space-y-3">
        {editingGoal ? (
          <form
            aria-label="Sprint goal"
            className="space-y-2"
            onSubmit={(event) => {
              event.preventDefault();
              edit({ goal }, 'The goal could not be saved');
              setEditingGoal(false);
            }}
          >
            <Textarea
              aria-label="Goal"
              value={goal}
              onChange={(event) => setGoal(event.target.value)}
              rows={2}
            />
            <div className="flex gap-2">
              <Button type="submit" size="sm">
                Save goal
              </Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setEditingGoal(false)}>
                Cancel
              </Button>
            </div>
          </form>
        ) : null}

        <dl className="flex flex-wrap gap-4 text-xs">
          <Metric label="Committed" value={`${info.metrics.committedPoints} points`} />
          <Metric
            label="Completed"
            value={`${info.metrics.donePoints} of ${info.metrics.points} points`}
          />
          <Metric label="Items" value={`${info.metrics.done} of ${info.metrics.items} done`} />
          {info.metrics.added > 0 ? (
            <Metric label="Added mid-sprint" value={`${info.metrics.added}`} />
          ) : null}
        </dl>

        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setGoal(info.goal ?? '');
              setEditingGoal(true);
            }}
          >
            Edit goal
          </Button>
          <Button size="sm" variant="outline" onClick={() => setPlanning(true)}>
            Plan sprint
          </Button>
          {info.state === 'planned' ? (
            <Button
              size="sm"
              onClick={() =>
                start.mutate(
                  { id: info.id, rev },
                  {
                    onError: (error) => {
                      if (
                        error instanceof ProviderError &&
                        error.code === 'sprint_already_active'
                      ) {
                        toast({
                          variant: 'destructive',
                          title: 'This board already runs a sprint',
                          description: error.message,
                        });
                        return;
                      }
                      refuse('The sprint could not be started')(error);
                    },
                  },
                )
              }
            >
              Start sprint
            </Button>
          ) : null}
          {info.state === 'active' ? (
            <Button size="sm" onClick={() => setClosing(true)}>
              Close sprint
            </Button>
          ) : null}
          <Button size="sm" variant="ghost" onClick={() => setCreating(true)}>
            New sprint
          </Button>
        </div>
      </CardContent>

      {/* Planning: references move in and out of the sprint file, and nothing else. */}
      <Dialog open={planning} onOpenChange={setPlanning}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Plan {info.title}</DialogTitle>
            <DialogDescription>
              Moving an item in or out of a sprint writes the sprint file in the team repository
              only, so it works for an item whose project nobody cloned.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 sm:grid-cols-2">
            <section aria-label="In the sprint" className="space-y-2">
              <h3 className="text-sm font-medium">
                In the sprint · {info.metrics.points} points
              </h3>
              <ul className="space-y-1 text-sm">
                {(detail?.cards ?? []).map((card) => (
                  <li key={card.ref} className="flex items-center justify-between gap-2">
                    <span>
                      <span className="font-mono text-xs">{card.item}</span>{' '}
                      {card.title ?? card.ref}
                      {card.remote ? (
                        <Badge variant="outline" size="sm" className="ml-1 font-normal">
                          remote
                        </Badge>
                      ) : null}
                    </span>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() =>
                        edit({ removeItems: [card.ref] }, `${card.ref} could not be removed`)
                      }
                    >
                      Remove
                    </Button>
                  </li>
                ))}
              </ul>
            </section>
            <section aria-label="Sprint candidates" className="space-y-2">
              <h3 className="text-sm font-medium">Candidates</h3>
              <ul className="space-y-1 text-sm">
                {(detail?.backlog ?? []).map((card) => (
                  <li key={card.ref} className="flex items-center justify-between gap-2">
                    <span>
                      <span className="font-mono text-xs">{card.item}</span>{' '}
                      {card.title ?? card.ref}
                    </span>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => edit({ addItems: [card.ref] }, `${card.ref} could not be added`)}
                    >
                      Add
                    </Button>
                  </li>
                ))}
                {(detail?.backlog ?? []).length === 0 ? (
                  <li className="text-muted-foreground">
                    Nothing else the board shows is outside this sprint.
                  </li>
                ) : null}
              </ul>
            </section>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setPlanning(false)}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Closing: what was finished, and one explicit decision per unfinished item. */}
      <Dialog open={closing} onOpenChange={setClosing}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Close {info.title}</DialogTitle>
            <DialogDescription>
              {completed.length} of {detail?.cards.length ?? 0} items finished (
              {info.metrics.donePoints} of {info.metrics.points} points). Closing a sprint changes
              no item by itself: choose what happens to each unfinished one.
            </DialogDescription>
          </DialogHeader>
          <ul className="space-y-2 text-sm" aria-label="Unfinished items">
            {incomplete.map((card) => (
              <li key={card.ref} className="flex flex-wrap items-center justify-between gap-2">
                <span>
                  <span className="font-mono text-xs">{card.item}</span> {card.title ?? card.ref}
                </span>
                <Select
                  aria-label={`What happens to ${card.item}`}
                  className="h-8 w-44 text-xs"
                  value={decisions[card.ref] ?? 'leave'}
                  onChange={(event) =>
                    setDecisions({
                      ...decisions,
                      [card.ref]: event.target.value as SprintCarryAction,
                    })
                  }
                >
                  <option value="leave">Leave it here</option>
                  <option value="next">Carry to the next sprint</option>
                  <option value="backlog">Back to the backlog</option>
                </Select>
              </li>
            ))}
            {incomplete.length === 0 ? (
              <li className="text-muted-foreground">Everything in this sprint is finished.</li>
            ) : null}
          </ul>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setClosing(false)}>
              Cancel
            </Button>
            <Button
              onClick={() => {
                const carry: SprintCarry[] = incomplete.map((card) => ({
                  ref: card.ref,
                  action: decisions[card.ref] ?? 'leave',
                }));
                close.mutate(
                  { id: info.id, carry, rev },
                  {
                    onSuccess: (result) => {
                      const failed = (result.report?.carried ?? []).filter((one) => one.error);
                      for (const one of failed) {
                        toast({
                          variant: 'destructive',
                          title: `${one.ref} stayed where it was`,
                          description: one.error ?? '',
                        });
                      }
                      setClosing(false);
                    },
                    onError: refuse('The sprint could not be closed'),
                  },
                );
              }}
            >
              Close sprint
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* A new sprint: the id is allocated by the core, never by the UI. */}
      <NewSprintDialog board={view.id} open={creating} onOpenChange={setCreating} />

    </Card>
  );
}

/** One number of the sprint header. */
function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{value}</dd>
    </div>
  );
}

/** A card sits in a terminal status of its own project. */
function isDone(card: BoardCard): boolean {
  return card.category === 'done' || card.category === 'cancelled';
}
