/**
 * The issue picker of the import dialog (story GIT-US-0059, task GIT-T-0099).
 *
 * It is a typeahead over the linked YouTrack project plus five saved queries,
 * because the alternative is asking a person to remember a query language to
 * answer "which of my issues do I want here". The preset chips are the common
 * questions; the text box is the escape hatch, and the two compose — a preset
 * narrows what typing searches.
 *
 * Selection is multiple and additive, and the running count is always on
 * screen: an import is a batch, and the thing a person needs to know before
 * pressing preview is how big the batch is.
 *
 * A result that a previous import already created is marked with the
 * git-in-track id it became and stays selectable. That is not an edge case but
 * the normal second import: re-selecting it is how an issue gets refreshed, and
 * the preview will say `update` rather than `create`.
 *
 * The accessible mechanics are not re-implemented here: `Combobox` owns the
 * `role="combobox"` input, the `role="listbox"` popup, keyboard navigation and
 * the blur grace. The debounce is not its either — it lives in the search hook,
 * so the preset chips and the keyboard share one timer and one query key.
 */

import { Check, X } from 'lucide-react';

import type { YouTrackIssue, YouTrackIssuePreset, YouTrackScope } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Combobox } from '@/components/ui/combobox';
import { youtrackMessage } from '@/features/settings/youtrack-messages';
import { IMPORT_PRESETS } from '@/features/youtrack/import-model';
import { useYouTrackIssueSearch } from '@/features/youtrack/queries';
import { cn } from '@/lib/cn';

export type ImportQueryBarProps = {
  /** What the user typed. Controlled: the dialog owns the whole draft. */
  text: string;
  onTextChange: (text: string) => void;
  preset: YouTrackIssuePreset;
  onPresetChange: (preset: YouTrackIssuePreset) => void;
  /** The issues picked so far, in the order they were picked. */
  selected: YouTrackIssue[];
  onSelectedChange: (selected: YouTrackIssue[]) => void;
  scope?: YouTrackScope;
  /** Injected by tests; production waits the hook's default. */
  debounceMs?: number;
};

export function ImportQueryBar({
  text,
  onTextChange,
  preset,
  onPresetChange,
  selected,
  onSelectedChange,
  scope,
  debounceMs,
}: ImportQueryBarProps) {
  const search = useYouTrackIssueSearch({
    q: text,
    preset,
    ...(scope === undefined ? {} : { scope }),
    ...(debounceMs === undefined ? {} : { debounceMs }),
  });

  const picked = new Set(selected.map((issue) => issue.idReadable));
  const options = search.data?.items ?? [];

  /** Picking an issue twice removes it: the list is the only selection UI. */
  const toggle = (issue: YouTrackIssue) => {
    onSelectedChange(
      picked.has(issue.idReadable)
        ? selected.filter((row) => row.idReadable !== issue.idReadable)
        : [...selected, issue],
    );
  };

  return (
    <div className="space-y-3">
      <div role="group" aria-label="Issue presets" className="flex flex-wrap items-center gap-1.5">
        {IMPORT_PRESETS.map((option) => {
          const active = option.value === preset;
          return (
            <button
              key={option.value === '' ? 'any' : option.value}
              type="button"
              aria-pressed={active}
              onClick={() => {
                // Switching preset starts the selection's paging again: the
                // cursor of one query means nothing to another.
                onPresetChange(option.value);
              }}
              className={cn(
                'rounded-full border px-2.5 py-0.5 text-xs font-medium transition-colors duration-fast',
                active
                  ? 'border-transparent bg-accent/15 text-accent'
                  : 'border-border-strong text-muted-foreground hover:bg-secondary hover:text-foreground',
              )}
            >
              {option.label}
            </button>
          );
        })}
      </div>

      <Combobox<YouTrackIssue>
        id="youtrack-import-search"
        label="Search YouTrack issues"
        placeholder="Search by id or summary"
        value={text}
        onValueChange={onTextChange}
        options={options}
        // The debounce lives in the search hook, so the picker must not add a
        // second one: two timers would make one keystroke cost 400 ms.
        debounceMs={0}
        loading={search.isFetching}
        emptyLabel={search.isFetching ? 'Searching…' : 'No issue matches this search.'}
        getOptionKey={(issue) => issue.idReadable}
        isOptionSelected={(issue) => picked.has(issue.idReadable)}
        onSelect={toggle}
        renderOption={(issue) => <IssueRow issue={issue} picked={picked.has(issue.idReadable)} />}
      />

      {search.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {youtrackMessage(search.error)}
        </p>
      ) : null}

      <div className="space-y-2">
        <p className="section-label" data-testid="import-selected-count">
          {selected.length === 0
            ? 'No issue selected'
            : `${String(selected.length)} issue${selected.length === 1 ? '' : 's'} selected`}
        </p>
        {selected.length > 0 ? (
          <ul className="flex flex-wrap gap-1.5">
            {selected.map((issue) => (
              <li key={issue.idReadable}>
                <span className="inline-flex items-center gap-1 rounded-full bg-secondary px-2 py-0.5 text-xs">
                  <span className="font-mono">{issue.idReadable}</span>
                  <button
                    type="button"
                    aria-label={`Remove ${issue.idReadable} from the selection`}
                    onClick={() => {
                      toggle(issue);
                    }}
                    className="rounded-sm text-subtle-foreground transition-colors duration-fast hover:text-foreground"
                  >
                    <X aria-hidden="true" className="h-3 w-3" />
                  </button>
                </span>
              </li>
            ))}
            <li>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  onSelectedChange([]);
                }}
              >
                Clear selection
              </Button>
            </li>
          </ul>
        ) : null}
      </div>
    </div>
  );
}

/**
 * One suggestion. Everything on this row is text the instance wrote, so it is
 * rendered as text and nothing else.
 */
function IssueRow({ issue, picked }: { issue: YouTrackIssue; picked: boolean }) {
  return (
    <span className="flex items-start gap-2">
      <Check
        aria-hidden="true"
        className={cn('mt-0.5 h-3.5 w-3.5 shrink-0', picked ? 'text-accent' : 'invisible')}
      />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-1.5">
          <span className="font-mono text-xs text-muted-foreground">{issue.idReadable}</span>
          <span className="truncate">{issue.summary}</span>
        </span>
        <span className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
          {issue.type === '' ? null : <span>{issue.type}</span>}
          {issue.state === '' ? null : <span>· {issue.state}</span>}
          {issue.linked === null ? null : (
            <Badge variant="info" size="sm" title="A previous import already created this item">
              Imported as {issue.linked.itemId}
            </Badge>
          )}
        </span>
      </span>
    </span>
  );
}
