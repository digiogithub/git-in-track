import { bodyTemplate, isPristineTemplate, requirementTemplate } from '@/features/editor/templates';

describe('spec templates', () => {
  it('ships the core spec template: purpose, scope, glossary and one example block', () => {
    const spec = bodyTemplate('spec');
    for (const section of ['## Purpose\n', '## Scope\n', '## Glossary\n', '## Requirements\n']) {
      expect(spec).toContain(section);
    }
    expect(spec).toContain('### <SPEC-ID>.R1 — ');
    expect(isPristineTemplate(spec)).toBe(true);
  });

  it('uses the requirement template as the example block of the spec template', () => {
    expect(requirementTemplate).toMatch(/SHALL/);
    expect(requirementTemplate).toContain('#### Scenario: ');
    expect(bodyTemplate('spec').endsWith(`\n\n${requirementTemplate}`)).toBe(true);
  });
});

describe('a project spec template override', () => {
  const custom = '## Purpose\n\nOurs.\n';

  it('replaces the embedded spec template and nothing else', () => {
    expect(bodyTemplate('spec', custom)).toBe(custom);
    expect(bodyTemplate('story', custom)).toBe(bodyTemplate('story'));
  });

  it('counts as pristine, so the type can still be switched', () => {
    expect(isPristineTemplate(custom, custom)).toBe(true);
    expect(isPristineTemplate(custom)).toBe(false);
    expect(isPristineTemplate(`${custom}edited`, custom)).toBe(false);
  });
});
