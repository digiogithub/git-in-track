import type { Heading } from '@/markdown';

/** The headings the page outline lists: `##` to `####`. */
export function tocOutline(headings: Heading[]): Heading[] {
  return headings.filter((heading) => heading.depth >= 2 && heading.depth <= 4);
}
