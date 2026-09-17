import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';

/** One scope a search can be restricted to: a project, or a team's knowledge base. */
export type SearchProjectOption = { key: string; name: string };

export type SearchProjectFilterProps = {
  options: SearchProjectOption[];
  /** The keys the user turned off. Anything not listed is selected. */
  excluded: ReadonlySet<string>;
  onChange: (excluded: Set<string>) => void;
};

/**
 * The project multi-select of the workspace search (story GIT-US-0102).
 *
 * The state is the set of keys turned *off*, so every project is selected by
 * default and a project opened later joins the search without a click.
 */
export function SearchProjectFilter({ options, excluded, onChange }: SearchProjectFilterProps) {
  const selected = options.filter((option) => !excluded.has(option.key)).length;
  return (
    <fieldset className="space-y-2">
      <legend className="sr-only">Projects to search</legend>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs font-medium text-muted-foreground">
          Projects ({selected} of {options.length})
        </span>
        <Button
          size="sm"
          variant="ghost"
          aria-label="Search every project"
          disabled={selected === options.length}
          onClick={() => {
            onChange(new Set());
          }}
        >
          All
        </Button>
        <Button
          size="sm"
          variant="ghost"
          aria-label="Search no project"
          disabled={selected === 0}
          onClick={() => {
            onChange(new Set(options.map((option) => option.key)));
          }}
        >
          None
        </Button>
      </div>
      <ul className="flex flex-wrap gap-x-4 gap-y-1">
        {options.map((option) => (
          <li key={option.key}>
            <label className="flex items-center gap-1.5 text-sm" title={option.name}>
              <Checkbox
                checked={!excluded.has(option.key)}
                onChange={(event) => {
                  const next = new Set(excluded);
                  if (event.target.checked) next.delete(option.key);
                  else next.add(option.key);
                  onChange(next);
                }}
              />
              {option.key}
            </label>
          </li>
        ))}
      </ul>
    </fieldset>
  );
}
