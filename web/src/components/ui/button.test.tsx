import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { Button } from './button';

describe('Button', () => {
  it('renders an accessible button and calls the handler on click', async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();

    render(<Button onClick={onClick}>Add repository</Button>);

    const button = screen.getByRole('button', { name: /add repository/i });
    expect(button).toBeInTheDocument();
    expect(button).toHaveAttribute('type', 'button');

    await user.click(button);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('applies variant classes and lets the caller override them', () => {
    render(
      <Button variant="destructive" className="bg-accent">
        Delete
      </Button>,
    );

    const button = screen.getByRole('button', { name: /delete/i });
    // The variant is applied...
    expect(button.className).toContain('text-destructive-foreground');
    // ...but tailwind-merge lets the caller-supplied background win.
    expect(button.className).toContain('bg-accent');
    expect(button.className).not.toMatch(/(^|\s)bg-destructive(\s|$)/);
  });

  it('does not fire when disabled', async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();

    render(
      <Button disabled onClick={onClick}>
        Detect repository
      </Button>,
    );

    await user.click(screen.getByRole('button', { name: /detect repository/i }));
    expect(onClick).not.toHaveBeenCalled();
  });
});
