import { describe, expect, it } from 'vitest';

import { routeTree } from '@/app/router';

type TreeNode = { options?: { path?: string }; children?: readonly TreeNode[] };

/** Every path in the tree, so a route cannot quietly go missing. */
function paths(route: TreeNode): string[] {
  return [route.options?.path ?? '', ...(route.children ?? []).flatMap(paths)];
}

describe('the route tree', () => {
  it('registers /agent exactly once (story GIT-US-0057)', () => {
    const agent = paths(routeTree as unknown as TreeNode).filter((path) => path === '/agent');

    expect(agent).toHaveLength(1);
  });

  it('registers the specs page under the project (story GIT-US-0128)', () => {
    expect(paths(routeTree as unknown as TreeNode)).toContain('specs');
  });

  it('registers the coverage matrix under the project (story GIT-US-0130)', () => {
    expect(paths(routeTree as unknown as TreeNode)).toContain('specs/coverage');
  });
});
