import { useMemo } from 'react';

import { FeatureLink } from '@/features/backlog/FeatureLink';
import {
  MarkdownContent,
  useMarkdown,
  type LinkResolution,
  type MarkdownLinkProps,
  type RenderOptions,
  type WikiTarget,
} from '@/markdown';

export type ItemBodyProps = {
  body: string;
  /** Vault-relative path of the item file; relative links resolve against it. */
  path: string;
  /** Project of the item, used for unqualified wikilink targets. */
  project: string;
  /** `path@rev`, so a re-render of the same revision is served from the cache. */
  cacheKey?: string;
};

/**
 * The item body, rendered with the shared Markdown pipeline (docs/05-web-app.md
 * §7). Wikilinks resolve into app routes: `[[ACME-US-0042]]` to the item view,
 * `[[architecture/overview]]` to the knowledge base.
 */
export function ItemBody({ body, path, project, cacheKey }: ItemBodyProps) {
  const options = useMemo<RenderOptions>(
    () => ({
      basePath: path,
      ...(cacheKey ? { cacheKey } : {}),
      resolveLink: (target: WikiTarget): LinkResolution => {
        const key = target.projectKey ?? project;
        return target.kind === 'item'
          ? { href: `/p/${key}/items/${target.target}`, kind: 'item' }
          : { href: `/p/${key}/kb/${target.target}`, kind: 'page' };
      },
      resolveHref: (vaultPath: string): LinkResolution => ({
        href: `/p/${project}/kb/${vaultPath}`,
        kind: 'page',
      }),
    }),
    [path, project, cacheKey],
  );

  const markdown = useMarkdown(body, options);

  if (body.trim().length === 0) {
    return <p className="text-sm text-muted-foreground">This item has no body yet.</p>;
  }

  if (markdown.status === 'error') {
    return (
      <div className="space-y-2">
        <p className="text-sm text-destructive">
          The body could not be rendered: {markdown.error?.message}
        </p>
        <pre className="whitespace-pre-wrap break-words font-mono text-sm">{body}</pre>
      </div>
    );
  }

  if (!markdown.result) {
    return <p className="text-sm text-muted-foreground">Rendering…</p>;
  }

  return (
    <MarkdownContent
      result={markdown.result}
      renderLink={({ href, children, className, title }: MarkdownLinkProps) => (
        <FeatureLink to={href} {...(className ? { className } : {})} {...(title ? { title } : {})}>
          {children}
        </FeatureLink>
      )}
    />
  );
}
