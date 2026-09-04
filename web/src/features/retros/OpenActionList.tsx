import type { RetroActionView } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

/**
 * The improvement actions a set of retros left open, with the live state of the
 * tasks they were promoted into (docs/04 R-RETRO-1).
 *
 * It is deliberately the same component in the index and at the top of a retro
 * board: what a team owes itself reads the same wherever it is shown.
 */
export function OpenActionList({
  actions,
  title,
  empty,
}: {
  actions: RetroActionView[];
  title: string;
  empty: string;
}) {
  return (
    <Card data-testid="open-actions">
      <CardHeader className="gap-1">
        <CardTitle className="text-base">{title}</CardTitle>
        <CardDescription>
          {actions.length === 0 ? empty : `${actions.length} still open`}
        </CardDescription>
      </CardHeader>
      {actions.length === 0 ? null : (
        <CardContent>
          <ul className="space-y-2 text-sm">
            {actions.map((action) => (
              <li
                key={`${action.retro}-${action.id}`}
                className="flex flex-wrap items-center gap-2"
              >
                <span>{action.title}</span>
                {action.owner ? (
                  <Badge variant="outline" size="sm" className="font-normal">
                    {action.owner}
                  </Badge>
                ) : (
                  <Badge variant="destructive" size="sm" className="font-normal">
                    no owner
                  </Badge>
                )}
                {action.due ? (
                  <span className="text-xs text-muted-foreground">due {action.due}</span>
                ) : null}
                {action.task ? (
                  <span className="text-xs text-muted-foreground">
                    {action.task}
                    {action.card?.status ? ` · ${action.card.status}` : ''}
                  </span>
                ) : null}
                <span className="text-xs text-muted-foreground">from {action.retroTitle}</span>
              </li>
            ))}
          </ul>
        </CardContent>
      )}
    </Card>
  );
}
