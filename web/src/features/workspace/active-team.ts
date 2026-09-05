import { useQuery } from '@tanstack/react-query';
import { useEffect } from 'react';

import type { TeamSummary } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { readActiveTeam, useAppStore } from '@/app/store';

/**
 * The active team of the workspace (story GIT-US-0036, ADR-019).
 *
 * A workspace may hold several team repositories, and boards, sprints, retros
 * and the team knowledge base all resolve through one of them. Which one is a
 * client-side choice: it is remembered per workspace in `localStorage` and sent
 * as `team` on every team-scoped call, so the companion stays stateless and
 * browser-only mode behaves identically.
 */

/** Key factory. Every team key lives under the `teams` prefix. */
export const teamKeys = {
  all: () => ['teams'] as const,
  list: () => ['teams', 'list'] as const,
  detail: (team = '') => ['teams', 'detail', team] as const,
};

/** Every open team repository, in mount order. */
export function useTeams() {
  const provider = useProvider();
  return useQuery<TeamSummary[]>({
    queryKey: teamKeys.list(),
    queryFn: () => provider.listTeams(),
  });
}

export type ActiveTeam = {
  /** Every open team, in mount order. */
  teams: TeamSummary[];
  /** The team every team-scoped call is made against, when there is one. */
  team: TeamSummary | undefined;
  /**
   * The key to pass as `team`. It is `undefined` while no team is open, and
   * while the teams are still loading, which is what keeps a team-scoped query
   * from firing against the wrong team on the first render.
   */
  teamKey: string | undefined;
  /** Chooses a team and remembers the choice for this workspace. */
  select: (teamKey: string) => void;
  isPending: boolean;
};

/**
 * Resolves the active team: the one remembered for this workspace when it is
 * still open, and otherwise the first one. A remembered team that has been
 * unmounted is not an error — the workspace simply falls back and the selector
 * shows which team answered.
 */
export function useActiveTeam(): ActiveTeam {
  const query = useTeams();
  const companionUrl = useAppStore((state) => state.companionUrl);
  const chosen = useAppStore((state) => state.activeTeamKey);
  const setActiveTeam = useAppStore((state) => state.setActiveTeam);

  // Hydrate the choice made in an earlier session, once, before anything reads
  // it. Writing it back through the store is idempotent.
  useEffect(() => {
    if (chosen !== null) return;
    const remembered = readActiveTeam(companionUrl);
    if (remembered !== null) setActiveTeam(remembered);
  }, [chosen, companionUrl, setActiveTeam]);

  const teams = query.data ?? [];
  const team = teams.find((candidate) => candidate.key === chosen) ?? teams[0];

  return {
    teams,
    team,
    ...(team === undefined ? { teamKey: undefined } : { teamKey: team.key }),
    select: setActiveTeam,
    isPending: query.isPending,
  };
}

/** The active team key, for a query key or a team-scoped provider call. */
export function useActiveTeamKey(): string | undefined {
  return useActiveTeam().teamKey;
}
