import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { CredentialPrompt } from '@/features/sync/CredentialPrompt';
import { createAuthCallback, forgetCredentials } from '@/git/credentials';

/**
 * Opens a prompt the way a fetch does, inside `act` so the queue's synchronous
 * notification is a React update the test owns.
 */
function ask(url: string, opts: { corsProxy?: string } = {}): Promise<unknown> {
  let pending: Promise<unknown> | undefined;
  act(() => {
    pending = Promise.resolve(createAuthCallback(opts)(url, {}));
  });
  return pending ?? Promise.resolve(undefined);
}

/**
 * The token prompt (story GIT-US-0023). It is driven by the pending-request
 * queue of `@/git/credentials`, so every case here starts from a real `onAuth`
 * call — the same way a fetch or a push opens it.
 */

const TOKEN = 'ghp-prompt-token-value';

describe('CredentialPrompt', () => {
  beforeEach(() => {
    forgetCredentials();
  });

  afterEach(() => {
    // The panel may still be mounted here, and forgetting drops any pending
    // prompt, which is a React update the test has to own.
    act(() => {
      forgetCredentials();
    });
  });

  it('renders nothing until a transport asks for a credential', () => {
    render(<CredentialPrompt />);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('asks for a token, names the host and hands it back to the caller', async () => {
    const user = userEvent.setup();
    render(<CredentialPrompt />);
    const pending = ask('https://git.acme.test/acme/web.git');

    expect(await screen.findByText('git.acme.test needs a token')).toBeInTheDocument();
    const field = screen.getByLabelText('Personal access token');
    expect(field).toHaveAttribute('type', 'password');

    await user.type(field, TOKEN);
    await user.click(screen.getByRole('button', { name: 'Use for this session' }));

    expect(await pending).toEqual({ username: 'token', password: TOKEN });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('warns that a configured CORS proxy will see the token', async () => {
    render(<CredentialPrompt />);
    void ask('https://git.acme.test/acme/web.git', { corsProxy: 'https://proxy.example.test' });

    const warning = await screen.findByRole('alert');
    expect(warning).toHaveTextContent('https://proxy.example.test');
    expect(warning).toHaveTextContent('will see it and the token it carries');
  });

  it('suggests the username a known host expects', async () => {
    render(<CredentialPrompt />);
    void ask('https://github.com/acme/web.git');

    expect(await screen.findByLabelText('Username')).toHaveValue('x-access-token');
  });

  it('cancels the operation when the user dismisses it', async () => {
    const user = userEvent.setup();
    render(<CredentialPrompt />);
    const pending = ask('https://git.acme.test/acme/web.git');

    await user.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(await pending).toEqual({ cancel: true });
  });

  it('writes the typed token to no storage API', async () => {
    const writes: string[] = [];
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation((key: string, value: string) => {
      writes.push(`${key}=${value}`);
    });
    const user = userEvent.setup();
    render(<CredentialPrompt />);
    const pending = ask('https://git.acme.test/acme/web.git');

    await user.type(await screen.findByLabelText('Personal access token'), TOKEN);
    await user.click(screen.getByRole('button', { name: 'Use for this session' }));
    await pending;

    expect(writes).toEqual([]);
    expect(document.body.innerHTML).not.toContain(TOKEN);
  });
});
