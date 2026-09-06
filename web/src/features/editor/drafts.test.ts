import { beforeEach, describe, expect, it } from 'vitest';

import { clearDraft, draftKey, readDraft, writeDraft } from '@/features/editor/drafts';

const values = { title: 'Login with SSO', status: 'todo' } as never;

describe('editor drafts', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('scopes the key by workspace, project and item', () => {
    expect(draftKey(null, 'ACME', 'ACME-US-0042')).toBe('gintrack:draft:browser:ACME:ACME-US-0042');
    expect(draftKey('http://127.0.0.1:7317', 'ACME', 'ACME-US-0042')).toBe(
      'gintrack:draft:http://127.0.0.1:7317:ACME:ACME-US-0042',
    );
    // Two workspaces, and two items, never read each other's drafts.
    expect(draftKey(null, 'ACME', 'ACME-US-0042')).not.toBe(draftKey(null, 'ACME', 'ACME-US-0043'));
  });

  it('round-trips a draft and stamps when it was written', () => {
    const key = draftKey(null, 'ACME', 'ACME-US-0042');
    writeDraft(key, { rev: 'sha256:42', values, body: 'half-written' });

    const draft = readDraft(key);
    expect(draft?.body).toBe('half-written');
    expect(draft?.rev).toBe('sha256:42');
    expect(Number.isNaN(Date.parse(draft?.savedAt ?? ''))).toBe(false);
  });

  it('clears a draft', () => {
    const key = draftKey(null, 'ACME', 'ACME-US-0042');
    writeDraft(key, { rev: 'sha256:42', values, body: 'gone' });
    clearDraft(key);
    expect(readDraft(key)).toBeNull();
  });

  it('drops a draft that is not readable instead of failing', () => {
    const key = draftKey(null, 'ACME', 'ACME-US-0042');
    localStorage.setItem(key, 'not json at all');
    expect(readDraft(key)).toBeNull();
    expect(localStorage.getItem(key)).toBeNull();

    localStorage.setItem(key, JSON.stringify({ body: 'no rev, no values' }));
    expect(readDraft(key)).toBeNull();
  });

  it('drops a draft older than a month', () => {
    const key = draftKey(null, 'ACME', 'ACME-US-0042');
    localStorage.setItem(
      key,
      JSON.stringify({
        rev: 'sha256:42',
        values,
        body: 'ancient',
        savedAt: new Date(Date.now() - 40 * 24 * 60 * 60 * 1000).toISOString(),
      }),
    );
    expect(readDraft(key)).toBeNull();
    expect(localStorage.getItem(key)).toBeNull();
  });

  it('reads as "no draft" when storage is unavailable', () => {
    const key = draftKey(null, 'ACME', 'ACME-US-0042');
    const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get() {
        throw new Error('storage is disabled');
      },
    });
    try {
      expect(readDraft(key)).toBeNull();
      // Writing must not throw either: the buffer in memory still saves.
      expect(() => {
        writeDraft(key, { rev: 'sha256:42', values, body: 'x' });
      }).not.toThrow();
    } finally {
      if (original) Object.defineProperty(globalThis, 'localStorage', original);
    }
  });
});
