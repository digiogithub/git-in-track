import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { Search, Sparkles } from 'lucide-react';
import {
  useDeferredValue,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type MutableRefObject,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react';

import type { SearchHit } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { useUiPrefs } from '@/app/ui-prefs';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { hitKey, MIN_QUERY, queryTerms } from '@/features/search/search-hits';
import { HitRow } from '@/features/search/SearchHits';
import { cn } from '@/lib/cn';

/** How many hits the overlay asks the core for. */
const SEARCH_LIMIT = 20;

type Tab = 'all' | 'items' | 'kb';

const TABS: { id: Tab; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'items', label: 'Items' },
  { id: 'kb', label: 'KB' },
];

/** Items are item hits, comment matches included; KB is pages and files. */
function inTab(hit: SearchHit, tab: Tab): boolean {
  switch (tab) {
    case 'items':
      return hit.kind === 'item';
    case 'kb':
      return hit.kind === 'page' || hit.kind === 'file';
    default:
      return true;
  }
}

type HitTarget =
  { kind: 'item'; project: string; id: string } | { kind: 'page'; project: string; path: string };

/**
 * Where choosing a hit goes, or `null` when it has no screen: a `file` hit is
 * a document the index owns neither as an item nor as a page.
 */
function hitTarget(hit: SearchHit, project: string): HitTarget | null {
  const key = hit.project ?? project;
  if (hit.kind === 'item' && hit.id) return { kind: 'item', project: key, id: hit.id };
  // The KB route's splat is the vault path, which is what a page hit carries.
  if (hit.kind === 'page' && hit.path) return { kind: 'page', project: key, path: hit.path };
  return null;
}

/** Ctrl+Shift+F, or Cmd+Shift+F on macOS; `code` covers non-Latin layouts. */
function isSearchShortcut(event: KeyboardEvent): boolean {
  return (
    (event.ctrlKey || event.metaKey) &&
    event.shiftKey &&
    !event.altKey &&
    (event.key.toLowerCase() === 'f' || event.code === 'KeyF')
  );
}

/**
 * The project search overlay (story GIT-US-0103).
 *
 * Mounted by the `/p/$project` layout, so the shortcut exists on every project
 * route and nowhere else. It asks the same `search` as the workspace panel,
 * scoped to the current project, and lists the hits with the same rows: exact
 * matches first, then — where Pando answers and the user has not hidden them —
 * the passages related by meaning. The selection is one index over that
 * visible order, driven from the input so typing and choosing never compete
 * for focus.
 */
export function ProjectSearchOverlay() {
  const params = useParams({ strict: false });
  const project = params.project ?? '';
  const [open, setOpen] = useState(false);
  const returnFocus = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || !isSearchShortcut(event)) return;
      // CodeMirror binds no Mod-Shift-f today; should it ever, the editor wins.
      if (event.target instanceof Element && event.target.closest('.cm-editor')) return;
      event.preventDefault();
      // There is no trigger for Radix to hand focus back to, so remember it.
      if (document.activeElement instanceof HTMLElement) {
        returnFocus.current = document.activeElement;
      }
      setOpen(true);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {open ? (
        <SearchPanel
          project={project}
          returnFocus={returnFocus}
          onClose={() => {
            setOpen(false);
          }}
        />
      ) : null}
    </Dialog>
  );
}

/**
 * The dialog body. It unmounts on close, so every opening starts from an empty
 * query and the first tab, and focus goes back to where it was.
 */
function SearchPanel({
  project,
  returnFocus,
  onClose,
}: {
  project: string;
  returnFocus: MutableRefObject<HTMLElement | null>;
  onClose: () => void;
}) {
  const provider = useProvider();
  const navigate = useNavigate();
  const inputId = useId();
  const listId = useId();
  const [text, setText] = useState('');
  const [tab, setTab] = useState<Tab>('all');
  const [selected, setSelected] = useState(0);
  const query = useDeferredValue(text.trim());
  const enabled = query.length >= MIN_QUERY;
  const semanticSupported = provider.capabilities.fullTextSearch === 'pando';
  const semanticEnabled = useUiPrefs((state) => state.semanticResults);
  const setSemanticResults = useUiPrefs((state) => state.setSemanticResults);
  const showSemantic = semanticSupported && semanticEnabled;

  const results = useQuery({
    queryKey: ['project-search', project, query],
    queryFn: () => provider.search({ text: query, projectKey: project, limit: SEARCH_LIMIT }),
    enabled,
  });

  const hits = results.data?.hits ?? [];
  const exact = hits.filter((hit) => hit.source !== 'pando' && inTab(hit, tab));
  const semantic = showSemantic
    ? hits.filter((hit) => hit.source === 'pando' && inTab(hit, tab))
    : [];
  const visible = [...exact, ...semantic];
  const terms = useMemo(() => queryTerms(query), [query]);
  const degraded = showSemantic && results.data?.degraded === true;
  const nothing = enabled && !results.isPending && visible.length === 0;

  // A new query or tab is a new list: the selection starts at its top.
  useEffect(() => {
    setSelected(0);
  }, [query, tab]);

  const current = Math.min(selected, visible.length - 1);
  const optionId = (index: number) => `${listId}-option-${index}`;

  // Keep the selected row on screen when the arrows walk past the fold.
  useEffect(() => {
    document.getElementById(`${listId}-option-${current}`)?.scrollIntoView?.({ block: 'nearest' });
  }, [listId, current]);

  const choose = (hit: SearchHit | undefined) => {
    const target = hit ? hitTarget(hit, project) : null;
    if (!target) return;
    onClose();
    if (target.kind === 'item') {
      void navigate({
        to: '/p/$project/items/$id',
        params: { project: target.project, id: target.id },
      });
    } else {
      void navigate({
        to: '/p/$project/kb/$',
        params: { project: target.project, _splat: target.path },
      });
    }
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    if (visible.length === 0) return;
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        setSelected((current + 1) % visible.length);
        break;
      case 'ArrowUp':
        event.preventDefault();
        setSelected((current - 1 + visible.length) % visible.length);
        break;
      case 'Enter':
        event.preventDefault();
        choose(visible[current]);
        break;
      default:
    }
  };

  const row = (hit: SearchHit, index: number) => {
    const navigable = hitTarget(hit, project) !== null;
    return (
      <HitRow
        key={hitKey(hit)}
        id={optionId(index)}
        hit={hit}
        terms={terms}
        role="option"
        aria-selected={index === current}
        aria-disabled={navigable ? undefined : true}
        selected={index === current}
        className={cn(navigable && 'cursor-pointer')}
        onMouseMove={() => {
          if (index !== current) setSelected(index);
        }}
        onClick={() => {
          choose(hit);
        }}
      />
    );
  };

  return (
    <DialogContent
      className="top-[12vh] w-[min(44rem,calc(100vw-2rem))] translate-y-0"
      onOpenAutoFocus={(event) => {
        // Focus the query, not the first tab: the overlay exists to type into.
        event.preventDefault();
        document.getElementById(inputId)?.focus();
      }}
      onCloseAutoFocus={(event) => {
        // Radix would focus the (absent) trigger. After navigating, the element
        // may be gone, and then the new page keeps its own focus.
        const element = returnFocus.current;
        returnFocus.current = null;
        if (!element?.isConnected) return;
        event.preventDefault();
        element.focus();
      }}
    >
      <DialogTitle className="flex items-center gap-2">
        <Search aria-hidden="true" className="h-4 w-4" />
        Search {project}
      </DialogTitle>
      <DialogDescription className="mt-1.5">
        Items and knowledge-base pages in this project. ↑ ↓ to move, Enter to open, Esc to close.
      </DialogDescription>

      <div className="mt-4 space-y-3">
        <label className="sr-only" htmlFor={inputId}>
          Search this project
        </label>
        <Input
          id={inputId}
          type="search"
          value={text}
          placeholder="Search items and pages…"
          role="combobox"
          aria-expanded={visible.length > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={visible.length > 0 ? optionId(current) : undefined}
          onChange={(event) => {
            setText(event.target.value);
          }}
          onKeyDown={onKeyDown}
        />

        <div className="flex flex-wrap items-center justify-between gap-2">
          <div role="tablist" aria-label="Result type" className="flex gap-1">
            {TABS.map((entry) => (
              <button
                key={entry.id}
                type="button"
                role="tab"
                aria-selected={tab === entry.id}
                aria-controls={listId}
                className={cn(
                  'rounded-md px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors duration-fast hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                  tab === entry.id && 'bg-secondary text-foreground',
                )}
                onClick={() => {
                  setTab(entry.id);
                }}
              >
                {entry.label}
              </button>
            ))}
          </div>

          {semanticSupported ? (
            <div className="flex items-center gap-2">
              <Switch
                checked={semanticEnabled}
                onCheckedChange={setSemanticResults}
                aria-label="Show related by meaning"
              />
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <Sparkles aria-hidden="true" className="h-3 w-3" />
                Show related by meaning
              </span>
            </div>
          ) : null}
        </div>

        <div className="max-h-[55vh] space-y-3 overflow-y-auto">
          {enabled && results.isPending ? (
            <p className="text-sm text-muted-foreground">Searching…</p>
          ) : null}

          {nothing ? <p className="empty-state">Nothing matched “{query}”.</p> : null}

          <div id={listId} role="listbox" aria-label="Search results" className="space-y-3">
            {exact.length > 0 ? (
              <ul role="group" aria-label="Exact matches" className="space-y-2">
                {exact.map((hit, index) => row(hit, index))}
              </ul>
            ) : null}
            {semantic.length > 0 ? (
              <div className="space-y-2">
                <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  Related by meaning
                </h3>
                <ul role="group" aria-label="Related by meaning" className="space-y-2">
                  {semantic.map((hit, index) => row(hit, exact.length + index))}
                </ul>
              </div>
            ) : null}
          </div>

          {degraded ? (
            <p className="text-xs text-muted-foreground">
              Semantic search unavailable — exact matches only.
            </p>
          ) : null}
        </div>
      </div>
    </DialogContent>
  );
}
