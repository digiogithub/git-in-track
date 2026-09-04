import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import { renderEditorRoute } from '@/features/editor/test-utils';

vi.mock('@/components/editor/MarkdownEditor', () => ({
  MarkdownEditor: ({
    value,
    onChange,
    label,
  }: {
    value: string;
    onChange: (next: string) => void;
    label: string;
  }) => (
    <textarea
      aria-label={label}
      value={value}
      onChange={(event) => {
        onChange(event.target.value);
      }}
    />
  ),
}));

const storyId = 'ACME-US-0042';
const storyRev = 'sha256:0000000000000042';
const editPath = `/p/ACME/items/${storyId}/edit`;

describe('ItemEditorPage', () => {
  let provider: FakeProvider;

  beforeEach(() => {
    provider = new FakeProvider();
  });

  it('loads the item into the form and the body editor', async () => {
    renderEditorRoute(editPath, provider);

    expect(await screen.findByLabelText('Title')).toHaveValue('Login with SSO');
    expect(screen.getByLabelText('Status')).toHaveValue('in_progress');
    expect(screen.getByLabelText<HTMLTextAreaElement>('Item body').value).toContain(
      '## Acceptance Criteria',
    );
  });

  it('saves a minimal patch with the observed revision', async () => {
    const user = userEvent.setup();
    const update = vi.spyOn(provider, 'updateItem');
    renderEditorRoute(editPath, provider);

    const title = await screen.findByLabelText('Title');
    await user.clear(title);
    await user.type(title, 'Login with SAML');
    fireEvent.change(screen.getByLabelText('Item body'), {
      target: { value: '## Description\n\nRewritten.\n' },
    });

    await user.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => {
      expect(update).toHaveBeenCalledTimes(1);
    });
    expect(update.mock.calls[0]).toEqual([
      storyId,
      { set: { title: 'Login with SAML' }, body: '## Description\n\nRewritten.\n' },
      storyRev,
    ]);
    expect(await screen.findByText('Saved')).toBeInTheDocument();
  });

  it('validates before saving and never writes an item without a title', async () => {
    const user = userEvent.setup();
    const update = vi.spyOn(provider, 'updateItem');
    renderEditorRoute(editPath, provider);

    const title = await screen.findByLabelText('Title');
    await user.clear(title);

    expect(await screen.findAllByText('E-TITLE')).not.toHaveLength(0);
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(screen.getAllByText(/Title is required/)).not.toHaveLength(0);
    });
    expect(update).not.toHaveBeenCalled();
  });

  it('opens the conflict dialog when the revision is stale, and can overwrite', async () => {
    const user = userEvent.setup();
    renderEditorRoute(editPath, provider);

    const title = await screen.findByLabelText('Title');
    // Someone else writes the file after the buffer was opened.
    await provider.updateItem(storyId, { set: { status: 'in_review' } }, storyRev);

    await user.clear(title);
    await user.type(title, 'Login with SSO everywhere');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent(`${storyId} changed on disk`);

    await user.click(screen.getByRole('button', { name: 'Overwrite with mine' }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
    const saved = await provider.getItem(storyId);
    expect(saved.title).toBe('Login with SSO everywhere');
    // The other write survived: overwriting re-reads the revision, it does not
    // resend the stale front matter.
    expect(saved.status).toBe('in_review');
  });

  it('reloads the other version when the conflict dialog offers it', async () => {
    const user = userEvent.setup();
    renderEditorRoute(editPath, provider);

    const title = await screen.findByLabelText('Title');
    await provider.updateItem(storyId, { set: { title: 'Renamed on disk' } }, storyRev);
    await user.clear(title);
    await user.type(title, 'Mine');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    await screen.findByRole('dialog');
    await user.click(screen.getByRole('button', { name: 'Reload theirs' }));

    await waitFor(() => {
      expect(screen.getByLabelText('Title')).toHaveValue('Renamed on disk');
    });
  });

  it('round-trips the front matter through the raw YAML view', async () => {
    const user = userEvent.setup();
    renderEditorRoute(editPath, provider);
    await screen.findByLabelText('Title');

    const toggle = screen.getByRole('switch', { name: 'Raw YAML' });
    await user.click(toggle);

    const raw = await screen.findByLabelText('Front matter YAML');
    expect((raw satisfies HTMLElement as HTMLTextAreaElement).value).toContain('title: Login with SSO');

    fireEvent.change(raw, {
      target: { value: 'title: Renamed in YAML\nstatus: todo\nlabels:\n  - security\n' },
    });
    await user.click(toggle);

    expect(await screen.findByLabelText('Title')).toHaveValue('Renamed in YAML');
    expect(screen.getByLabelText('Status')).toHaveValue('todo');
    expect(screen.getByRole('button', { name: 'Remove security from Labels' })).toBeInTheDocument();
  });

  it('surfaces invalid YAML inline with its diagnostic code', async () => {
    const user = userEvent.setup();
    renderEditorRoute(editPath, provider);
    await screen.findByLabelText('Title');

    await user.click(screen.getByRole('switch', { name: 'Raw YAML' }));
    fireEvent.change(await screen.findByLabelText('Front matter YAML'), {
      target: { value: 'title: [unclosed\n' },
    });

    expect(await screen.findAllByText('E-FM-YAML')).not.toHaveLength(0);
    expect(screen.getByRole('button', { name: 'Restore last valid' })).toBeInTheDocument();
  });

  it('keeps autosave off until it is switched on', async () => {
    renderEditorRoute(editPath, provider);
    const autosave = await screen.findByRole('switch', { name: 'Autosave' });
    expect(autosave).toHaveAttribute('aria-checked', 'false');
  });
});
