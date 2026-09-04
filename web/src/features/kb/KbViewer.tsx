/**
 * KbViewer (`/p/$project/kb/$`) — docs/05-web-app.md §3.1.
 *
 * Three columns: the docs tree, the rendered page, the outline. Everything the
 * page needs comes from the provider (`listKbTree`, `getPage`, `readAsset`), so
 * the screen is identical in browser-only and companion mode.
 */

import { useParams, useRouter } from '@tanstack/react-router';
import { FileQuestion, PanelLeft } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';

import type { KbScope } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import {
  breadcrumbs,
  buildKbIndex,
  createKbResolvers,
  EMPTY_KB_INDEX,
  kbHref,
  resolveRequestedPath,
} from '@/features/kb/kb-links';
import { KbBacklinks } from '@/features/kb/KbBacklinks';
import { KbFrontMatter } from '@/features/kb/KbFrontMatter';
import { KbLink, RouterLink } from '@/features/kb/KbLink';
import { KbToc } from '@/features/kb/KbToc';
import { KbTree } from '@/features/kb/KbTree';
import { useKbInvalidation, useKbPage, useKbTree } from '@/features/kb/useKbData';
import { cn } from '@/lib/cn';
import type { RenderOptions } from '@/markdown';
import { MarkdownContent, useAssetResolver, useMarkdown } from '@/markdown';

export function KbViewer() {
  const params = useParams({ strict: false });
  const project = params.project ?? '';
  const splat = params['_splat'] ?? '';

  const provider = useProvider();
  const router = useRouter();

  const scope = useMemo<KbScope>(() => ({ kind: 'project', projectKey: project }), [project]);
  const treeQuery = useKbTree(project, scope);
  const index = useMemo(
    () => (treeQuery.data ? buildKbIndex(treeQuery.data) : EMPTY_KB_INDEX),
    [treeQuery.data],
  );

  const path = useMemo(() => {
    // Resolution needs the tree; before it arrives, trust the URL as written.
    const requested = splat.replace(/\/+$/, '');
    if (!treeQuery.data) return requested;
    return resolveRequestedPath(requested, index);
  }, [splat, treeQuery.data, index]);

  const pageQuery = useKbPage(project, scope, path);
  useKbInvalidation(project);

  const [rawOpen, setRawOpen] = useState(false);
  const [treeOpen, setTreeOpen] = useState(false);
  useEffect(() => {
    setRawOpen(false);
    setTreeOpen(false);
  }, [path]);

  const page = pageQuery.data;
  const resolvers = useMemo(
    () => createKbResolvers(project, index, path),
    [project, index, path],
  );
  const renderOptions = useMemo<RenderOptions>(
    () => ({
      basePath: path,
      resolveLink: resolvers.resolveLink,
      resolveHref: resolvers.resolveHref,
      ...(page ? { cacheKey: `${path}@${page.rev}` } : {}),
    }),
    [path, resolvers, page],
  );
  const markdown = useMarkdown(page?.body ?? '', renderOptions);

  const loadAsset = useCallback(
    (assetPath: string) => provider.readAsset(scope, assetPath),
    [provider, scope],
  );
  const resolveAsset = useAssetResolver(loadAsset);

  const editHref = useMemo(() => {
    const ids = Object.keys(router.routesById);
    return ids.some((id) => id.endsWith('/kb/$/edit')) ? `${kbHref(project, path)}/edit` : null;
  }, [router, project, path]);

  const crumbs = breadcrumbs(path);

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,14rem)_minmax(0,1fr)_minmax(0,12rem)]">
      <aside
        className={cn(
          'min-w-0 lg:block lg:border-r lg:pr-4',
          treeOpen ? 'block' : 'hidden',
        )}
      >
        {treeQuery.isPending ? (
          <TreeSkeleton />
        ) : treeQuery.isError ? (
          <p className="text-sm text-destructive">The docs folder could not be listed.</p>
        ) : (
          <KbTree project={project} nodes={treeQuery.data} currentPath={path} />
        )}
      </aside>

      <main className="min-w-0 space-y-4">
        <header className="space-y-2">
          <div className="flex items-start justify-between gap-3">
            <nav aria-label="Breadcrumb" className="min-w-0">
              <ol className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
                {crumbs.map((crumb) => (
                  <li key={crumb.path} className="flex items-center gap-1">
                    <span className="text-border">/</span>
                    {crumb.isPage ? (
                      <span className="font-medium text-foreground">{crumb.name}</span>
                    ) : (
                      <span>{crumb.name}</span>
                    )}
                  </li>
                ))}
              </ol>
            </nav>
            <div className="flex shrink-0 items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                className="lg:hidden"
                aria-expanded={treeOpen}
                onClick={() => setTreeOpen((open) => !open)}
              >
                <PanelLeft className="size-4" aria-hidden="true" />
                Pages
              </Button>
              <Button
                variant={rawOpen ? 'secondary' : 'ghost'}
                size="sm"
                aria-pressed={rawOpen}
                onClick={() => setRawOpen((open) => !open)}
              >
                Open raw
              </Button>
              {editHref ? (
                <RouterLink
                  to={editHref}
                  className="inline-flex h-8 items-center rounded-md border px-3 text-xs font-medium hover:bg-secondary"
                >
                  Edit
                </RouterLink>
              ) : null}
            </div>
          </div>
          {page ? (
            <h1 className="text-2xl font-semibold tracking-tight">{page.title}</h1>
          ) : null}
        </header>

        {pageQuery.isPending && path !== '' ? <PageSkeleton /> : null}

        {pageQuery.isError ? <MissingPage project={project} path={path} /> : null}

        {path === '' && !treeQuery.isPending && !treeQuery.isError ? (
          <Card>
            <CardContent className="p-5 text-sm text-muted-foreground">
              This project has no knowledge base pages yet.
            </CardContent>
          </Card>
        ) : null}

        {page ? (
          <>
            <KbFrontMatter page={page} />

            {rawOpen ? (
              <pre className="overflow-x-auto rounded-md border bg-muted p-4 text-xs">
                <code>{page.body}</code>
              </pre>
            ) : markdown.status === 'error' ? (
              <p className="text-sm text-destructive">
                This page could not be rendered: {markdown.error?.message}
              </p>
            ) : markdown.result ? (
              <MarkdownContent
                result={markdown.result}
                resolveAsset={resolveAsset}
                renderLink={KbLink}
                className="prose-kb"
              />
            ) : (
              <PageSkeleton />
            )}

            <KbBacklinks project={project} backlinks={page.backlinks} />
          </>
        ) : null}
      </main>

      <aside className="hidden min-w-0 lg:block">
        {markdown.result ? <KbToc headings={markdown.result.headings} /> : null}
      </aside>
    </div>
  );
}

function TreeSkeleton() {
  return (
    <div className="space-y-2" aria-hidden="true">
      {[0, 1, 2, 3, 4].map((row) => (
        <div key={row} className="h-4 animate-pulse rounded bg-muted" style={{ width: `${90 - row * 10}%` }} />
      ))}
    </div>
  );
}

function PageSkeleton() {
  return (
    <div className="space-y-3" role="status" aria-label="Loading page">
      <div className="h-6 w-1/3 animate-pulse rounded bg-muted" />
      <div className="h-4 w-full animate-pulse rounded bg-muted" />
      <div className="h-4 w-11/12 animate-pulse rounded bg-muted" />
      <div className="h-4 w-4/5 animate-pulse rounded bg-muted" />
    </div>
  );
}

function MissingPage({ project, path }: { project: string; path: string }) {
  return (
    <Card>
      <CardContent className="space-y-3 p-5">
        <div className="flex items-center gap-2">
          <FileQuestion className="size-5 text-muted-foreground" aria-hidden="true" />
          <h2 className="text-base font-semibold">Page not found</h2>
        </div>
        <p className="text-sm text-muted-foreground">
          Nothing exists at <code>{path}</code> in <strong>{project}</strong>. Create the file to
          start the page — the link that brought you here will resolve as soon as it exists.
        </p>
        <p className="text-sm text-muted-foreground">
          Pick another page from the tree, or start from the{' '}
          <RouterLink to={kbHref(project, '')} className="underline underline-offset-2">
            knowledge base index
          </RouterLink>
          .
        </p>
      </CardContent>
    </Card>
  );
}
