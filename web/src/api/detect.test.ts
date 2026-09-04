import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useAppStore } from '@/app/store';

import {
  COMPANION_ORIGIN,
  detectMode,
  HEALTH_PATH,
  probeCompanion,
  probeCompanionNow,
  PROBE_TIMEOUT_MS,
  resolveCompanionBaseUrl,
  watchCompanion,
} from './detect';

const health = { status: 'ok', version: '0.4.0', uptimeSeconds: 1 };

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

describe('resolveCompanionBaseUrl', () => {
  it('points at the documented loopback origin off the embedded server', () => {
    // jsdom is not the companion origin, and the suite runs in dev mode.
    expect(resolveCompanionBaseUrl()).toBe(COMPANION_ORIGIN);
  });
});

describe('watchCompanion', () => {
  beforeEach(() => {
    useAppStore.getState().reset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('upgrades a running tab when a later probe finds the companion', async () => {
    vi.useFakeTimers();
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValue(jsonResponse(health));

    await expect(detectMode({ fetchImpl })).resolves.toBe('browser');

    const onChange = vi.fn();
    const stop = watchCompanion({ fetchImpl, intervalMs: 1_000, onChange });

    await vi.advanceTimersByTimeAsync(1_000);

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({
      mode: 'companion',
      version: '0.4.0',
      baseUrl: COMPANION_ORIGIN,
    });
    expect(useAppStore.getState().mode).toBe('companion');
    expect(useAppStore.getState().companionVersion).toBe('0.4.0');
    expect(useAppStore.getState().companionUrl).toBe(COMPANION_ORIGIN);

    // A second confirming probe is not a flip: the UI is not notified again.
    await vi.advanceTimersByTimeAsync(1_000);
    expect(onChange).toHaveBeenCalledTimes(1);

    stop();
  });

  it('downgrades when the companion goes away', async () => {
    vi.useFakeTimers();
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse(health))
      .mockRejectedValue(new TypeError('Failed to fetch'));

    await expect(detectMode({ fetchImpl })).resolves.toBe('companion');

    const onChange = vi.fn();
    const stop = watchCompanion({ fetchImpl, intervalMs: 1_000, onChange });

    await vi.advanceTimersByTimeAsync(1_000);

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ mode: 'browser', version: null }),
    );
    expect(useAppStore.getState().mode).toBe('browser');
    expect(useAppStore.getState().companionUrl).toBeNull();

    stop();
  });

  it('stops probing once cancelled', async () => {
    vi.useFakeTimers();
    const fetchImpl = vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch'));

    const stop = watchCompanion({ fetchImpl, intervalMs: 1_000 });
    stop();

    await vi.advanceTimersByTimeAsync(5_000);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it('probes on demand for the "Check again" button', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse(health));
    const onChange = vi.fn();
    const stop = watchCompanion({ fetchImpl, intervalMs: 60_000, onChange });

    await probeCompanionNow();

    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(useAppStore.getState().mode).toBe('companion');
    stop();
  });
});
