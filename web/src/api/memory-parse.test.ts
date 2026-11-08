/**
 * The companion's wire shape of a memory item and of a knowledge-only project
 * (ADR-044, GIT-US-0216). Before memory existed, an unknown type made toItem
 * throw and a memory proposal broke the whole inbox page.
 */

import { describe, expect, it } from 'vitest';

import { toItem, toProjectSummary } from '@/api/companion-provider';

describe('memory on the wire', () => {
  it('parses a memory item and its block', () => {
    const item = toItem({
      id: 'ACME-MEM-0001',
      type: 'memory',
      title: 'Customer X is invoiced quarterly',
      status: 'proposed',
      rev: 'sha256:1',
      memory: {
        key: 'customer-x',
        version: 4,
        localId: 'project/acme/customer-x',
        source: { kind: 'agent', ref: 'memsync' },
      },
    });
    expect(item.type).toBe('memory');
    expect(item.memory).toEqual({
      key: 'customer-x',
      version: 4,
      localId: 'project/acme/customer-x',
      source: { kind: 'agent', ref: 'memsync' },
    });
  });

  it('reads the kind of a knowledge-only project, and nothing else as a kind', () => {
    expect(toProjectSummary({ key: 'ACME', kind: 'knowledge' }).kind).toBe('knowledge');
    expect(toProjectSummary({ key: 'ACME', kind: 'archive' }).kind).toBeUndefined();
    expect(toProjectSummary({ key: 'ACME' }).kind).toBeUndefined();
  });
});
