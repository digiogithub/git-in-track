import { describe, expect, it, vi } from 'vitest';

import type { DataProvider, SpecLintFinding, SpecLintInput } from '@/api/provider';
import { lintsSpecText, mayHaveSpecDelta, specLintSource } from '@/features/editor/spec-lint';

/**
 * A mocked core: it answers what `spec.lint` would for a project whose
 * `specs.lint` is given, without running a single grammar rule.
 */
function mockedCore(levels: Record<string, 'off' | 'warning' | 'error'>) {
  const all: Omit<SpecLintFinding, 'severity'>[] = [
    { code: 'LINT-REQ-VAGUE', line: 5, ref: 'ACME-SP-0001.R1', message: 'vague "fast"' },
    { code: 'LINT-REQ-SCENARIO', line: 3, ref: 'ACME-SP-0001.R1', message: 'no scenario' },
  ];
  const lintSpecText = vi.fn((_project: string, _input: SpecLintInput) =>
    Promise.resolve(
      all.flatMap((f) => {
        const level = levels[f.code] ?? 'warning';
        return level === 'off' ? [] : [{ ...f, severity: level }];
      }),
    ),
  );
  return { lintSpecText } satisfies Pick<DataProvider, 'lintSpecText'>;
}

describe('specLintSource', () => {
  it('sends the body with the project, the id and the type', async () => {
    const core = mockedCore({});
    await specLintSource(core, 'ACME', { id: 'ACME-SP-0001', type: 'spec' })('## body');
    expect(core.lintSpecText).toHaveBeenCalledWith('ACME', {
      id: 'ACME-SP-0001',
      type: 'spec',
      body: '## body',
    });
  });

  it('keeps the severity specs.lint gives each rule', async () => {
    const core = mockedCore({ 'LINT-REQ-VAGUE': 'error', 'LINT-REQ-SCENARIO': 'warning' });
    const got = await specLintSource(core, 'ACME', { type: 'spec' })('x');
    expect(got).toEqual([
      { line: 5, severity: 'error', message: 'vague "fast"', source: 'LINT-REQ-VAGUE' },
      { line: 3, severity: 'warning', message: 'no scenario', source: 'LINT-REQ-SCENARIO' },
    ]);
  });

  it('shows nothing for a rule at off', async () => {
    const core = mockedCore({ 'LINT-REQ-VAGUE': 'off', 'LINT-REQ-SCENARIO': 'off' });
    expect(await specLintSource(core, 'ACME', { type: 'story' })('x')).toEqual([]);
  });

  it('underlines nothing when the core cannot answer', async () => {
    const core = { lintSpecText: vi.fn(() => Promise.reject(new Error('worker not ready'))) };
    expect(await specLintSource(core, 'ACME', { type: 'task' })('x')).toEqual([]);
  });
});

describe('lintsSpecText', () => {
  it.each([
    ['spec', true],
    ['story', true],
    ['task', true],
    ['epic', false],
    ['milestone', false],
  ] as const)('%s → %s', (type, want) => {
    expect(lintsSpecText(type)).toBe(want);
  });
});

describe('mayHaveSpecDelta', () => {
  it('finds the level-2 heading in any case and nothing else', () => {
    expect(mayHaveSpecDelta('## Notes\n\n## spec delta\n')).toBe(true);
    expect(mayHaveSpecDelta('### Spec Delta\n')).toBe(false);
    expect(mayHaveSpecDelta('We will write a spec delta later.')).toBe(false);
  });
});
