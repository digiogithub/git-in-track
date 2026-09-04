/**
 * Front matter as a compact metadata card.
 *
 * The provider already parsed the YAML (`KbPage.frontMatter`), so this is a
 * presentation concern only: `title` is the page heading and is not repeated,
 * arrays render as chips, everything else as a value.
 */

import type { KbPage } from '@/api/provider';
import { Card, CardContent } from '@/components/ui/card';

export type KbFrontMatterProps = {
  page: KbPage;
};

function formatValue(value: unknown): string {
  if (value === null || value === undefined) return '—';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return value.toString();
  return JSON.stringify(value) ?? '';
}

export function KbFrontMatter({ page }: KbFrontMatterProps) {
  const entries = Object.entries(page.frontMatter).filter(([key]) => key !== 'title');
  if (entries.length === 0) return null;

  return (
    <Card>
      <CardContent className="p-4">
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
          {entries.map(([key, value]) => (
            <div key={key} className="contents">
              <dt className="text-muted-foreground">{key}</dt>
              <dd className="min-w-0">
                {Array.isArray(value) ? (
                  <span className="flex flex-wrap gap-1">
                    {value.map((entry, index) => (
                      <span
                        key={`${key}-${index}`}
                        className="rounded bg-secondary px-1.5 py-0.5 text-xs"
                      >
                        {formatValue(entry)}
                      </span>
                    ))}
                  </span>
                ) : (
                  <span className="break-words">{formatValue(value)}</span>
                )}
              </dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}
