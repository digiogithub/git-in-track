import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import type { CreateProjectInput, MountInput } from '@/api/provider';
import { useAppStore } from '@/app/store';
import { clearVaultRegistry, MemoryVault, registerVault } from '@/fs';
import { renderWithRouter } from '@/test/router';

import { AddRepositoryPage } from './AddRepositoryPage';

/** A provider that records what the wizard asked it to do. */
class RecordingProvider extends FakeProvider {
  readonly mounts: MountInput[] = [];
  readonly created: CreateProjectInput[] = [];

  override mountRepo(input: MountInput) {
    this.mounts.push(input);
    return super.mountRepo(input);
  }

  override createProject(input: CreateProjectInput) {
    this.created.push(input);
    return super.createProject(input);
  }
}

/** Registers a picked folder and points the store at it, as the picker does. */
function pickFolder(files: Record<string, string>): string {
  const id = registerVault(new MemoryVault(files, { name: 'acme-repo' }));
  useAppStore.getState().setPendingVault(id, 'acme-repo');
  return id;
}

afterEach(() => {
  clearVaultRegistry();
  useAppStore.getState().setPendingVault(null);
});

describe('AddRepositoryPage', () => {
  it('creates a project when the repository has no backlog', async () => {
    const user = userEvent.setup();
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'README.md': '# acme\n' });

    renderWithRouter({ index: AddRepositoryPage, provider });

    expect(
      await screen.findByRole('button', { name: /create project/i }, { timeout: 5000 }),
    ).toBeInTheDocument();

    await user.clear(screen.getByLabelText(/documentation folder/i));
    await user.type(screen.getByLabelText(/documentation folder/i), 'docs');
    await user.type(screen.getByLabelText(/project key/i), 'ACME');
    await user.type(screen.getByLabelText(/project name/i), 'ACME Platform');
    await user.click(screen.getByRole('button', { name: /create project/i }));

    await waitFor(() => {
      expect(provider.created).toEqual([
        expect.objectContaining({ docsFolder: 'docs', key: 'ACME', name: 'ACME Platform' }),
      ]);
    });
    // The folder is mounted first: the core has to hold it before it can write.
    expect(provider.mounts[0]).toEqual(
      expect.objectContaining({ docsFolder: 'docs', docsFolders: ['docs'] }),
    );
  });

  it('refuses a key the grammar does not accept, without calling the provider', async () => {
    const user = userEvent.setup();
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'README.md': '# acme\n' });

    renderWithRouter({ index: AddRepositoryPage, provider });
    await screen.findByLabelText(/project key/i, undefined, { timeout: 5000 });

    await user.type(screen.getByLabelText(/project key/i), 'A');
    await user.click(screen.getByRole('button', { name: /create project/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/2 to 10 characters/i);
    expect(provider.created).toEqual([]);
  });

  it('keeps "mount it anyway" as an explicit choice', async () => {
    const user = userEvent.setup();
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'README.md': '# acme\n' });

    renderWithRouter({ index: AddRepositoryPage, provider });
    await screen.findByRole('button', { name: /mount it anyway/i }, { timeout: 5000 });

    await user.click(screen.getByRole('button', { name: /mount it anyway/i }));

    await waitFor(() => {
      expect(provider.mounts).toHaveLength(1);
    });
    expect(provider.created).toEqual([]);
  });

  it('offers the detected folders instead of the create form when a backlog exists', async () => {
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'docs/.pmngr/project.yaml': 'schema: 1\nkey: ACME\nname: ACME Platform\n' });

    renderWithRouter({ index: AddRepositoryPage, provider });

    expect(await screen.findByRole('radio', { name: /ACME/ }, { timeout: 5000 })).toBeChecked();
    expect(screen.getByRole('button', { name: /mount repository/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /create project/i })).not.toBeInTheDocument();
  });

  it('marks a nested candidate as one the mount has to declare', async () => {
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'apps/api/docs/.pmngr/project.yaml': 'schema: 1\nkey: API\n' });

    renderWithRouter({ index: AddRepositoryPage, provider });

    expect(
      await screen.findByText(/indexed only if you choose it here/i, undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
  });
});
