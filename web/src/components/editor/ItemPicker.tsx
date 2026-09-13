import { useQuery } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useEffect, useState } from 'react';

import type { Item, ItemFilter, ItemType } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Combobox } from '@/components/ui/combobox';

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

/**
 * Typeahead over the index: type an id directly or pick one from the list.
 * The value is always an item id, never a title.
 *
 * The interaction — ARIA roles, debounce, keyboard and blur grace — lives in
 * `components/ui/combobox`; what stays here is what makes this picker an
 * *item* picker: the query, the filter and the id-shaped value.
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

  useEffect(() => {
    setText(value ?? '');
  }, [value]);

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
    <Combobox<Item>
      id={id}
      label={label}
      value={text}
      onValueChange={(next) => {
        setText(next);
        onChange(next.trim() === '' ? null : next.trim());
      }}
      onSearchChange={setSearch}
      onOpenChange={setOpen}
      options={options}
      getOptionKey={(option) => option.id}
      isOptionSelected={(option) => option.id === value}
      renderOption={(option) => (
        <>
          <span className="font-mono text-xs text-muted-foreground">{option.id}</span>{' '}
          {option.title}
        </>
      )}
      onSelect={(option) => {
        setText(option.id);
        onChange(option.id);
      }}
      placeholder={placeholder ?? 'Search by id or title'}
      disabled={disabled}
      {...(className === undefined ? {} : { className })}
      trailing={
        value ? (
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
        ) : null
      }
    />
  );
}
