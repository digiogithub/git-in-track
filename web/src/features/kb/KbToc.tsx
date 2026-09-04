/** Table of contents built from the rendered page's headings. */

import { cn } from '@/lib/cn';
import type { Heading } from '@/markdown';

export type KbTocProps = {
  headings: Heading[];
};

export function KbToc({ headings }: KbTocProps) {
  const outline = headings.filter((heading) => heading.depth >= 2 && heading.depth <= 4);
  if (outline.length === 0) return null;

  return (
    <nav aria-labelledby="kb-toc-title" className="space-y-2 text-sm">
      <p id="kb-toc-title" className="font-medium">
        On this page
      </p>
      <ul className="space-y-1 border-l">
        {outline.map((heading) => (
          <li key={heading.id}>
            <a
              href={`#${heading.id}`}
              className={cn(
                '-ml-px block border-l border-transparent py-0.5 text-muted-foreground hover:border-accent hover:text-foreground',
                heading.depth === 2 && 'pl-3',
                heading.depth === 3 && 'pl-6',
                heading.depth === 4 && 'pl-9',
              )}
            >
              {heading.text}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}
