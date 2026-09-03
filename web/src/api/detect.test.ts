import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useAppStore } from '@/app/store';

import { detectMode, HEALTH_PATH, probeCompanion, PROBE_TIMEOUT_MS } from './detect';

function jsonResponse(body: unknown, init: { ok?: boolean; status?: number } = {}): Response {
  const { ok = true, status = 200 } = init;
  return {
    ok,
    status,
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

describe('probeCompanion', () => {
  beforeEach(() => {
    useAppStore.getState().reset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns the health document when the companion answers', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse({ status: 'ok', version: '0.4.0', uptimeSeconds: 8123 }));

    const health = await probeCompanion({ baseUrl: 'http://127.0.0.1:7317', fetchImpl });

    expect(health).toEqual({ status: 'ok', version: '0.4.0', uptimeSeconds: 8123 });
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    const [url, init] = fetchImpl.mock.calls[0] ?? [];
    expect(url).toBe(`http://127.0.0.1:7317${HEALTH_PATH}`);
    expect(init?.mode).toBe('cors');
    expect(init?.signal).toBeInstanceOf(AbortSignal);
  });

  it('returns null when the request fails', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch'));

    await expect(probeCompanion({ fetchImpl })).resolves.toBeNull();
  });

  it('returns null on a non-2xx answer', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse({}, { ok: false, status: 503 }));

    await expect(probeCompanion({ fetchImpl })).resolves.toBeNull();
  });

  it('returns null when the body is not a health document', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({ hello: 'world' }));

    await expect(probeCompanion({ fetchImpl })).resolves.toBeNull();
  });

  it('aborts the request after the timeout', async () => {
    vi.useFakeTimers();

    const fetchImpl = vi.fn<typeof fetch>().mockImplementation(
      (_input, init) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => {
            reject(new DOMException('Aborted', 'AbortError'));
          });
        }),
    );

    const pending = probeCompanion({ fetchImpl });
    await vi.advanceTimersByTimeAsync(PROBE_TIMEOUT_MS + 1);

    await expect(pending).resolves.toBeNull();
  });
});

describe('detectMode', () => {
  beforeEach(() => {
    useAppStore.getState().reset();
  });

  it('switches the store to companion mode on a successful probe', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse({ status: 'ok', version: '0.4.0', uptimeSeconds: 1 }));

    await expect(detectMode({ fetchImpl })).resolves.toBe('companion');
    expect(useAppStore.getState().mode).toBe('companion');
    expect(useAppStore.getState().companionVersion).toBe('0.4.0');
  });

  it('falls back to browser mode when no companion answers', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch'));

    await expect(detectMode({ fetchImpl })).resolves.toBe('browser');
    expect(useAppStore.getState().mode).toBe('browser');
    expect(useAppStore.getState().companionVersion).toBeNull();
  });
});
