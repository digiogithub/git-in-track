import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { Combobox } from '@/components/ui/combobox';

type Fruit = { id: string; name: string };

const FRUITS: Fruit[] = [
  { id: 'a', name: 'Apricot' },
  { id: 'b', name: 'Blackberry' },
  { id: 'c', name: 'Cherry' },
];

function Harness({
  options = FRUITS,
  onSelect = vi.fn(),
  onSearchChange,
  debounceMs,
}: {
  options?: Fruit[];
  onSelect?: (option: Fruit) => void;
  onSearchChange?: (search: string) => void;
  debounceMs?: number;
}) {
  const [value, setValue] = useState('');
  return (
    <Combobox<Fruit>
      id="fruit"
      label="Fruit"
      value={value}
      onValueChange={setValue}
      options={options}
      getOptionKey={(option) => option.id}
      renderOption={(option) => option.name}
      onSelect={(option) => {
        setValue(option.name);
        onSelect(option);
      }}
      {...(onSearchChange === undefined ? {} : { onSearchChange })}
      {...(debounceMs === undefined ? {} : { debounceMs })}
      emptyLabel="No fruit"
    />
  );
}

const input = () => screen.getByRole('combobox', { name: 'Fruit' });

describe('Combobox', () => {
  it('carries the ARIA contract of a typeahead', async () => {
    render(<Harness />);
    const user = userEvent.setup();

    expect(input()).toHaveAttribute('aria-autocomplete', 'list');
    expect(input()).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('listbox')).toBeNull();

    await user.click(input());

    expect(input()).toHaveAttribute('aria-expanded', 'true');
    const list = screen.getByRole('listbox', { name: 'Fruit suggestions' });
    expect(input().getAttribute('aria-controls')).toBe(list.getAttribute('id'));
    expect(screen.getAllByRole('option')).toHaveLength(3);
  });

  it('walks the list with the arrow keys and picks with Enter', async () => {
    const onSelect = vi.fn();
    render(<Harness onSelect={onSelect} />);
    const user = userEvent.setup();

    await user.click(input());
    await user.keyboard('{ArrowDown}{ArrowDown}');

    const options = screen.getAllByRole('option');
    expect(input()).toHaveAttribute('aria-activedescendant', options[1]?.getAttribute('id'));

    await user.keyboard('{Enter}');

    expect(onSelect).toHaveBeenCalledWith(FRUITS[1]);
    expect(input()).toHaveValue('Blackberry');
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('wraps around at both ends of the list', async () => {
    render(<Harness />);
    const user = userEvent.setup();

    await user.click(input());
    await user.keyboard('{ArrowUp}');

    const options = screen.getAllByRole('option');
    expect(input()).toHaveAttribute('aria-activedescendant', options[2]?.getAttribute('id'));

    await user.keyboard('{ArrowDown}');
    expect(input()).toHaveAttribute('aria-activedescendant', options[0]?.getAttribute('id'));
  });

  it('picks an option with the mouse', async () => {
    const onSelect = vi.fn();
    render(<Harness onSelect={onSelect} />);
    const user = userEvent.setup();

    await user.click(input());
    await user.click(screen.getByRole('button', { name: 'Cherry' }));

    expect(onSelect).toHaveBeenCalledWith(FRUITS[2]);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('closes on Escape and again on blur', async () => {
    render(<Harness />);
    const user = userEvent.setup();

    await user.click(input());
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('listbox')).toBeNull();

    await user.click(input());
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    await user.tab();

    await waitFor(() => {
      expect(screen.queryByRole('listbox')).toBeNull();
    });
  });

  it('debounces the search: typing is one search, not one per keystroke', async () => {
    vi.useFakeTimers();
    try {
      const onSearchChange = vi.fn();
      render(<Harness onSearchChange={onSearchChange} debounceMs={200} />);

      // The mount fires once with the empty initial value.
      await vi.advanceTimersByTimeAsync(200);
      onSearchChange.mockClear();

      fireEvent.focus(input());
      for (const text of ['c', 'ch', 'che']) {
        fireEvent.change(input(), { target: { value: text } });
        await vi.advanceTimersByTimeAsync(50);
      }
      expect(onSearchChange).not.toHaveBeenCalled();

      await vi.advanceTimersByTimeAsync(200);
      expect(onSearchChange).toHaveBeenCalledTimes(1);
      expect(onSearchChange).toHaveBeenCalledWith('che');
    } finally {
      vi.useRealTimers();
    }
  });

  it('says so when a search came back with nothing', async () => {
    render(<Harness options={[]} />);
    const user = userEvent.setup();

    await user.click(input());

    expect(screen.getByText('No fruit')).toBeInTheDocument();
    expect(screen.queryAllByRole('option')).toHaveLength(0);
  });
});
