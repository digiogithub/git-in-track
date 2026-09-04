import { describe, expect, it } from 'vitest';

import { FakeDirectory } from './fake-handles';
import { createGitFs, FsError } from './fsa-fs';

describe('createGitFs', () => {
  it('round-trips bytes and text through a folder handle', async () => {
    const fs = createGitFs(new FakeDirectory()).promises;

    await fs.writeFile('.git/objects/ab/cdef', new Uint8Array([1, 2, 3]));
    const bytes = await fs.readFile('.git/objects/ab/cdef');
    expect(bytes).toBeInstanceOf(Uint8Array);
    expect([...(bytes as Uint8Array)]).toEqual([1, 2, 3]);

    await fs.writeFile('docs/index.md', '# hello\n');
    expect(await fs.readFile('docs/index.md', { encoding: 'utf8' })).toBe('# hello\n');
  });

  it('reports a missing path as ENOENT, which is how git asks a question', async () => {
    const fs = createGitFs(new FakeDirectory()).promises;
    await expect(fs.readFile('.git/HEAD')).rejects.toMatchObject({ code: 'ENOENT' });
    await expect(fs.stat('nope')).rejects.toMatchObject({ code: 'ENOENT' });
  });

  it('lists, stats and removes entries', async () => {
    const fs = createGitFs(new FakeDirectory()).promises;
    await fs.mkdir('docs');
    await fs.writeFile('docs/a.md', 'a');
    await fs.writeFile('docs/b.md', 'bb');

    expect(await fs.readdir('docs')).toEqual(['a.md', 'b.md']);

    const stat = await fs.stat('docs/b.md');
    expect(stat.isFile()).toBe(true);
    expect(stat.isDirectory()).toBe(false);
    expect(stat.size).toBe(2);

    const dir = await fs.stat('docs');
    expect(dir.isDirectory()).toBe(true);

    await fs.unlink('docs/a.md');
    expect(await fs.readdir('docs')).toEqual(['b.md']);
  });

  it('refuses a path that would escape the mounted folder', async () => {
    const fs = createGitFs(new FakeDirectory()).promises;
    await expect(fs.readFile('../outside')).rejects.toBeInstanceOf(FsError);
  });

  it('has no symlinks, and says so the way git expects', async () => {
    const fs = createGitFs(new FakeDirectory()).promises;
    await expect(fs.readlink('link')).rejects.toMatchObject({ code: 'EINVAL' });
    await expect(fs.symlink()).rejects.toMatchObject({ code: 'EPERM' });
  });
});
