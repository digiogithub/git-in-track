import { useId } from 'react';

import { Select } from '@/components/ui/select';
import { useActiveTeam } from '@/features/workspace/active-team';

/**
 * Which team repository the boards, sprints, retros and knowledge base on the
 * screen belong to, and how to switch (story GIT-US-0036).
 *
 * It renders only once there is a choice to make. A workspace holding a single
 * team has none, and a one-option dropdown reads as a broken one; a workspace
 * holding no team at all is told so by the empty state of the page itself,
 * which is also where it is offered a way to create or mount one — two messages
 * saying the same thing is one too many.
 */
export function TeamSelector({ className }: { className?: string }) {
  const { teams, team, select, isPending } = useActiveTeam();
  const selectId = useId();

  if (isPending || teams.length < 2 || team === undefined) return null;

  return (
    <div className={`flex items-center gap-2 ${className ?? ''}`}>
      <label htmlFor={selectId} className="text-sm text-muted-foreground">
        Team
      </label>
      <Select
        id={selectId}
        className="h-8 w-auto"
        value={team.key}
        onChange={(event) => select(event.target.value)}
      >
        {teams.map((candidate) => (
          <option key={candidate.key} value={candidate.key}>
            {candidate.name}
          </option>
        ))}
      </Select>
    </div>
  );
}
