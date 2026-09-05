import { Link } from '@tanstack/react-router';
import type { ReactElement, ReactNode } from 'react';

type FeatureLinkProps = {
  /** Route path template, e.g. `/p/$project/items/$id/edit`. */
  to: string;
  params?: Record<string, string>;
  search?: Record<string, string>;
  className?: string;
  title?: string;
  /** Accessible name, when the visible text alone does not carry the context. */
  'aria-label'?: string;
  children: ReactNode;
};

/**
 * Client-side link into a route owned by another feature (the item editor).
 *
 * The router's `Link` is typed against the assembled route tree; this wrapper
 * keeps the backlog compiling and navigating even while that route is being
 * added, without dropping back to a full page load.
 */
const LooseLink = Link as unknown as (props: FeatureLinkProps) => ReactElement;

export function FeatureLink(props: FeatureLinkProps) {
  return <LooseLink {...props} />;
}
