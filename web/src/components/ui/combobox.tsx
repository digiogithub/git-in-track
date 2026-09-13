/**
 * Accessible typeahead (story GIT-US-0055, task GIT-T-0054).
 *
 * The mechanics were hand-rolled once, in the editor's `ItemPicker`, and a
 * second picker — the YouTrack project autosuggest — would have copied them.
 * They live here instead, generic over the option type: the `role="combobox"`
 * input with `aria-expanded`, `aria-controls` and `aria-autocomplete="list"`,
 * the `role="listbox"` popup, the debounce that turns typing into one search,
 * keyboard navigation and the blur grace that lets a click on an option land
 * before the list closes.
 *
 * There is deliberately no `cmdk` and no shadcn `Command` behind it: AGENTS.md
 * asks for a justification before a dependency, and the accessible behaviour
 * this needs already existed in the repository.
 *
 * The component owns the interaction and nothing else. Fetching stays with the
 * caller, which is what keeps one TanStack Query cache per picker: the debounced
 * text arrives through `onSearchChange`, the open state through `onOpenChange`
 * (so a query can be `enabled` only while the list is visible), and the options
 * come back as a plain array.
 */

import { useEffect, useId, useRef, useState, type ReactNode } from 'react';

import { Input } from '@/components/ui/input';
import { cn } from '@/lib/cn';

/** How long typing is coalesced for before a search is issued. */
export const COMBOBOX_DEBOUNCE_MS = 200;

/**
 * How long a blur waits before closing the list. A click on an option blurs the
 * input first, so closing immediately would cancel the click.
 */
const BLUR_GRACE_MS = 150;

export type ComboboxProps<T> = {
  /** The input's id, so a `<Label htmlFor>` points at it. */
  id: string;
  /** Accessible name of the input and of the suggestion list. */
  label: string;
  /** The text in the input. Controlled: the caller decides what typing means. */
  value: string;
  onValueChange: (value: string) => void;
  /** The suggestions to render; the caller fetches them. */
  options: T[];
  /** Stable identity of an option, used as the React key and the option id. */
  getOptionKey: (option: T) => string;
  renderOption: (option: T) => ReactNode;
  /** Whether an option is the current selection, for `aria-selected`. */
  isOptionSelected?: (option: T) => boolean;
  onSelect: (option: T) => void;
  /** The input text, debounced by `debounceMs` and trimmed. */
  onSearchChange?: (search: string) => void;
  /** Open on focus, closed on Escape or on blur. */
  onOpenChange?: (open: boolean) => void;
  debounceMs?: number;
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  /** Rendered next to the input: a clear button, a status badge. */
  trailing?: ReactNode;
  /** Shown in place of the list when a search came back with nothing. */
  emptyLabel?: string;
  /** True while the caller is fetching, so the list can say so. */
  loading?: boolean;
};

/**
 * A text input with a suggestion list. Generic over the option type and free of
 * any knowledge of what is being picked.
 */
export function Combobox<T>({
  id,
  label,
  value,
  onValueChange,
  options,
  getOptionKey,
  renderOption,
  isOptionSelected,
  onSelect,
  onSearchChange,
  onOpenChange,
  debounceMs = COMBOBOX_DEBOUNCE_MS,
  placeholder,
  disabled = false,
  className,
  trailing,
  emptyLabel,
  loading = false,
}: ComboboxProps<T>) {
  const [open, setOpenState] = useState(false);
  /** Index of the keyboard-highlighted option; -1 while none is. */
  const [active, setActive] = useState(-1);
  const listId = useId();
  const blurTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const setOpen = (next: boolean) => {
    setOpenState((current) => {
      if (current !== next) onOpenChange?.(next);
      return next;
    });
    if (!next) setActive(-1);
  };

  useEffect(() => {
    if (onSearchChange === undefined) return;
    const timer = setTimeout(() => {
      onSearchChange(value.trim());
    }, debounceMs);
    return () => {
      clearTimeout(timer);
    };
    // `onSearchChange` is an inline closure in every caller; re-arming the
    // timer on each render of the parent would defeat the debounce entirely.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, debounceMs]);

  // A shorter list must never keep a highlight past its end.
  useEffect(() => {
    setActive((current) => (current >= options.length ? -1 : current));
  }, [options.length]);

  useEffect(
    () => () => {
      if (blurTimer.current !== null) clearTimeout(blurTimer.current);
    },
    [],
  );

  const optionId = (index: number) => `${listId}-option-${String(index)}`;
  const showList = open && (options.length > 0 || (emptyLabel !== undefined && !loading));

  const choose = (option: T) => {
    onSelect(option);
    setOpen(false);
  };

  return (
    <div className={cn('relative', className)}>
      <div className="flex items-center gap-1">
        <Input
          id={id}
          role="combobox"
          aria-expanded={open}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-label={label}
          {...(active >= 0 && active < options.length
            ? { 'aria-activedescendant': optionId(active) }
            : {})}
          autoComplete="off"
          value={value}
          disabled={disabled}
          placeholder={placeholder}
          onChange={(event) => {
            onValueChange(event.target.value);
            setOpen(true);
          }}
          onFocus={() => {
            setOpen(true);
          }}
          onClick={() => {
            // Re-opens after Escape, when the input already holds the focus and
            // no focus event is coming.
            setOpen(true);
          }}
          onKeyDown={(event) => {
            switch (event.key) {
              case 'Escape':
                setOpen(false);
                return;
              case 'ArrowDown':
                if (options.length === 0) return;
                event.preventDefault();
                setOpen(true);
                setActive((current) => (current + 1) % options.length);
                return;
              case 'ArrowUp':
                if (options.length === 0) return;
                event.preventDefault();
                setOpen(true);
                setActive((current) => (current <= 0 ? options.length - 1 : current - 1));
                return;
              case 'Enter': {
                const option = active >= 0 ? options[active] : undefined;
                if (!open || option === undefined) return;
                // Enter picks the highlighted option rather than submitting the
                // form the picker sits in.
                event.preventDefault();
                choose(option);
                return;
              }
              default:
                return;
            }
          }}
          onBlur={() => {
            blurTimer.current = setTimeout(() => {
              setOpen(false);
            }, BLUR_GRACE_MS);
          }}
        />
        {trailing}
      </div>

      {showList ? (
        <ul
          id={listId}
          role="listbox"
          aria-label={`${label} suggestions`}
          className="absolute z-20 mt-1 max-h-56 w-full overflow-auto rounded-md border border-border bg-popover p-1 shadow-pop"
        >
          {options.length === 0 ? (
            <li className="px-2 py-1 text-sm text-muted-foreground">{emptyLabel}</li>
          ) : (
            options.map((option, index) => (
              <li
                key={getOptionKey(option)}
                id={optionId(index)}
                role="option"
                aria-selected={isOptionSelected?.(option) ?? index === active}
              >
                <button
                  type="button"
                  tabIndex={-1}
                  className={cn(
                    'w-full rounded px-2 py-1 text-left text-sm hover:bg-secondary',
                    index === active && 'bg-secondary',
                  )}
                  onMouseDown={(event) => {
                    // Keep the input focused so the blur grace never races the
                    // click that is about to land.
                    event.preventDefault();
                  }}
                  onClick={() => {
                    choose(option);
                  }}
                >
                  {renderOption(option)}
                </button>
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  );
}
