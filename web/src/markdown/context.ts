/**
 * What the React renderer needs from its host, injected rather than imported:
 * `markdown/` knows nothing about the router or the data provider.
 */

import { createContext, useContext, type ReactElement, type ReactNode } from 'react';

import type { ResolveAsset } from '@/markdown/types';

export type MarkdownLinkProps = {
  href: string;
  children: ReactNode;
  className?: string;
  title?: string;
  /** `page`, `item` or `missing`, as decided by the caller's `resolveLink`. */
  kind?: string;
  /** Item id for `[[ACME-US-0042]]`-style links. */
  itemRef?: string;
  /** Raw wikilink target, for "create page" affordances. */
  wikilink?: string;
};

/** Renders an in-app link. The KB viewer supplies a TanStack Router `<Link>`. */
export type MarkdownLinkRenderer = (props: MarkdownLinkProps) => ReactElement;

export type MarkdownContextValue = {
  /** Turns a vault-relative asset path into a URL the browser can load. */
  resolveAsset?: ResolveAsset;
  renderLink?: MarkdownLinkRenderer;
};

export const MarkdownContext = createContext<MarkdownContextValue>({});

export function useMarkdownContext(): MarkdownContextValue {
  return useContext(MarkdownContext);
}
