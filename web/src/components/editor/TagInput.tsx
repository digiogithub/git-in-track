import { X } from 'lucide-react';
import { useId, useState, type KeyboardEvent } from 'react';

import { Input } from '@/components/ui/input';
import { cn } from '@/lib/cn';

export type TagInputProps = {
  id: string;
  label: string;
  values: string[];
  onChange: (values: string[]) => void;
  /** Offered through a `<datalist>`; free entries are always allowed. */
  suggestions?: string[];
  placeholder?: string;
  disabled?: boolean;
  className?: string;
};

/** Free-form tag list (assignees, labels) with an optional catalog. */
export function TagInput({
  id,
  label,
  values,
  onChange,
  suggestions = [],
  placeholder,
  disabled = false,
  className,
}: TagInputProps) {
  const [draft, setDraft] = useState('');
  const listId = useId();

  const commit = (raw: string) => {
    const next = raw.trim();
    if (next === '' || values.includes(next)) {
      setDraft('');
      return;
    }
    onChange([...values, next]);
    setDraft('');
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault();
      commit(draft);
      return;
    }
    if (event.key === 'Backspace' && draft === '' && values.length > 0) {
      onChange(values.slice(0, -1));
    }
  };

  return (
    <div className={cn('space-y-2', className)}>
      <ul className="flex flex-wrap gap-1" aria-label={`${label} selected`}>
        {values.map((value) => (
          <li
            key={value}
            className="inline-flex items-center gap-1 rounded-full bg-secondary px-2 py-0.5 text-xs text-secondary-foreground"
          >
            {value}
            <button
              type="button"
              aria-label={`Remove ${value} from ${label}`}
              disabled={disabled}
              onClick={() => {
                onChange(values.filter((v) => v !== value));
              }}
              className="rounded-full hover:text-destructive"
            >
              <X aria-hidden="true" className="h-3 w-3" />
            </button>
          </li>
        ))}
      </ul>
      <Input
        id={id}
        value={draft}
        list={suggestions.length > 0 ? listId : undefined}
        placeholder={placeholder ?? 'Type and press Enter'}
        disabled={disabled}
        onChange={(event) => {
          setDraft(event.target.value);
        }}
        onKeyDown={onKeyDown}
        onBlur={() => {
          commit(draft);
        }}
      />
      {suggestions.length > 0 ? (
        <datalist id={listId}>
          {suggestions.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
      ) : null}
    </div>
  );
}
