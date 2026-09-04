import { useQuery } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';

import type { ItemFilter, ItemType } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/cn';

export type ItemPickerProps = {
  id: string;
  label: string;
  value: string | null;
  onChange: (value: string | null) => void;
  projectKey: string;
  /** Types the picker searches: `epic` for a story's parent, `story` for a task's. */
  types: ItemType[];
  placeholder?: string;
  disabled?: boolean;
  className?: string;
};

const debounceMs = 200;

/**
 * Typeahead over the index: type an id directly or pick one from the list.
 * The value is always an item id, never a title.
 */
export function ItemPicker({
  id,
  label,
  value,
  onChange,
  projectKey,
  types,
  placeholder,
  disabled = false,
  className,
}: ItemPickerProps) {
  const provider = useProvider();
  const [text, setText] = useState(value ?? '');
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const listId = useId();
  const blurTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    setText(value ?? '');
  }, [value]);

  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(text.trim());
    }, debounceMs);
    return () => {
      clearTimeout(timer);
    };
  }, [text]);

  useEffect(
    () => () => {
      if (blurTimer.current !== null) clearTimeout(blurTimer.current);
    },
    [],
  );

  const firstType = types[0];
  const filter: ItemFilter = {
    project: projectKey,
    type: types.length === 1 && firstType ? firstType : types,
    limit: 10,
    sort: 'id',
    order: 'asc',
    ...(search ? { text: search } : {}),
  };

  const { data } = useQuery({
    queryKey: ['items', projectKey, 'list', 'picker', types.join(','), search],
    queryFn: () => provider.listItems(filter),
    enabled: open && projectKey.length > 0,
  });

  const options = data?.items ?? [];

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
          autoComplete="off"
          value={text}
          disabled={disabled}
          placeholder={placeholder ?? 'Search by id or title'}
          onChange={(event) => {
            const next = event.target.value;
            setText(next);
            setOpen(true);
            onChange(next.trim() === '' ? null : next.trim());
          }}
          onFocus={() => {
            setOpen(true);
          }}
          onKeyDown={(event) => {
            if (event.key === 'Escape') setOpen(false);
          }}
          onBlur={() => {
            blurTimer.current = setTimeout(() => {
              setOpen(false);
            }, 150);
          }}
        />
        {value ? (
          <button
            type="button"
            aria-label={`Clear ${label}`}
            disabled={disabled}
            className="rounded-md p-1 text-muted-foreground hover:text-destructive"
            onClick={() => {
              setText('');
              onChange(null);
            }}
          >
            <X aria-hidden="true" className="h-4 w-4" />
          </button>
        ) : null}
      </div>

      {open && options.length > 0 ? (
        <ul
          id={listId}
          role="listbox"
          aria-label={`${label} suggestions`}
          className="absolute z-20 mt-1 max-h-56 w-full overflow-auto rounded-md border border-border bg-popover p-1 shadow-md"
        >
          {options.map((option) => (
            <li key={option.id} role="option" aria-selected={option.id === value}>
              <button
                type="button"
                className="w-full rounded px-2 py-1 text-left text-sm hover:bg-secondary"
                onMouseDown={(event) => {
                  event.preventDefault();
                }}
                onClick={() => {
                  setText(option.id);
                  onChange(option.id);
                  setOpen(false);
                }}
              >
                <span className="font-mono text-xs text-muted-foreground">{option.id}</span>{' '}
                {option.title}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
