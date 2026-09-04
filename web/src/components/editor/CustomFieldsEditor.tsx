import type { Diagnostic } from '@/api/provider';
import { FieldIssue } from '@/components/editor/DiagnosticList';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import type { CustomFieldDef } from '@/features/editor/project-schema';
import { cn } from '@/lib/cn';

export type CustomFieldsEditorProps = {
  fields: CustomFieldDef[];
  values: Record<string, unknown>;
  onChange: (values: Record<string, unknown>) => void;
  diagnostics?: Diagnostic[];
  disabled?: boolean;
  className?: string;
};

const selectClass =
  'h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring';

function asText(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
}

/** Declared `custom_fields` (docs/03-data-model.md §13.2), stored under `custom:`. */
export function CustomFieldsEditor({
  fields,
  values,
  onChange,
  diagnostics = [],
  disabled = false,
  className,
}: CustomFieldsEditorProps) {
  if (fields.length === 0) return null;

  const set = (key: string, value: unknown) => {
    const next = { ...values };
    if (value === '' || value === undefined || value === null) {
      delete next[key];
    } else {
      next[key] = value;
    }
    onChange(next);
  };

  return (
    <fieldset className={cn('space-y-3', className)}>
      <legend className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        Custom fields
      </legend>
      {fields.map((field) => {
        const inputId = `custom-${field.key}`;
        const value = values[field.key];
        return (
          <div key={field.key} className="space-y-1">
            <Label htmlFor={inputId}>{field.key}</Label>
            {field.type === 'enum' ? (
              <select
                id={inputId}
                className={selectClass}
                disabled={disabled}
                value={asText(value)}
                onChange={(event) => {
                  set(field.key, event.target.value);
                }}
              >
                <option value="">—</option>
                {field.values.map((option) => (
                  <option key={option} value={option}>
                    {option}
                  </option>
                ))}
              </select>
            ) : field.type === 'bool' ? (
              <input
                id={inputId}
                type="checkbox"
                className="h-4 w-4 rounded border-input"
                disabled={disabled}
                checked={value === true}
                onChange={(event) => {
                  set(field.key, event.target.checked ? true : undefined);
                }}
              />
            ) : field.type === 'text' ? (
              <Textarea
                id={inputId}
                disabled={disabled}
                value={asText(value)}
                onChange={(event) => {
                  set(field.key, event.target.value);
                }}
              />
            ) : (
              <Input
                id={inputId}
                type={
                  field.type === 'number'
                    ? 'number'
                    : field.type === 'date'
                      ? 'date'
                      : field.type === 'url'
                        ? 'url'
                        : 'text'
                }
                disabled={disabled}
                value={field.type === 'list' && Array.isArray(value) ? value.join(', ') : asText(value)}
                onChange={(event) => {
                  const raw = event.target.value;
                  if (field.type === 'number') {
                    set(field.key, raw === '' ? undefined : Number(raw));
                    return;
                  }
                  if (field.type === 'list') {
                    set(
                      field.key,
                      raw
                        .split(',')
                        .map((entry) => entry.trim())
                        .filter(Boolean),
                    );
                    return;
                  }
                  set(field.key, raw);
                }}
              />
            )}
            <FieldIssue diagnostics={diagnostics} field={`custom.${field.key}`} />
          </div>
        );
      })}
    </fieldset>
  );
}
