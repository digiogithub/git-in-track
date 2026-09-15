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
});
