import { X } from 'lucide-react';
import { useEffect, useId, useState } from 'react';

import type { Item, ProjectSummary } from '@/api/provider';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { useIdentity } from '@/features/backlog/identity';
import { typeName } from '@/features/backlog/item-meta';
import {
  filterableItemTypes,
  isEmptySearch,
  statusCategories,
  type ItemSearch,
} from '@/features/backlog/search';
import { useSetItemSearch } from '@/features/backlog/use-search';

type Option = { value: string; label: string };

/**
 * Multi-value filter as a disclosure of checkboxes.
 *
 * A native `<details>` plus real checkboxes keeps the control keyboard- and
 * screen-reader-correct without a popover library, and every option stays in
 * the accessibility tree for testing.
 */
function MultiSelectFilter({
  label,
  options,
  selected,
  onChange,
}: {
  label: string;
  options: Option[];
  selected: string[];
  onChange: (next: string[]) => void;
}) {
  const groupId = useId();

  const toggle = (value: string, checked: boolean) => {
    onChange(checked ? [...selected, value] : selected.filter((entry) => entry !== value));
  };

  return (
    <details className="relative">
      <summary className="flex h-9 cursor-pointer list-none items-center gap-1 rounded-md border border-input bg-background px-3 text-sm shadow-sm marker:hidden hover:bg-secondary">
        {label}
        {selected.length > 0 ? (
          <span className="rounded-full bg-accent/15 px-1.5 text-xs text-accent">
            {selected.length}
          </span>
        ) : null}
      </summary>
      <div className="absolute left-0 z-20 mt-1 max-h-72 w-56 overflow-y-auto rounded-md border border-border bg-popover p-2 shadow-md">
        <fieldset>
          <legend className="sr-only">{label}</legend>
          {options.length === 0 ? (
            <p className="px-1 py-2 text-xs text-muted-foreground">Nothing to filter by yet.</p>
          ) : null}
          {options.map((option) => {
            const id = `${groupId}-${option.value}`;
            return (
              <div key={option.value} className="flex items-center gap-2 rounded px-1 py-1">
                <Checkbox
                  id={id}
                  checked={selected.includes(option.value)}
                  onChange={(event) => {
                    toggle(option.value, event.target.checked);
                  }}
                />
                <label htmlFor={id} className="cursor-pointer text-sm">
                  {option.label}
                </label>
              </div>
            );
          })}
        </fieldset>
      </div>
    </details>
  );
}

export type FilterBarProps = {
  search: ItemSearch;
  project: ProjectSummary | undefined;
  milestones: Item[];
};

/** Filters bar. Every control writes straight into the URL search params. */
export function FilterBar({ search, project, milestones }: FilterBarProps) {
  const setSearch = useSetItemSearch();
  const { remember } = useIdentity();
  const [text, setText] = useState(search.q ?? '');
  const searchId = useId();
  const assigneeId = useId();
  const parentId = useId();
  const categoryId = useId();
  const milestoneId = useId();

  // Keep the input in sync when the URL changes from elsewhere (quick views).
  useEffect(() => {
    setText(search.q ?? '');
  }, [search.q]);

  // Debounce typing so every keystroke is not a navigation.
  useEffect(() => {
    const current = search.q ?? '';
    if (text === current) return;
    const handle = setTimeout(() => {
      setSearch({ q: text.length > 0 ? text : undefined, view: undefined });
    }, 250);
    return () => {
      clearTimeout(handle);
    };
  }, [text, search.q, setSearch]);

  const statusOptions: Option[] = (project?.statuses ?? []).map((status) => ({
    value: status.id,
    label: status.name,
  }));
  const labelOptions: Option[] = (project?.labels ?? []).map((label) => ({
    value: label.name,
    label: label.name,
  }));
  const typeOptions: Option[] = filterableItemTypes.map((type) => ({
    value: type,
    label: typeName(type),
  }));

  const join = (values: string[]) => (values.length > 0 ? values.join(',') : undefined);

  return (
    <div className="flex flex-wrap items-end gap-2">
      <div className="min-w-56 flex-1">
        <label htmlFor={searchId} className="mb-1 block text-xs text-muted-foreground">
          Search
        </label>
        <Input
          id={searchId}
          type="search"
          value={text}
          placeholder="Title, id or body…"
          onChange={(event) => {
            setText(event.target.value);
          }}
        />
      </div>

      <MultiSelectFilter
        label="Type"
        options={typeOptions}
        selected={search.type ?? []}
        onChange={(next) => {
          setSearch({ type: join(next), view: undefined });
        }}
      />

      <MultiSelectFilter
        label="Status"
        options={statusOptions}
        selected={search.status ?? []}
        onChange={(next) => {
          setSearch({ status: join(next), view: undefined });
        }}
      />

      <MultiSelectFilter
        label="Label"
        options={labelOptions}
        selected={search.label ?? []}
        onChange={(next) => {
          setSearch({ label: join(next), view: undefined });
        }}
      />

      <div>
        <label htmlFor={categoryId} className="mb-1 block text-xs text-muted-foreground">
          Category
        </label>
        <Select
          id={categoryId}
          className="w-36"
          value={search.category ?? ''}
          onChange={(event) => {
            setSearch({ category: event.target.value || undefined, view: undefined });
          }}
        >
          <option value="">Any</option>
          {statusCategories.map((category) => (
            <option key={category} value={category}>
              {category.replace('_', ' ')}
            </option>
          ))}
        </Select>
      </div>

      <div>
        <label htmlFor={assigneeId} className="mb-1 block text-xs text-muted-foreground">
          Assignee
        </label>
        <Input
          id={assigneeId}
          className="w-36"
          value={search.assignee ?? ''}
          placeholder="handle"
          onChange={(event) => {
            const value = event.target.value;
            remember(value);
            setSearch({ assignee: value || undefined, view: undefined });
          }}
        />
      </div>

      <div>
        <label htmlFor={milestoneId} className="mb-1 block text-xs text-muted-foreground">
          Milestone
        </label>
        <Select
          id={milestoneId}
          className="w-44"
          value={search.milestone ?? ''}
          onChange={(event) => {
            setSearch({ milestone: event.target.value || undefined, view: undefined });
          }}
        >
          <option value="">Any</option>
          {milestones.map((milestone) => (
            <option key={milestone.id} value={milestone.id}>
              {milestone.title}
            </option>
          ))}
        </Select>
      </div>

      <div>
        <label htmlFor={parentId} className="mb-1 block text-xs text-muted-foreground">
          Parent
        </label>
        <Input
          id={parentId}
          className="w-40 font-mono"
          value={search.parent ?? ''}
          placeholder="ACME-EP-0001"
          onChange={(event) => {
            setSearch({ parent: event.target.value || undefined, view: undefined });
          }}
        />
      </div>

      {!isEmptySearch(search) ? (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setSearch({
              q: undefined,
              type: undefined,
              status: undefined,
              category: undefined,
              label: undefined,
              assignee: undefined,
              milestone: undefined,
              parent: undefined,
              view: undefined,
            });
          }}
        >
          <X aria-hidden="true" className="h-3.5 w-3.5" />
          Clear filters
        </Button>
      ) : null}
    </div>
  );
}
