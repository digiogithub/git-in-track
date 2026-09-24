import { beforeEach, describe, expect, it, vi } from 'vitest';

import { CompanionProvider, companionCapabilities } from '@/api/companion-provider';
import { FakeProvider } from '@/api/fake-provider';
import type { ChangeEvent, Item, Requirement } from '@/api/provider';
import { resetTokenCache } from '@/api/token';

/**
 * The spec surface of the provider boundary (GIT-US-0127): the companion maps
 * each call onto `/api/v1/projects/{key}/specs`, and the fake mirrors the
 * browser — requirements work, trace, coverage and impact answer `unavailable`
 * — unless a test scripts the companion answers.
 */

const BASE = 'http://127.0.0.1:7317';
const SPECS = `${BASE}/api/v1/projects/ACME/specs`;

function response(
  body: unknown,
  init: { status?: number; headers?: Record<string, string> } = {},
): Response {
  const { status = 200, headers = {} } = init;
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? 'OK' : 'Error',
    headers: { get: (name: string) => headers[name] ?? null },
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

function companion(fetchImpl: ReturnType<typeof vi.fn>): CompanionProvider {
  return new CompanionProvider({
    baseUrl: BASE,
    fetchImpl: fetchImpl as unknown as typeof fetch,
    capabilities: companionCapabilities,
    webSocketFactory: null,
  });
}

function lastCall(fetchImpl: ReturnType<typeof vi.fn>): { url: string; init: RequestInit } {
  const call = fetchImpl.mock.calls.at(-1);
  return { url: String(call?.[0]), init: (call?.[1] ?? {}) as RequestInit };
}

const requirement: Requirement = {
  ref: 'ACME-SP-0001.R1',
  spec: 'ACME-SP-0001',
  path: 'docs/.pmngr/specs/ACME-SP-0001-checkout.md',
  anchor: 'acme-sp-0001-r1',
  line: 7,
  title: 'Trim input',
  status: 'backlog',
  rev: 'sha256:00000000000000r1',
  blockRev: 'sha256:00000000000000b1',
};

const spec: Item = {
  id: 'ACME-SP-0001',
  type: 'spec',
  title: 'Checkout',
  status: 'draft',
  body: '',
  path: requirement.path,
  rev: 'sha256:00000000000000s1',
};

beforeEach(() => {
  resetTokenCache();
  globalThis.sessionStorage?.clear();
});

describe('CompanionProvider specs', () => {
  it('lists and reads specs under the project', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(
        response({ items: [spec], total: 1 }, { headers: { 'X-Total-Count': '1' } }),
      )
      .mockResolvedValueOnce(response(spec));
    const provider = companion(fetchImpl);

    await expect(provider.listSpecs('ACME', { status: 'draft', limit: 10 })).resolves.toMatchObject(
      {
        items: [{ id: 'ACME-SP-0001', type: 'spec' }],
        total: 1,
      },
    );
    expect(lastCall(fetchImpl).url).toBe(`${SPECS}?status=draft&limit=10`);

    await expect(provider.getSpec('ACME', 'ACME-SP-0001')).resolves.toMatchObject({
      id: 'ACME-SP-0001',
    });
    expect(lastCall(fetchImpl).url).toBe(`${SPECS}/ACME-SP-0001`);
  });

  it('lists requirements of the project or of one spec', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(response({ requirements: [requirement], total: 1 }));
    const provider = companion(fetchImpl);

    await expect(
      provider.listRequirements('ACME', { status: ['todo', 'done'], q: 'trim' }),
    ).resolves.toEqual({
      requirements: [requirement],
      total: 1,
    });
    expect(lastCall(fetchImpl).url).toBe(`${SPECS}/requirements?status=todo&status=done&q=trim`);

    await provider.listRequirements('ACME', { spec: 'ACME-SP-0001', text: true });
    expect(lastCall(fetchImpl).url).toBe(`${SPECS}/ACME-SP-0001/requirements?text=true`);
  });

  it('reads, creates and updates one requirement with its rev in If-Match', async () => {
    const write = { requirement, specRev: 'sha256:00000000000000s2' };
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(response({ requirement, specRev: spec.rev }))
      .mockResolvedValueOnce(response(write, { status: 201 }))
      .mockResolvedValueOnce(response(write));
    const provider = companion(fetchImpl);

    await expect(provider.getRequirement('ACME', 'ACME/ACME-SP-0001.R1')).resolves.toEqual({
      requirement,
      specRev: spec.rev,
    });
    expect(lastCall(fetchImpl).url).toBe(`${SPECS}/ACME-SP-0001/requirements/R1`);

    await expect(
      provider.createRequirement('ACME', { spec: 'ACME-SP-0001', title: 'Keep the postcode' }),
    ).resolves.toEqual(write);
    let call = lastCall(fetchImpl);
    expect(call.url).toBe(`${SPECS}/ACME-SP-0001/requirements`);
    expect(call.init.method).toBe('POST');
    expect(JSON.parse(call.init.body as string)).toEqual({ title: 'Keep the postcode' });

    await provider.updateRequirement(
      'ACME',
      'ACME-SP-0001.R1',
      { status: 'todo' },
      requirement.rev,
    );
    call = lastCall(fetchImpl);
    expect(call.url).toBe(`${SPECS}/ACME-SP-0001/requirements/R1`);
    expect(call.init.method).toBe('PATCH');
    expect((call.init.headers as Record<string, string>)['If-Match']).toBe(requirement.rev);
    expect(JSON.parse(call.init.body as string)).toEqual({ patch: { status: 'todo' } });
  });

  it('maps a stale requirement rev onto stale_revision', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        response(
          { code: 'stale_revision', status: 412, detail: 'moved', currentRev: 'sha256:new' },
          { status: 412 },
        ),
      );

    await expect(
      companion(fetchImpl).updateRequirement(
        'ACME',
        'ACME-SP-0001.R1',
        { title: 'x' },
        'sha256:old',
      ),
    ).rejects.toMatchObject({ name: 'ProviderError', code: 'stale_revision' });
  });

  it('reads trace, coverage and impact with their query strings', async () => {
    const trace = { ref: requirement.ref, code: [], tests: [], work: [] };
    const coverage = { coverage: [{ ref: requirement.ref, status: 'untested' }], total: 1 };
    const impact = { base: 'main', files: 1, symbols: 0, tiers: [], hits: [] };
    const report = { base: 'main', files: 1, symbols: 0, total: 0, budget: 500, tokens: 20 };
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(response({ trace }))
      .mockResolvedValueOnce(response(coverage))
      .mockResolvedValueOnce(response({ impact }))
      .mockResolvedValueOnce(response({ report }));
    const provider = companion(fetchImpl);

    await expect(provider.traceRequirement('ACME', 'ACME-SP-0001.R1')).resolves.toEqual(trace);
    expect(lastCall(fetchImpl).url).toBe(`${SPECS}/ACME-SP-0001/requirements/R1/trace`);

    await expect(
      provider.listCoverage('ACME', { spec: 'ACME-SP-0001', status: ['failing', 'suspect'] }),
    ).resolves.toEqual(coverage);
    expect(lastCall(fetchImpl).url).toBe(
      `${SPECS}/coverage?spec=ACME-SP-0001&status=failing&status=suspect`,
    );

    await expect(provider.queryImpact('ACME', { base: 'main', tiers: [1, 2] })).resolves.toEqual(
      impact,
    );
    expect(lastCall(fetchImpl).url).toBe(`${SPECS}/impact?base=main&tiers=1&tiers=2`);

    await expect(
      provider.getImpactReport('ACME', { base: 'main', budget: 500, format: 'json' }),
    ).resolves.toEqual(report);
    expect(lastCall(fetchImpl).url).toBe(`${SPECS}/impact/report?base=main&budget=500&format=json`);
  });

  it('maps a 503 unavailable problem onto the unavailable code', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        response(
          { code: 'unavailable', status: 503, detail: 'requirement coverage is not available' },
          { status: 503 },
        ),
      );

    await expect(companion(fetchImpl).listCoverage('ACME')).rejects.toMatchObject({
      name: 'ProviderError',
      code: 'unavailable',
    });
  });
});

describe('FakeProvider specs', () => {
  it('answers like browser-only mode by default', async () => {
    const fake = new FakeProvider({ items: [spec], requirements: [requirement] });

    await expect(fake.listSpecs('ACME')).resolves.toMatchObject({ total: 1 });
    await expect(fake.listRequirements('ACME')).resolves.toMatchObject({ total: 1 });
    for (const pending of [
      fake.traceRequirement('ACME', requirement.ref),
      fake.listCoverage('ACME'),
      fake.queryImpact('ACME'),
    ]) {
      await expect(pending).rejects.toMatchObject({ code: 'unavailable' });
    }
  });

  it('writes requirements under their rev and announces the spec', async () => {
    const fake = new FakeProvider({ items: [spec], requirements: [requirement] });
    const events: ChangeEvent[] = [];
    fake.subscribe((event) => events.push(event));

    const created = await fake.createRequirement('ACME', {
      spec: spec.id,
      title: 'Keep the postcode',
    });
    expect(created.requirement.ref).toBe('ACME-SP-0001.R2');

    await expect(
      fake.updateRequirement('ACME', requirement.ref, { title: 'x' }, 'sha256:stale'),
    ).rejects.toMatchObject({ code: 'stale_revision' });
    const updated = await fake.updateRequirement(
      'ACME',
      requirement.ref,
      { title: 'Trim' },
      requirement.rev,
    );
    expect(updated.requirement).toMatchObject({ title: 'Trim' });
    expect(updated.requirement.rev).not.toBe(requirement.rev);
    expect(events.at(-1)).toEqual({ kind: 'items', repoId: 'repo-1', ids: [spec.id] });
  });

  it('serves scripted companion answers', async () => {
    const fake = new FakeProvider({
      items: [spec],
      requirements: [requirement],
      specAnalysis: {
        coverage: [
          { ref: requirement.ref, status: 'failing' },
          { ref: 'ACME-SP-0002.R1', status: 'passing' },
        ],
        impact: { base: 'HEAD', files: 1, symbols: 1, tiers: [], hits: [] },
      },
    });

    await expect(fake.listCoverage('ACME', { status: ['failing'] })).resolves.toEqual({
      coverage: [{ ref: requirement.ref, status: 'failing' }],
      total: 1,
    });
    await expect(fake.queryImpact('ACME', { base: 'main' })).resolves.toMatchObject({
      base: 'main',
    });
    await expect(fake.traceRequirement('ACME', requirement.ref)).resolves.toMatchObject({
      ref: requirement.ref,
    });
  });
});
