/**
 * KbViewer (`/p/$project/kb/$`) — docs/05-web-app.md §3.1.
 *
 * Three columns: the docs tree, the rendered page, the outline. Everything the
 * page needs comes from the provider (`listKbTree`, `getPage`, `readAsset`), so
 * the screen is identical in browser-only and companion mode.
 */

import { useParams, useRouter } from '@tanstack/react-router';
import {
  FileQuestion,
  Maximize2,
  Minimize2,
  PanelLeft,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
} from 'lucide-react';
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react';

import type { KbScope } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import {
  KB_TREE_DEFAULT_WIDTH,
  KB_TREE_MAX_WIDTH,
  KB_TREE_MIN_WIDTH,
  useUiPrefs,
} from '@/app/ui-prefs';
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
import { tocOutline } from '@/features/kb/toc';
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
  const treeWidth = useUiPrefs((state) => state.kbTreeWidth);
  const setTreeWidth = useUiPrefs((state) => state.setKbTreeWidth);
  const tocOpen = useUiPrefs((state) => state.kbTocOpen);
  const setTocOpen = useUiPrefs((state) => state.setKbTocOpen);
  const treePinned = useUiPrefs((state) => state.kbTreeOpen);
  const setTreePinned = useUiPrefs((state) => state.setKbTreeOpen);
  const maximized = useUiPrefs((state) => state.kbMaximized);
  const setMaximized = useUiPrefs((state) => state.setKbMaximized);
  useEffect(() => {
    setRawOpen(false);
    setTreeOpen(false);
  }, [path]);

  // Escape leaves maximized mode, unless something else (a dialog, a search
  // box) already handled the key.
  useEffect(() => {
    if (!maximized) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) setMaximized(false);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [maximized, setMaximized]);

  // Desktop panels: the tree and the outline each have their own toggle, and
  // maximized mode folds both without forgetting those choices.
  const showTree = treePinned && !maximized;

  const page = pageQuery.data;
  const resolvers = useMemo(() => createKbResolvers(project, index, path), [project, index, path]);
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

  // The document usually opens with its own `# Title`. Rendering the chrome
  // heading as well would duplicate it, for readers and for screen readers
  // alike, so the document wins when it already states the title.
  const firstHeading = markdown.result?.headings[0];
  const documentOwnsTitle =
    firstHeading?.depth === 1 &&
    page !== undefined &&
    firstHeading.text.trim().toLowerCase() === page.title.trim().toLowerCase();

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
  const outline = markdown.result ? tocOutline(markdown.result.headings) : [];
  const hasOutline = outline.length > 0 && !rawOpen;
  const showToc = hasOutline && tocOpen && !maximized;

  return (
    <div className="flex flex-col gap-6 lg:flex-row lg:gap-0">
      <aside
        className={cn(
          'min-w-0 lg:sticky lg:top-4 lg:max-h-[calc(100vh-2rem)] lg:w-[var(--kb-tree-width)] lg:shrink-0 lg:self-start lg:overflow-y-auto lg:pr-3',
          treeOpen ? 'block' : 'hidden',
          showTree ? 'lg:block' : 'lg:hidden',
        )}
        data-testid="kb-tree-panel"
        data-desktop-visible={showTree}
        style={{ '--kb-tree-width': `${treeWidth}px` } as CSSProperties}
      >
        {treeQuery.isPending ? (
          <TreeSkeleton />
        ) : treeQuery.isError ? (
          <p className="text-sm text-destructive">The docs folder could not be listed.</p>
        ) : (
          <KbTree project={project} nodes={treeQuery.data} currentPath={path} />
        )}
      </aside>

      {showTree ? <TreeResizeHandle width={treeWidth} onResize={setTreeWidth} /> : null}

      <main className={cn('min-w-0 flex-1 space-y-4', showTree && 'lg:pl-6')}>
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
              {maximized ? null : (
                <Button
                  variant="ghost"
                  size="sm"
                  className="hidden lg:inline-flex"
                  aria-pressed={treePinned}
                  aria-label={treePinned ? 'Hide pages panel' : 'Show pages panel'}
                  title={treePinned ? 'Hide pages panel' : 'Show pages panel'}
                  onClick={() => setTreePinned(!treePinned)}
                >
                  {treePinned ? (
                    <PanelLeftClose className="size-4" aria-hidden="true" />
                  ) : (
                    <PanelLeftOpen className="size-4" aria-hidden="true" />
                  )}
                  Pages
                </Button>
              )}
              {hasOutline && !maximized ? (
                <Button
                  variant="ghost"
                  size="sm"
                  className="hidden lg:inline-flex"
                  aria-pressed={tocOpen}
                  aria-label={tocOpen ? 'Hide page outline' : 'Show page outline'}
                  title={tocOpen ? 'Hide page outline' : 'Show page outline'}
                  onClick={() => setTocOpen(!tocOpen)}
                >
                  {tocOpen ? (
                    <PanelRightClose className="size-4" aria-hidden="true" />
                  ) : (
                    <PanelRightOpen className="size-4" aria-hidden="true" />
                  )}
                  Outline
                </Button>
              ) : null}
              <Button
                variant={maximized ? 'secondary' : 'ghost'}
                size="sm"
                className="hidden lg:inline-flex"
                aria-pressed={maximized}
                aria-label={maximized ? 'Restore panels' : 'Maximize content'}
                title={maximized ? 'Restore panels (Esc)' : 'Maximize content'}
                onClick={() => setMaximized(!maximized)}
              >
                {maximized ? (
                  <Minimize2 className="size-4" aria-hidden="true" />
                ) : (
                  <Maximize2 className="size-4" aria-hidden="true" />
                )}
                {maximized ? 'Restore' : 'Maximize'}
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
          {page && !documentOwnsTitle ? <h1 className="page-title">{page.title}</h1> : null}
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

      {showToc ? (
        // Follows the reader down the page; an outline taller than the screen
        // scrolls on its own so every section stays reachable.
        <aside className="hidden w-56 shrink-0 self-start lg:sticky lg:top-4 lg:block lg:max-h-[calc(100vh-2rem)] lg:overflow-y-auto lg:pl-6">
          <KbToc headings={outline} />
        </aside>
      ) : null}
    </div>
  );
}

/** Keyboard step of the tree resize handle, in pixels. */
const RESIZE_STEP = 16;

/**
 * The drag handle between the docs tree and the page. Pointer drags and the
 * arrow keys both resize the tree, clamped to `KB_TREE_MIN_WIDTH`…`MAX`.
 */
function TreeResizeHandle({
  width,
  onResize,
}: {
  width: number;
  onResize: (width: number) => void;
}) {
  const [dragging, setDragging] = useState(false);

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = width;
    setDragging(true);

    const onMove = (move: PointerEvent) => onResize(startWidth + move.clientX - startX);
    const onUp = () => {
      setDragging(false);
      document.body.style.removeProperty('cursor');
      document.body.style.removeProperty('user-select');
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onUp);
    };
    // The cursor and the no-select must hold while the pointer leaves the
    // handle, which it does on the first fast move.
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onUp);
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'ArrowLeft') onResize(width - RESIZE_STEP);
    else if (event.key === 'ArrowRight') onResize(width + RESIZE_STEP);
    else if (event.key === 'Home') onResize(KB_TREE_MIN_WIDTH);
    else if (event.key === 'End') onResize(KB_TREE_MAX_WIDTH);
    else return;
    event.preventDefault();
  };

  // A focusable separator is the WAI-ARIA window splitter pattern: an
  // interactive widget, which jsx-a11y does not recognise.
  return (
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize pages panel"
      aria-valuenow={width}
      aria-valuemin={KB_TREE_MIN_WIDTH}
      aria-valuemax={KB_TREE_MAX_WIDTH}
      // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
      tabIndex={0}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onDoubleClick={() => onResize(KB_TREE_DEFAULT_WIDTH)}
      title="Drag to resize, double-click to reset"
      className="group hidden w-2 shrink-0 cursor-col-resize touch-none justify-center self-stretch focus-visible:outline-none lg:flex"
    >
      <span
        aria-hidden="true"
        className={cn(
          'w-px bg-border transition-colors duration-fast group-hover:w-0.5 group-hover:bg-accent group-focus-visible:w-0.5 group-focus-visible:bg-accent',
          dragging && 'w-0.5 bg-accent',
        )}
      />
    </div>
  );
}

function TreeSkeleton() {
  return (
    <div className="space-y-2" aria-hidden="true">
      {[0, 1, 2, 3, 4].map((row) => (
        <div
          key={row}
          className="h-4 animate-pulse rounded bg-muted"
          style={{ width: `${90 - row * 10}%` }}
        />
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
