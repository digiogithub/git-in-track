/**
 * The router link the Markdown renderer uses for in-app targets.
 *
 * `markdown/` deliberately knows nothing about routing: it hands back an href
 * plus the resolution kind, and this component turns that into a TanStack
 * Router `<Link>` so navigation stays client-side and preloading works.
 *
 * Hrefs are computed from the vault at runtime, so they cannot be checked
 * against the static route tree; `to` is therefore typed as a plain string
 * here. The routes themselves (`/p/$project/kb/$`, `/p/$project/items/$id`)
 * stay type-checked where they are declared.
 */

import { Link } from '@tanstack/react-router';
import type { AnchorHTMLAttributes, FunctionComponent } from 'react';

import { cn } from '@/lib/cn';
import type { MarkdownLinkProps } from '@/markdown';

type RouterLinkProps = { to: string } & AnchorHTMLAttributes<HTMLAnchorElement>;

const RouterLink = Link as unknown as FunctionComponent<RouterLinkProps>;

export function KbLink({ href, children, className, title, kind, wikilink }: MarkdownLinkProps) {
  const missing = kind === 'missing';
  return (
    <RouterLink
      to={href}
      className={cn(
        className,
        'underline-offset-2 hover:underline',
        kind === 'item' && 'font-medium text-accent',
        missing && 'text-destructive decoration-dotted underline',
      )}
      {...(title ? { title } : {})}
      data-kind={kind}
      data-wikilink={wikilink}
    >
      {children}
      {missing ? <span className="sr-only"> (page not created yet)</span> : null}
    </RouterLink>
  );
}

export { RouterLink };
