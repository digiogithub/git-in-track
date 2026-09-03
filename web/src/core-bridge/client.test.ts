import { describe, expect, it, vi } from 'vitest';

import { CoreClient, CoreError, type WorkerLike } from './client';
import type { CoreRequest, CoreResponse } from './protocol';

/** A worker stub that answers `ping`/`version` and rejects anything else. */
function createFakeWorker(): { worker: WorkerLike; terminate: ReturnType<typeof vi.fn> } {
  const listeners = new Map<string, ((event: unknown) => void)[]>();
  const terminate = vi.fn();

  const emit = (type: string, event: unknown): void => {
    for (const listener of listeners.get(type) ?? []) listener(event);
  };

  const worker: WorkerLike = {
    postMessage: (message: unknown) => {
      const request = message as CoreRequest;
      const response: CoreResponse =
        request.method === 'ping'
          ? { id: request.id, ok: true, result: { pong: true, wasm: false } }
          : {
              id: request.id,
              ok: false,
              error: { code: 'unknown_method', message: `unknown method "${request.method}"` },
            };
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

  return { worker, terminate };
}

describe('CoreClient', () => {
  it('resolves a call with the worker result', async () => {
    const { worker } = createFakeWorker();
    const client = new CoreClient({ createWorker: () => worker });

    await expect(client.ping()).resolves.toEqual({ pong: true, wasm: false });
  });

  it('rejects with a typed CoreError when the worker reports a failure', async () => {
    const { worker } = createFakeWorker();
    const client = new CoreClient({ createWorker: () => worker });

    await expect(client.call('query')).rejects.toBeInstanceOf(CoreError);
    await expect(client.call('query')).rejects.toMatchObject({ code: 'unknown_method' });
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
