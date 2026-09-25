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
