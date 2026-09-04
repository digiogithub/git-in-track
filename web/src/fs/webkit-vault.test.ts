import { describe, expect, it } from 'vitest';

import { VaultError } from './types';
import { WebkitDirectoryVault } from './webkit-vault';

/** `webkitdirectory` inputs expose the folder path on every `File`. */
function file(path: string, text: string): File {
  const created = new File([text], path.slice(path.lastIndexOf('/') + 1));
  Object.defineProperty(created, 'webkitRelativePath', { value: path });
  return created;
}

const picked = [
  file('acme-repo/README.md', '# Acme'),
  file('acme-repo/docs/index.md', '# Docs'),
  file('acme-repo/docs/.pmngr/project.yaml', 'key: ACME\n'),
  file('acme-repo/docs/assets/logo.png', 'binary-bytes'),
  file('acme-repo/.git/config', '[core]'),
  file('acme-repo/node_modules/left-pad/index.js', 'x'),
];

describe('WebkitDirectoryVault', () => {
  it('takes the folder name from the relative paths', () => {
    const vault = new WebkitDirectoryVault(picked);

    expect(vault.name).toBe('acme-repo');
    expect(vault.kind).toBe('webkitdirectory');
    expect(vault.hasGit).toBe(true);
  });

  it('reads text files and skips ignored folders', async () => {
    const vault = new WebkitDirectoryVault(picked);

    const files = await vault.readTextFiles();

    expect(files.map((entry) => entry.path)).toEqual([
      'docs/.pmngr/project.yaml',
      'docs/index.md',
      'README.md',
    ]);
    expect(vault.cachedFiles()).toHaveLength(3);
  });

  it('serves binary assets from the in-memory snapshot', async () => {
    const vault = new WebkitDirectoryVault(picked);

    const blob = await vault.readBinary('docs/assets/logo.png');
    await expect(blob.text()).resolves.toBe('binary-bytes');
    await expect(vault.readBinary('missing.png')).rejects.toBeInstanceOf(VaultError);
  });

  it('is read-only: every write rejects with `read_only`', async () => {
    const vault = new WebkitDirectoryVault(picked);

    expect(vault.capabilities.write).toBe(false);
    await expect(vault.writeFile('docs/index.md', 'nope')).rejects.toMatchObject({
      code: 'read_only',
    });
    await expect(vault.removeFile('docs/index.md')).rejects.toMatchObject({ code: 'read_only' });
    await expect(vault.rename('docs/index.md', 'docs/other.md')).rejects.toMatchObject({
      code: 'read_only',
    });
  });

  it('never produces rescan events: the snapshot cannot change', async () => {
    const vault = new WebkitDirectoryVault(picked);

    await expect(vault.rescan()).resolves.toEqual([]);
    expect(vault.cachedFiles()).not.toBeNull();
  });
});
