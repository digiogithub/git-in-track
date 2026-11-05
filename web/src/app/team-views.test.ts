import { describe, expect, it } from 'vitest';

import { isTeamViewPath } from '@/app/team-views';

describe('isTeamViewPath (GIT-US-0204)', () => {
  it.each([
    '/boards',
    '/boards/main',
    '/sprints',
    '/retros',
    '/retros/ACME-TEAM-RT-0001',
    '/metrics',
    '/metrics/ACME-TEAM-SP-0002',
  ])('treats %s as a team view', (path) => {
    expect(isTeamViewPath(path)).toBe(true);
  });

  it.each(['/', '/settings', '/sync', '/p/ACME/items', '/boardsx', '/metrics-old'])(
    'leaves %s alone',
    (path) => {
      expect(isTeamViewPath(path)).toBe(false);
    },
  );
});
