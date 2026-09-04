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

describe('NewItemPage', () => {
  let provider: FakeProvider;

  beforeEach(() => {
    provider = new FakeProvider();
  });

  it('prefills the type, parent and milestone from the search params', async () => {
    renderEditorRoute(
      '/p/ACME/items/new?type=task&parent=ACME-US-0042&milestone=ACME-M-0001',
      provider,
    );

    expect(await screen.findByLabelText('Type')).toHaveValue('task');
    expect(screen.getByLabelText('Parent')).toHaveValue('ACME-US-0042');
    expect(screen.getByLabelText('Milestone')).toHaveValue('ACME-M-0001');
    expect(screen.getByLabelText<HTMLTextAreaElement>('Item body').value).toBe(
      '## Description\n\n\n',
    );
  });

  it('uses the story template and creates the item, then opens its detail page', async () => {
    const user = userEvent.setup();
    const create = vi.spyOn(provider, 'createItem');
    const { router } = renderEditorRoute('/p/ACME/items/new?parent=ACME-EP-0001', provider);

    const body = await screen.findByLabelText('Item body');
    expect((body satisfies HTMLElement as HTMLTextAreaElement).value).toBe(
      '## Description\n\n\n\n## Acceptance Criteria\n\n- [ ] \n',
    );

    await user.type(screen.getByLabelText('Title'), 'Refresh tokens');
    fireEvent.change(body, {
      target: { value: '## Description\n\nRotate refresh tokens.\n' },
    });
    await user.click(screen.getByRole('button', { name: 'Create' }));

    await waitFor(() => {
      expect(create).toHaveBeenCalledTimes(1);
    });
    expect(create.mock.calls[0]?.[0]).toEqual({
      project: 'ACME',
      type: 'story',
      title: 'Refresh tokens',
      status: 'backlog',
      parent: 'ACME-EP-0001',
      body: '## Description\n\nRotate refresh tokens.\n',
    });

    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/p/ACME/items/ACME-US-0003');
    });
    expect(await screen.findByText('Detail of ACME-US-0003')).toBeInTheDocument();
  });

  it('swaps the body template when the type changes and the body is untouched', async () => {
    const user = userEvent.setup();
    renderEditorRoute('/p/ACME/items/new', provider);

    const body = await screen.findByLabelText('Item body');
    await user.selectOptions(screen.getByLabelText('Type'), 'milestone');

    await waitFor(() => {
      expect((body satisfies HTMLElement as HTMLTextAreaElement).value).toBe(
        '## Description\n\n\n\n## Exit Criteria\n\n- [ ] \n',
      );
    });
    // A milestone has no parent picker.
    expect(screen.queryByLabelText('Parent')).not.toBeInTheDocument();
  });

  it('refuses to create an item without a title', async () => {
    const user = userEvent.setup();
    const create = vi.spyOn(provider, 'createItem');
    renderEditorRoute('/p/ACME/items/new', provider);

    await user.click(await screen.findByRole('button', { name: 'Create' }));

    expect(create).not.toHaveBeenCalled();
    expect(await screen.findAllByText('E-TITLE')).not.toHaveLength(0);
  });

  it('carries labels and estimate from the front matter form into the draft', async () => {
    const user = userEvent.setup();
    const create = vi.spyOn(provider, 'createItem');
    renderEditorRoute('/p/ACME/items/new', provider);

    await user.type(await screen.findByLabelText('Title'), 'Rotate keys');
    await user.type(screen.getByLabelText('Labels'), 'security{Enter}');
    await user.clear(screen.getByLabelText('Estimate'));
    await user.type(screen.getByLabelText('Estimate'), '5');
    await user.click(screen.getByRole('button', { name: 'Create' }));

    await waitFor(() => {
      expect(create).toHaveBeenCalledTimes(1);
    });
    expect(create.mock.calls[0]?.[0]).toMatchObject({
      labels: ['security'],
      estimate: 5,
    });
  });
});
