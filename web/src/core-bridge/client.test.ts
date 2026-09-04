import { describe, expect, it, vi } from 'vitest';

import type { IndexStats, VaultFile } from './api';
import { chunkFiles, CoreClient, CoreError, type WorkerLike } from './client';
import type { CoreRequest, CoreResponse } from './protocol';

type Answer = (request: CoreRequest) => CoreResponse;

/** The default answers: `ping` succeeds, everything else is an unknown method. */
const defaultAnswer: Answer = (request) =>
  request.method === 'ping'
    ? { id: request.id, ok: true, result: { pong: true, wasm: false } }
    : {
        id: request.id,
        ok: false,
        error: { code: 'unknown_method', message: `unknown method "${request.method}"` },
      };

/** A worker stub that records every request and replies with `answer`. */
function createFakeWorker(answer: Answer = defaultAnswer): {
  worker: WorkerLike;
  terminate: ReturnType<typeof vi.fn>;
  requests: CoreRequest[];
} {
  const listeners = new Map<string, ((event: unknown) => void)[]>();
  const terminate = vi.fn();
  const requests: CoreRequest[] = [];

  const emit = (type: string, event: unknown): void => {
    for (const listener of listeners.get(type) ?? []) listener(event);
  };

  const worker: WorkerLike = {
    postMessage: (message: unknown) => {
      const request = message as CoreRequest;
      requests.push(request);
      const response = answer(request);
      queueMicrotask(() => {
        emit('message', { data: response });
      });
    },
    terminate,
    addEventListener: ((type: string, listener: (event: unknown) => void) => {
      const existing = listeners.get(type) ?? [];
      existing.push(listener);
      listeners.set(type, existing);
    }) as WorkerLike['addEventListener'],
  };

  return { worker, terminate, requests };
}

/** An IndexStats with the fields the client's own code reads. */
function stats(fingerprint = 'fp'): IndexStats {
  return {
    projects: 1,
    items: 0,
    pages: 0,
    comments: 0,
    durationMs: 0,
    fingerprint,
    diagnostics: [],
  };
}

describe('CoreClient', () => {
  it('resolves a call with the worker result', async () => {
    const { worker } = createFakeWorker();
    const client = new CoreClient({ createWorker: () => worker });

    await expect(client.ping()).resolves.toEqual({ pong: true, wasm: false });
  });

  it('sends params only when the method takes them', async () => {
    const { worker, requests } = createFakeWorker((request) => ({
      id: request.id,
      ok: true,
      result: { items: [], total: 0 },
    }));
    const client = new CoreClient({ createWorker: () => worker });

    await client.call('vault.stats');
    await client.listItems({ type: 'story', limit: 10 });

    expect(requests[0]).toEqual({ id: 1, method: 'vault.stats' });
    expect(requests[1]).toEqual({
      id: 2,
      method: 'item.list',
      params: { type: 'story', limit: 10 },
    });
  });

  it('rejects with a typed CoreError when the worker reports a failure', async () => {
    const { worker } = createFakeWorker();
    const client = new CoreClient({ createWorker: () => worker });

    await expect(client.call('item.get', { id: 'X-US-0001' })).rejects.toBeInstanceOf(CoreError);
    await expect(client.call('item.get', { id: 'X-US-0001' })).rejects.toMatchObject({
      code: 'unknown_method',
    });
  });

  it('carries the failing path on the error', async () => {
    const { worker } = createFakeWorker((request) => ({
      id: request.id,
      ok: false,
      error: {
        code: 'stale_revision',
        message: 'rev mismatch',
        path: 'docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md',
      },
    }));
    const client = new CoreClient({ createWorker: () => worker });

    await expect(
      client.updateItem('DEMO-US-0001', { set: { title: 'x' } }, 'sha256:stale'),
    ).rejects.toMatchObject({
      code: 'stale_revision',
      path: 'docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md',
    });
  });

  it('spawns the worker once and terminates it on dispose', async () => {
    const { worker, terminate } = createFakeWorker();
    const createWorker = vi.fn(() => worker);
    const client = new CoreClient({ createWorker });

    await client.ping();
    await client.ping();
    expect(createWorker).toHaveBeenCalledTimes(1);

    client.dispose();
    expect(terminate).toHaveBeenCalledTimes(1);
  });

  it('rejects in-flight calls when the worker is disposed', async () => {
    const worker: WorkerLike = {
      postMessage: () => undefined,
      terminate: vi.fn(),
      addEventListener: (() => undefined) as WorkerLike['addEventListener'],
    };
    const client = new CoreClient({ createWorker: () => worker });

    const pending = client.call('ping');
    client.dispose();

    await expect(pending).rejects.toMatchObject({ code: 'worker_crashed' });
  });
});

describe('CoreClient.loadVault', () => {
  const files: VaultFile[] = [
    { path: 'docs/.pmngr/project.yaml', text: 'schema: 1\nkey: DEMO\n' },
    { path: 'docs/index.md', text: 'a'.repeat(10) },
    { path: 'docs/architecture/overview.md', text: 'b'.repeat(10) },
  ];

  it('pushes a small vault in one message', async () => {
    const { worker, requests } = createFakeWorker((request) => ({
      id: request.id,
      ok: true,
      result: stats(),
    }));
    const client = new CoreClient({ createWorker: () => worker });

    const result = await client.loadVault(files, { rootLabel: 'demo' });

    expect(result.fingerprint).toBe('fp');
    expect(requests).toHaveLength(1);
    expect(requests[0]?.method).toBe('vault.load');
    expect(requests[0]?.params).toEqual({ files, rootLabel: 'demo' });
  });

  it('splits a large vault into vault.apply batches', async () => {
    const { worker, requests } = createFakeWorker((request) => ({
      id: request.id,
      ok: true,
      result: stats(`fp-${request.id}`),
    }));
    const client = new CoreClient({ createWorker: () => worker, chunkBytes: 12 });
    const progress: number[] = [];

    const result = await client.loadVault(files, {
      onProgress: (p) => progress.push(p.files),
    });

    expect(requests.map((r) => r.method)).toEqual(['vault.load', 'vault.apply', 'vault.apply']);
    // The project descriptor always travels in the first message: the core
    // cannot classify any other file before it knows where the projects are.
    expect(requests[0]?.params).toEqual({ files: [files[0]] });
    expect(requests[1]?.params).toEqual({
      events: [{ op: 'create', path: 'docs/index.md', text: files[1]?.text }],
    });
    expect(progress).toEqual([1, 2, 3]);
    // The stats of the last message win: they describe the whole vault.
    expect(result.fingerprint).toBe('fp-3');
  });

  it('loads an empty vault without failing', async () => {
    const { worker, requests } = createFakeWorker((request) => ({
      id: request.id,
      ok: true,
      result: stats(),
    }));
    const client = new CoreClient({ createWorker: () => worker });

    await client.loadVault([]);

    expect(requests).toHaveLength(1);
    expect(requests[0]?.params).toEqual({ files: [] });
  });
});

describe('chunkFiles', () => {
  it('keeps every project descriptor in the first batch', () => {
    const batches = chunkFiles(
      [
        { path: 'a/docs/index.md', text: 'x'.repeat(100) },
        { path: 'a/docs/.pmngr/project.yaml', text: 'schema: 1\n' },
        { path: 'b/docs/.pmngr/project.yaml', text: 'schema: 1\n' },
      ],
      10,
      10,
    );

    expect(batches[0]?.map((f) => f.path)).toEqual([
      'a/docs/.pmngr/project.yaml',
      'b/docs/.pmngr/project.yaml',
    ]);
    expect(batches[1]?.map((f) => f.path)).toEqual(['a/docs/index.md']);
  });

  it('respects the file-count cap', () => {
    const files = Array.from({ length: 5 }, (_, i) => ({ path: `p${i}.md`, text: '.' }));

    const batches = chunkFiles(files, 1024, 2);

    expect(batches.map((b) => b.length)).toEqual([2, 2, 1]);
  });

  it('never returns an empty batch list', () => {
    expect(chunkFiles([])).toEqual([[]]);
  });
});
