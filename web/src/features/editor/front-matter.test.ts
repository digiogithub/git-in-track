import { describe, expect, it } from 'vitest';

import { sampleItems, sampleProject } from '@/api/fake-provider';
import type { Item, ProjectSummary } from '@/api/provider';
import {
  buildPatch,
  hasErrors,
  isEmptyPatch,
  serializeItem,
  validateValues,
  valuesFromItem,
  valuesFromYaml,
  valuesToYaml,
} from '@/features/editor/front-matter';
import { readProjectSchema } from '@/features/editor/project-schema';

const story = sampleItems.find((i) => i.id === 'ACME-US-0042') as Item;
const schema = readProjectSchema(sampleProject);

describe('valuesFromItem / buildPatch', () => {
  it('produces an empty patch when nothing changed', () => {
    const values = valuesFromItem(story);
    const patch = buildPatch(story, values, story.body);
    expect(isEmptyPatch(patch)).toBe(true);
  });

  it('sets only the fields that changed', () => {
    const values = { ...valuesFromItem(story), title: 'Login with SAML' };
    expect(buildPatch(story, values, story.body)).toEqual({ set: { title: 'Login with SAML' } });
  });

  it('patches the body on its own', () => {
    const values = valuesFromItem(story);
    expect(buildPatch(story, values, '## Description\n\nRewritten.\n')).toEqual({
      body: '## Description\n\nRewritten.\n',
    });
  });

  it('unsets cleared fields and keeps the change minimal', () => {
    const values = {
      ...valuesFromItem(story),
      milestone: null,
      estimate: null,
      labels: [],
      status: 'in_review',
    };
    const patch = buildPatch(story, values, story.body);
    expect(patch.set).toEqual({ status: 'in_review' });
    expect(patch.unset?.sort()).toEqual(['estimate', 'labels', 'milestone']);
    expect(patch.body).toBeUndefined();
  });

  it('replaces list fields wholesale when their contents change', () => {
    const values = { ...valuesFromItem(story), assignees: ['marta', 'jose'] };
    expect(buildPatch(story, values, story.body)).toEqual({
      set: { assignees: ['marta', 'jose'] },
    });
  });
});

describe('raw YAML round trip', () => {
  it('serialises and parses back to the same values', () => {
    const values = valuesFromItem(story);
    const yaml = valuesToYaml(values);
    expect(yaml).toContain('title: Login with SSO');
    expect(yaml).toContain('kind: blocked_by');
    const parsed = valuesFromYaml(yaml);
    expect(parsed.ok).toBe(true);
    if (parsed.ok) expect(parsed.values).toEqual(values);
  });

  it('omits empty fields instead of writing null', () => {
    const values = valuesFromItem(story);
    const yaml = valuesToYaml({ ...values, milestone: null, labels: [], links: [] });
    expect(yaml).not.toContain('milestone');
    expect(yaml).not.toContain('labels');
    expect(yaml).not.toContain('links');
  });

  it('reports malformed YAML with a diagnostic code', () => {
    const parsed = valuesFromYaml('title: [unclosed\n');
    expect(parsed.ok).toBe(false);
    if (!parsed.ok) expect(parsed.issues[0]?.code).toBe('E-FM-YAML');
  });

  it('rejects an unknown priority', () => {
    const parsed = valuesFromYaml('title: A\npriority: urgent\n');
    expect(parsed.ok).toBe(false);
    if (!parsed.ok) expect(parsed.issues.map((i) => i.code)).toContain('E-ENUM');
  });
});

describe('validateValues', () => {
  it('requires a title and a known status', () => {
    const values = { ...valuesFromItem(story), title: '  ', status: 'archived' };
    const diagnostics = validateValues(values, schema, 'story');
    expect(diagnostics.map((d) => d.code)).toEqual(
      expect.arrayContaining(['E-TITLE', 'E-STATUS-UNKNOWN']),
    );
    expect(hasErrors(diagnostics)).toBe(true);
  });

  it('warns about an estimate outside the scale and an undeclared label', () => {
    const values = { ...valuesFromItem(story), estimate: 7, labels: ['made-up'] };
    const diagnostics = validateValues(values, schema, 'story');
    const warnings = diagnostics.filter((d) => d.severity === 'warning').map((d) => d.code);
    expect(warnings).toEqual(
      expect.arrayContaining(['W-ESTIMATE-SCALE', 'W-LABEL-UNDECLARED']),
    );
    expect(hasErrors(diagnostics)).toBe(false);
  });

  it('rejects a parent on an epic', () => {
    const values = { ...valuesFromItem(story), parent: 'ACME-EP-0001' };
    const codes = validateValues(values, schema, 'epic').map((d) => d.code);
    expect(codes).toContain('E-REF-PARENT-TYPE');
  });

  it('honours custom field declarations from project.yaml', () => {
    const project = {
      ...sampleProject,
      custom_fields: [{ key: 'risk', type: 'enum', values: ['low', 'high'], applies_to: ['story'] }],
    } as unknown as ProjectSummary;
    const withCustom = readProjectSchema(project);
    const values = { ...valuesFromItem(story), custom: { risk: 'unknown' } };
    const codes = validateValues(values, withCustom, 'story').map((d) => d.code);
    expect(codes).toContain('E-ENUM');
  });
});

describe('serializeItem', () => {
  it('emits front matter in canonical key order for validation', () => {
    const text = serializeItem(story, valuesFromItem(story), story.body);
    const keys = [...text.matchAll(/^([a-z]+):/gm)].map((m) => m[1]);
    expect(keys.slice(0, 4)).toEqual(['id', 'type', 'title', 'status']);
    expect(text.startsWith('---\n')).toBe(true);
    expect(text).toContain('\n---\n\n## Description');
  });
});
