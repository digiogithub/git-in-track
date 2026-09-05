/**
 * Team-repository detection for the add-repository wizard (docs/05-web-app.md
 * §3.1, story GIT-US-0035).
 *
 * A folder is a team repository when it holds a `team.yaml` at its root, and
 * only then: that file is the discovery marker of R-TEAM-LOC-1 and the routing
 * table every board, sprint and retro reads. The role recorded on a mount is
 * reported, never enforced, so registering a folder without one produces a
 * repository whose team surfaces all fail — which is what this detection
 * exists to prevent.
 */

import type { VaultFile } from '@/core-bridge/api';

export const TEAM_FILE = 'team.yaml';

export type TeamCandidate = {
  /** Path of the `team.yaml` that was found; always the repository root. */
  teamFile: string;
  /** `key:` read from `team.yaml`, when the line is a plain scalar. */
  teamKey?: string;
  /** `name:` read from `team.yaml`. */
  teamName?: string;
};

function scalar(text: string, field: string): string | undefined {
  const match = new RegExp(`^${field}:\\s*["']?([^"'#\\n]+?)["']?\\s*$`, 'm').exec(text);
  return match?.[1]?.trim();
}

/**
 * Finds the root `team.yaml` of the scanned files, or `null` when the folder is
 * not a team repository. A `team.yaml` deeper in the tree is deliberately
 * ignored: a team repository is a repository, not a folder inside one.
 */
export function detectTeam(files: VaultFile[]): TeamCandidate | null {
  const found = files.find((file) => file.path === TEAM_FILE);
  if (!found) return null;
  const key = scalar(found.text, 'key');
  const name = scalar(found.text, 'name');
  return {
    teamFile: found.path,
    ...(key ? { teamKey: key } : {}),
    ...(name ? { teamName: name } : {}),
  };
}

/** The team-key grammar of docs/04-team-repository.md §3.1. */
export const TEAM_KEY = /^[A-Z][A-Z0-9-]{1,15}$/;
