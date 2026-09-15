import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { PermissionPrompt } from '@/features/agent/hitl';
import { PermissionDialog } from '@/features/agent/ui/PermissionDialog';

const prompt: PermissionPrompt = {
  kind: 'permission',
  toolCallId: 'perm-1',
  request: {
    toolName: 'write',
    action: 'write',
    path: 'main.go',
    description: 'Create the entry point',
    params: { content: '<script>alert(1)</script>' },
  },
};

function setup(readOnly = false) {
  const onApprove = vi.fn();
  const onDeny = vi.fn();
  render(
    <PermissionDialog prompt={prompt} readOnly={readOnly} onApprove={onApprove} onDeny={onDeny} />,
  );
  return { onApprove, onDeny, user: userEvent.setup() };
}

describe('the approval card', () => {
  it('names the tool and shows its arguments as text, never as markup', () => {
    setup();

    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getAllByText('write').length).toBeGreaterThan(0);
    expect(screen.getByText('main.go')).toBeInTheDocument();
    const params = screen.getByTestId('permission-params');
    expect(params).toHaveTextContent('<script>alert(1)</script>');
    expect(params.querySelector('script')).toBeNull();
  });

  it('approves once without granting anything standing', async () => {
    const { onApprove, user } = setup();

    await user.click(screen.getByRole('button', { name: 'Approve once' }));

    expect(onApprove).toHaveBeenCalledWith({ always: false });
  });

  it('approves always when the switch is on', async () => {
    const { onApprove, user } = setup();

    await user.click(screen.getByRole('switch', { name: 'Always allow' }));
    await user.click(screen.getByRole('button', { name: 'Always allow' }));

    expect(onApprove).toHaveBeenCalledWith({ always: true });
  });

  it('denies on the explicit button', async () => {
    const { onDeny, user } = setup();

    await user.click(screen.getByRole('button', { name: 'Deny' }));

    expect(onDeny).toHaveBeenCalledTimes(1);
  });

  it('turns Escape into a confirmed denial rather than a silent one', async () => {
    const { onDeny, user } = setup();

    await user.keyboard('{Escape}');

    // The dialog is still open: a stray keypress may not end a turn.
    expect(onDeny).not.toHaveBeenCalled();
    const confirm = screen.getByRole('alertdialog', { name: 'Confirm the denial' });
    await user.click(within(confirm).getByRole('button', { name: 'Deny' }));
    expect(onDeny).toHaveBeenCalledTimes(1);
  });

  it('lets the confirmation be backed out of', async () => {
    const { onDeny, user } = setup();

    await user.keyboard('{Escape}');
    await user.click(screen.getByRole('button', { name: 'Keep deciding' }));

    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(screen.getByRole('button', { name: 'Approve once' })).toBeInTheDocument();
    expect(onDeny).not.toHaveBeenCalled();
  });

  it('offers no answer at all in a read-only tab', () => {
    setup(true);

    expect(screen.queryByRole('button', { name: 'Approve once' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Deny' })).toBeNull();
    expect(screen.getByText(/Another tab owns this conversation/)).toBeInTheDocument();
  });
});
