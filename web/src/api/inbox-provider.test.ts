/**
 * The inbox half of the provider boundary (story GIT-US-0060, task GIT-T-0051).
 *
 * These tests are about the contract rather than about a screen: what a triage
 * listing counts, what each of the four decisions writes, and the two refusals
 * a surface has to render — a project with no triage status, and a revision
 * that moved on.
 */

import { describe, expect, it } from 'vitest';

import {
  FakeProvider,
  sampleInboxItems,
  sampleItems,
  sampleProject,
  sampleTriageProject,
} from '@/api/fake-provider';
import { ProviderError, type ChangeEvent } from '@/api/provider';

function provider() {
  return new FakeProvider({
    projects: [sampleTriageProject],
    items: [...sampleItems, ...sampleInboxItems],
    today: '2026-09-02',
  });
}

describe('listInbox', () => {
  it('counts the whole queue, not the page, and counts an expired snooze as pending', async () => {
    const page = await provider().listInbox({ project: 'ACME', limit: 1 });

    // ACME-US-0200 is pending and ACME-US-0201 was snoozed until a date that
    // has passed, so two items are waiting even though only one is on the page.
    expect(page.items).toHaveLength(1);
    expect(page.total).toBe(4);
    expect(page.pending).toBe(2);
    expect(page.counts).toEqual({ pending: 2, snoozed: 1, rejected: 1 });
    expect(page.nextCursor).toBe('1');
  });

  it('lists an item whose snooze has passed under the pending filter', async () => {
    const page = await provider().listInbox({ project: 'ACME', status: ['pending'] });

    expect(page.items.map((item) => item.id)).toEqual(['ACME-US-0200', 'ACME-US-0201']);
  });

  it('keeps a snooze that has not arrived out of the pending filter', async () => {
    const page = await provider().listInbox({ project: 'ACME', status: ['snoozed'] });

    expect(page.items.map((item) => item.id)).toEqual(['ACME-T-0202']);
  });

  it('pages through the queue with the cursor it handed back', async () => {
    const store = provider();
    const first = await store.listInbox({ project: 'ACME', limit: 2 });
    const second = await store.listInbox({
      project: 'ACME',
      limit: 2,
      cursor: first.nextCursor ?? '',
    });

    expect(second.items).toHaveLength(2);
    expect(second.nextCursor).toBeUndefined();
    expect([...first.items, ...second.items].map((item) => item.id)).toHaveLength(4);
  });

  it('refuses a project that declares no triage status, which is a state not an error', async () => {
    const store = new FakeProvider({ projects: [sampleProject], items: sampleItems });

    await expect(store.listInbox({ project: 'ACME' })).rejects.toMatchObject({
      code: 'no_triage_status',
    });
  });
});

describe('createInboxItem', () => {
  it('files a draft into the queue as pending, with its source', async () => {
    const store = provider();
    const item = await store.createInboxItem({
      project: 'ACME',
      type: 'story',
      title: 'Single sign-out',
      source: 'web',
    });

    expect(item.status).toBe('triage');
    expect(item.inbox?.status).toBe('pending');
    expect(item.inbox?.source).toBe('web');
    const page = await store.listInbox({ project: 'ACME' });
    expect(page.pending).toBe(3);
  });
});

describe('triageInboxItem', () => {
  it('accepts into the workflow initial status and leaves the type alone', async () => {
    const store = provider();
    const result = await store.triageInboxItem({
      id: 'ACME-US-0200',
      rev: 'sha256:0000000000000201',
      action: 'accept',
      parent: 'ACME-EP-0001',
    });

    expect(result.item.status).toBe('backlog');
    expect(result.item.parent).toBe('ACME-EP-0001');
    expect(result.item.inbox).toBeUndefined();
    // An item id encodes its type for life (R-ID-3): accepting never moves it.
    expect(result.item.type).toBe('story');
    expect(result.pending).toBe(1);
  });

  it('accepts into an explicit status when one is chosen', async () => {
    const result = await provider().triageInboxItem({
      id: 'ACME-US-0200',
      rev: '*',
      action: 'accept',
      status: 'todo',
    });

    expect(result.item.status).toBe('todo');
  });

  it('rejects without deleting anything', async () => {
    const store = provider();
    const result = await store.triageInboxItem({ id: 'ACME-US-0200', rev: '*', action: 'reject' });

    expect(result.item.inbox?.status).toBe('rejected');
    expect(result.item.deleted).toBeUndefined();
    expect(result.pending).toBe(1);
  });

  it('snoozes until a date and refuses a snooze without one', async () => {
    const store = provider();
    const result = await store.triageInboxItem({
      id: 'ACME-US-0200',
      rev: '*',
      action: 'snooze',
      snoozedUntil: '2026-10-15',
    });

    expect(result.item.inbox).toMatchObject({ status: 'snoozed', snoozedUntil: '2026-10-15' });
    await expect(
      store.triageInboxItem({ id: 'ACME-US-0201', rev: '*', action: 'snooze' }),
    ).rejects.toBeInstanceOf(ProviderError);
  });

  it('marks a duplicate and records the link to the item it repeats', async () => {
    const result = await provider().triageInboxItem({
      id: 'ACME-US-0200',
      rev: '*',
      action: 'duplicate',
      duplicateOf: 'ACME-US-0042',
    });

    expect(result.item.inbox).toMatchObject({ status: 'duplicate', duplicateOf: 'ACME-US-0042' });
    expect(result.item.links).toContainEqual({ kind: 'duplicates', target: 'ACME-US-0042' });
  });

  it('refuses a decision taken on a revision that has moved on', async () => {
    await expect(
      provider().triageInboxItem({ id: 'ACME-US-0200', rev: 'sha256:stale', action: 'reject' }),
    ).rejects.toMatchObject({ code: 'stale_revision' });
  });

  it('announces the decision with the queue that is left behind it', async () => {
    const store = provider();
    const seen: ChangeEvent[] = [];
    store.subscribe((event) => seen.push(event));

    await store.triageInboxItem({ id: 'ACME-US-0200', rev: '*', action: 'reject' });

    expect(seen).toContainEqual({
      kind: 'inbox',
      repoId: 'repo-1',
      project: 'ACME',
      id: 'ACME-US-0200',
      action: 'reject',
      pending: 1,
    });
  });
});
