import { describe, expect, it } from 'vitest';

import type { VaultFile } from '@/core-bridge/api';

import { detectDocsFolders, normalizeDocsFolder } from './detect-project';

/** The smallest `project.yaml` the detector reads a key and a name out of. */
function project(key: string, name = key): string {
  return `schema: 1\nkey: ${key}\nname: ${name}\n`;
}

function files(paths: Record<string, string>): VaultFile[] {
  return Object.entries(paths).map(([path, text]) => ({ path, text }));
}

describe('detectDocsFolders', () => {
  const cases: { name: string; input: VaultFile[]; expected: [string, boolean][] }[] = [
    {
      name: 'the repository root',
      input: files({ '.pmngr/project.yaml': project('ROOT') }),
      expected: [['', false]],
    },
    {
      name: 'a first-level folder discovery reaches on its own',
      input: files({ 'docs/.pmngr/project.yaml': project('DOCS') }),
      expected: [['docs', false]],
    },
    {
      name: 'a nested folder that mounting has to declare',
      input: files({ 'apps/api/docs/.pmngr/project.yaml': project('API') }),
      expected: [['apps/api/docs', true]],
    },
    {
      name: 'anything deeper than the detector looks is not offered at all',
      input: files({ 'a/b/c/d/e/.pmngr/project.yaml': project('DEEP') }),
      expected: [],
    },
    {
      name: 'docs first, then the shallowest, then alphabetically',
      input: files({
        'apps/web/docs/.pmngr/project.yaml': project('WEB'),
        'apps/api/docs/.pmngr/project.yaml': project('API'),
        'other/.pmngr/project.yaml': project('OTHER'),
        'docs/.pmngr/project.yaml': project('DOCS'),
      }),
      expected: [
        ['docs', false],
        ['other', false],
        ['apps/api/docs', true],
        ['apps/web/docs', true],
      ],
    },
  ];

  for (const testCase of cases) {
    it(`detects ${testCase.name}`, () => {
      const found = detectDocsFolders(testCase.input);
      expect(found.map((c) => [c.docsFolder, c.declarationNeeded])).toEqual(testCase.expected);
    });
  }

  it('reads the key and the name out of project.yaml', () => {
    const [candidate] = detectDocsFolders(
      files({ 'docs/.pmngr/project.yaml': project('ACME', 'ACME Platform') }),
    );
    expect(candidate?.projectKey).toBe('ACME');
    expect(candidate?.projectName).toBe('ACME Platform');
  });

  it('finds nothing in a repository that has no backlog', () => {
    expect(detectDocsFolders(files({ 'README.md': '# hello\n' }))).toEqual([]);
  });
});

describe('normalizeDocsFolder', () => {
  const cases: [string, string][] = [
    ['docs', 'docs'],
    ['  docs  ', 'docs'],
    ['./docs', 'docs'],
    ['/docs/', 'docs'],
    ['docs\\nested', 'docs/nested'],
    ['', ''],
    ['/', ''],
  ];

  for (const [input, expected] of cases) {
    it(`normalises ${JSON.stringify(input)} to ${JSON.stringify(expected)}`, () => {
      expect(normalizeDocsFolder(input)).toBe(expected);
    });
  }
});
