import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { PALETTE_STORAGE_KEY } from '@/app/palette';
import { THEME_STORAGE_KEY } from '@/app/theme';
import { AppearanceCard } from '@/features/settings/AppearanceCard';

function reset(): void {
  localStorage.clear();
  delete document.documentElement.dataset['theme'];
  delete document.documentElement.dataset['palette'];
}

describe('AppearanceCard', () => {
  beforeEach(reset);
  afterEach(reset);

  it('shows the current mode and palette, defaulting to system and default', () => {
    render(<AppearanceCard />);

    expect(screen.getByRole('heading', { name: 'Appearance' })).toBeInTheDocument();
    const mode = screen.getByRole('group', { name: 'Mode' });
    expect(mode).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'System' })).toBeChecked();
    expect(screen.getByRole('radio', { name: /^Default/ })).toBeChecked();
  });

  it('offers a swatch preview per palette', () => {
    const { container } = render(<AppearanceCard />);
    const swatches = [...container.querySelectorAll('[data-swatch]')].map((node) =>
      node.getAttribute('data-swatch'),
    );
    expect(swatches).toEqual(['default', 'high-contrast', 'cool', 'colour-blind']);
    const cool = container.querySelector('[data-swatch="cool"]') as HTMLElement;
    expect(cool.style.background).toBe('hsl(var(--swatch-cool-background))');
  });

  it('switches the palette, stamps it and remembers it', async () => {
    const user = userEvent.setup();
    render(<AppearanceCard />);

    await user.click(screen.getByRole('radio', { name: /^High contrast/ }));
    expect(screen.getByRole('radio', { name: /^High contrast/ })).toBeChecked();
    expect(document.documentElement.dataset['palette']).toBe('high-contrast');
    expect(localStorage.getItem(PALETTE_STORAGE_KEY)).toBe('high-contrast');

    await user.click(screen.getByRole('radio', { name: /^Default/ }));
    expect(document.documentElement.dataset['palette']).toBeUndefined();
    expect(localStorage.getItem(PALETTE_STORAGE_KEY)).toBeNull();
  });

  it('switches the mode without touching the palette', async () => {
    const user = userEvent.setup();
    localStorage.setItem(PALETTE_STORAGE_KEY, 'colour-blind');
    render(<AppearanceCard />);

    expect(screen.getByRole('radio', { name: /^Colour-blind safe/ })).toBeChecked();
    await user.click(screen.getByRole('radio', { name: 'Dark' }));
    expect(document.documentElement.dataset['theme']).toBe('dark');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(localStorage.getItem(PALETTE_STORAGE_KEY)).toBe('colour-blind');
  });

  it('shows default for a corrupt stored palette', () => {
    localStorage.setItem(PALETTE_STORAGE_KEY, 'sepia');
    render(<AppearanceCard />);
    expect(screen.getByRole('radio', { name: /^Default/ })).toBeChecked();
  });
});
