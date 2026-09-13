/**
 * The options panel of the import dialog (story GIT-US-0059, task GIT-T-0104).
 *
 * Five switches and a stepper, and the only interesting one is the depth. An
 * import is a graph walk, and "import these three issues" and "import these
 * three issues and everything under them" are wildly different amounts of
 * writing — so the recursion is an explicit number the user sets, not a
 * checkbox whose meaning they have to guess, and 0 (the selected issues and
 * nothing else) is the default.
 *
 * The sixth control is disabled on purpose. Landing an import in the Inbox
 * instead of straight in the backlog is the shape the Inbox epic gives it, and
 * a control that is *visibly* not available yet says that far better than a
 * control that is simply missing — which reads as an option nobody thought of.
 */

import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { IMPORT_MAX_DEPTH, type ImportOptionsDraft } from '@/features/youtrack/import-model';

/** Why the Inbox option is there and not usable. */
const INBOX_REASON =
  'Landing an import in the Inbox instead of the backlog arrives with the Inbox epic; today every imported issue is created in the backlog.';

export type ImportOptionsProps = {
  value: ImportOptionsDraft;
  onChange: (value: ImportOptionsDraft) => void;
  /** Locks the panel while a preview or a run is in flight. */
  disabled?: boolean;
};

export function ImportOptions({ value, onChange, disabled = false }: ImportOptionsProps) {
  const set = (patch: Partial<ImportOptionsDraft>) => {
    onChange({ ...value, ...patch });
  };

  /** The stepper clamps rather than refuses: there is nothing to explain. */
  const setDepth = (next: number) => {
    set({ depth: Math.min(IMPORT_MAX_DEPTH, Math.max(0, Number.isNaN(next) ? 0 : next)) });
  };

  return (
    <fieldset className="space-y-3" disabled={disabled}>
      <legend className="section-label">Options</legend>

      <div className="flex flex-wrap items-center gap-2">
        <Label htmlFor="import-depth">Subtask depth</Label>
        <Input
          id="import-depth"
          type="number"
          inputMode="numeric"
          min={0}
          max={IMPORT_MAX_DEPTH}
          value={value.depth}
          onChange={(event) => {
            setDepth(Number.parseInt(event.target.value, 10));
          }}
          className="h-8 w-20"
        />
        <span className="text-xs text-muted-foreground">
          {value.depth === 0
            ? 'The selected issues only.'
            : `Their subtasks, ${String(value.depth)} level${value.depth === 1 ? '' : 's'} down.`}
        </span>
      </div>

      <OptionRow
        id="import-links"
        label="Include linked issues"
        hint="Non-hierarchy relations become links[] on the item."
        checked={value.includeLinks}
        onChange={(checked) => {
          set({ includeLinks: checked });
        }}
      />
      <OptionRow
        id="import-comments"
        label="Include comments"
        hint="Each comment becomes a comment file, keeping its original author and time."
        checked={value.includeComments}
        onChange={(checked) => {
          set({ includeComments: checked });
        }}
      />
      <OptionRow
        id="import-attachments"
        label="Include attachments"
        hint="Attachment paths are recorded on the item; the files are fetched by the job engine."
        checked={value.includeAttachments}
        onChange={(checked) => {
          set({ includeAttachments: checked });
        }}
      />

      <Tooltip>
        <TooltipTrigger asChild>
          {/* A disabled input fires no pointer events, so the tooltip belongs
              to the row around it. A keyboard user never has to find that row:
              the reason is on the checkbox itself through `aria-describedby`. */}
          <div className="flex items-start gap-2 opacity-60">
            <Checkbox
              id="import-inbox"
              checked={false}
              disabled
              readOnly
              aria-describedby="import-inbox-reason"
              className="mt-0.5"
            />
            <div>
              <Label htmlFor="import-inbox" className="text-subtle-foreground">
                Land in Inbox (not available yet)
              </Label>
              <span id="import-inbox-reason" className="sr-only">
                {INBOX_REASON}
              </span>
            </div>
          </div>
        </TooltipTrigger>
        <TooltipContent>{INBOX_REASON}</TooltipContent>
      </Tooltip>
    </fieldset>
  );
}

function OptionRow({
  id,
  label,
  hint,
  checked,
  onChange,
}: {
  id: string;
  label: string;
  hint: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-start gap-2">
      <Checkbox
        id={id}
        checked={checked}
        onChange={(event) => {
          onChange(event.target.checked);
        }}
        className="mt-0.5"
      />
      <div>
        <Label htmlFor={id}>{label}</Label>
        <p className="text-xs text-muted-foreground">{hint}</p>
      </div>
    </div>
  );
}
