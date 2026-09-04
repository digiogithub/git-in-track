import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import type { DataProvider } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';
import { GitSettingsCard } from '@/features/settings/GitSettingsCard';

/** Renders the card against a provider, which is the only dependency it has. */
function renderCard(provider: DataProvider = new FakeProvider()) {
  render(
    <ProviderContext.Provider value={provider}>
      <GitSettingsCard />
    </ProviderContext.Provider>,
  );
  return provider;
}

describe('GitSettingsCard', () => {
  it('shows commit-on-save off with the shipped template', async () => {
    renderCard();

    expect(await screen.findByText('Off')).toBeInTheDocument();
    expect(await screen.findByLabelText('Message template')).toHaveValue(
      'pmngr: update {{.ItemID}} "{{.Title}}"',
    );
    expect(screen.getByLabelText('Batching window (ms)')).toHaveValue('2000');
    expect(screen.getByRole('switch', { name: 'Commit each save' })).not.toBeChecked();
  });

  it('turns commit-on-save on through the provider', async () => {
    const provider = renderCard();
    const toggle = await screen.findByRole('switch', { name: 'Commit each save' });

    await userEvent.click(toggle);

    await waitFor(() => {
      expect(screen.getByText('On')).toBeInTheDocument();
    });
    await expect(provider.getGitSettings()).resolves.toMatchObject({ commitOnSave: true });
  });

  it('previews the message the configured template renders', async () => {
    renderCard();
    const template = await screen.findByLabelText('Message template');

    // fireEvent, not userEvent.type: `{{` is an escape in userEvent's syntax
    // and a literal in ours.
    fireEvent.change(template, { target: { value: '{{action}} {{id}}: {{title}}' } });

    expect(await screen.findByText(/move ACME-US-0042: Login with SSO/)).toBeInTheDocument();
  });

  it('saves a new template and window', async () => {
    const provider = renderCard();
    const template = await screen.findByLabelText('Message template');
    fireEvent.change(template, { target: { value: '{{action}} {{id}}' } });
    const window = screen.getByLabelText('Batching window (ms)');
    fireEvent.change(window, { target: { value: '500' } });

    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(async () => {
      await expect(provider.getGitSettings()).resolves.toMatchObject({
        messageTemplate: '{{action}} {{id}}',
        commitDebounceMs: 500,
      });
    });
  });

  it('refuses a broken template and says which placeholders exist', async () => {
    const provider = renderCard();
    const template = await screen.findByLabelText('Message template');
    fireEvent.change(template, { target: { value: '{{nosuchplaceholder}}' } });

    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/unknown placeholder/);
    await expect(provider.getGitSettings()).resolves.toMatchObject({
      messageTemplate: 'pmngr: update {{.ItemID}} "{{.Title}}"',
    });
  });

  it('refuses a negative window before calling the provider', async () => {
    renderCard();
    const window = await screen.findByLabelText('Batching window (ms)');
    fireEvent.change(window, { target: { value: '-5' } });

    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/not negative/);
  });

  it('explains a runtime that cannot commit and disables the switch', async () => {
    renderCard(new FakeProvider({}, { readOnly: true }));

    expect(await screen.findByRole('status')).toHaveTextContent('read-only');
    expect(screen.getByRole('switch', { name: 'Commit each save' })).toBeDisabled();
  });

  it('lists the repositories and the identity commits are attributed to', async () => {
    renderCard();
    expect(await screen.findByText('Test User <test@example.com>')).toBeInTheDocument();
  });
});
