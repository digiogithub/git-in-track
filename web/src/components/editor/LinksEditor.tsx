import { Plus, Trash2 } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import type { Link, LinkKind } from '@/core-bridge/api';
import { linkKinds } from '@/features/editor/front-matter';
import { cn } from '@/lib/cn';

export type LinksEditorProps = {
  links: Link[];
  onChange: (links: Link[]) => void;
  disabled?: boolean;
  className?: string;
};

const selectClass =
  'h-9 rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring';

/** Typed relations (docs/03-data-model.md §12): a kind plus a target item id. */
export function LinksEditor({ links, onChange, disabled = false, className }: LinksEditorProps) {
  const update = (index: number, next: Partial<Link>) => {
    onChange(links.map((link, i) => (i === index ? { ...link, ...next } : link)));
  };

  return (
    <div className={cn('space-y-2', className)}>
      {links.map((link, index) => (
        <div key={`${link.kind}-${link.target}-${index}`} className="flex items-center gap-2">
          <select
            className={selectClass}
            aria-label={`Link ${index + 1} kind`}
            value={link.kind}
            disabled={disabled}
            onChange={(event) => {
              update(index, { kind: event.target.value as LinkKind });
            }}
          >
            {linkKinds.map((kind) => (
              <option key={kind} value={kind}>
                {kind}
              </option>
            ))}
          </select>
          <Input
            aria-label={`Link ${index + 1} target`}
            value={link.target}
            disabled={disabled}
            placeholder="ACME-US-0042"
            onChange={(event) => {
              update(index, { target: event.target.value });
            }}
          />
          <Button
            variant="ghost"
            size="icon"
            aria-label={`Remove link ${index + 1}`}
            disabled={disabled}
            onClick={() => {
              onChange(links.filter((_, i) => i !== index));
            }}
          >
            <Trash2 aria-hidden="true" className="h-4 w-4" />
          </Button>
        </div>
      ))}
      <Button
        variant="outline"
        size="sm"
        disabled={disabled}
        onClick={() => {
          onChange([...links, { kind: 'relates_to', target: '' }]);
        }}
      >
        <Plus aria-hidden="true" className="h-4 w-4" />
        Add link
      </Button>
    </div>
  );
}
