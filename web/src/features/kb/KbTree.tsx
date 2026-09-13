/**
 * The docs folder as a collapsible tree with a filter box.
 *
 * Folders start collapsed, except the ones leading to the current page.
 * Filtering keeps the ancestors of every match so the hierarchy stays readable,
 * and auto-expands while a filter is active. Expansion is keyed by folder path.
 */

import { ChevronDown, ChevronRight, FileText } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';

import type { KbNode, KbSyncState } from '@/api/provider';
import { Input } from '@/components/ui/input';
import { kbHref } from '@/features/kb/kb-links';
import { summariseKbSync } from '@/features/kb/kb-sync';
import { RouterLink } from '@/features/kb/KbLink';
import { KbSyncChip } from '@/features/kb/KbSyncBadge';
import { cn } from '@/lib/cn';

export type KbTreeProps = {
  project: string;
  nodes: KbNode[];
  currentPath: string;
  /**
   * The synchronization state of every page, by path. Absent — the normal case
   * in browser-only mode, and while the first answer is in flight — means the
   * tree renders exactly as it always did: this is a decoration on a
   * navigation aid, never a reason to hold it back.
   */
  syncStates?: Map<string, KbSyncState>;
};

/** Every page path at or below a node, so a folder can summarise its children. */
function pageStates(node: KbNode, states: Map<string, KbSyncState>): KbSyncState[] {
  if (node.kind === 'page') {
    const state = states.get(node.path);
    return state === undefined ? [] : [state];
  }
  return (node.children ?? []).flatMap((child) => pageStates(child, states));
}

function matches(node: KbNode, needle: string): boolean {
  if (!needle) return true;
  if (node.name.toLowerCase().includes(needle)) return true;
  if ((node.title ?? '').toLowerCase().includes(needle)) return true;
  return (node.children ?? []).some((child) => matches(child, needle));
}

/** The folders leading to `path`: only those start open, the rest stay folded. */
function ancestors(path: string): string[] {
  const parts = path.split('/');
  const out: string[] = [];
  for (let i = 1; i < parts.length; i += 1) out.push(parts.slice(0, i).join('/'));
  return out;
}

export function KbTree({ project, nodes, currentPath, syncStates }: KbTreeProps) {
  const [filter, setFilter] = useState('');
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(ancestors(currentPath)));
  const needle = filter.trim().toLowerCase();

  // Navigating to a page reveals it without folding what the reader opened.
  useEffect(() => {
    setExpanded((previous) => {
      const missing = ancestors(currentPath).filter((path) => !previous.has(path));
      return missing.length === 0 ? previous : new Set([...previous, ...missing]);
    });
  }, [currentPath]);

  const visible = useMemo(() => nodes.filter((node) => matches(node, needle)), [nodes, needle]);

  const toggle = (path: string) => {
    setExpanded((previous) => {
      const next = new Set(previous);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  const isOpen = (path: string) => needle !== '' || expanded.has(path);

  const renderNodes = (list: KbNode[], depth: number) => (
    <ul className="space-y-0.5">
      {list
        .filter((node) => matches(node, needle))
        .map((node) => (
          <li key={node.path}>
            {node.kind === 'dir' ? (
              <>
                <button
                  type="button"
                  onClick={() => toggle(node.path)}
                  aria-expanded={isOpen(node.path)}
                  className="flex w-full items-center gap-1 rounded px-1.5 py-1 text-left text-sm hover:bg-secondary"
                  style={{ paddingLeft: `${depth * 12 + 6}px` }}
                >
                  {isOpen(node.path) ? (
                    <ChevronDown className="size-3.5 shrink-0" aria-hidden="true" />
                  ) : (
                    <ChevronRight className="size-3.5 shrink-0" aria-hidden="true" />
                  )}
                  <span className="truncate font-medium">{node.name}</span>
                  <KbNodeBadge node={node} syncStates={syncStates} />
                </button>
                {isOpen(node.path) ? renderNodes(node.children ?? [], depth + 1) : null}
              </>
            ) : (
              <RouterLink
                to={kbHref(project, node.path)}
                className={cn(
                  'flex items-center gap-1 rounded px-1.5 py-1 text-sm hover:bg-secondary',
                  node.path === currentPath && 'bg-secondary font-medium text-foreground',
                )}
                style={{ paddingLeft: `${depth * 12 + 6}px` }}
                {...(node.path === currentPath ? { 'aria-current': 'page' } : {})}
              >
                <FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                <span className="truncate">{node.title ?? node.name}</span>
                <KbNodeBadge node={node} syncStates={syncStates} />
              </RouterLink>
            )}
          </li>
        ))}
    </ul>
  );

  return (
    <nav aria-label="Knowledge base pages" className="space-y-2">
      <Input
        type="search"
        value={filter}
        onChange={(event) => setFilter(event.target.value)}
        placeholder="Filter pages"
        aria-label="Filter pages"
        className="h-8"
      />
      {visible.length === 0 ? (
        <p className="px-1.5 text-sm text-muted-foreground">No page matches “{filter}”.</p>
      ) : (
        renderNodes(nodes, 0)
      )}
    </nav>
  );
}

/**
 * The compact state chip on one tree row.
 *
 * A page shows its own state; a folder shows the worst state under it and how
 * many of its pages hold it, because the reason to summarise a folder is to
 * find the page that needs looking at. A row with nothing to say — no state
 * known, or a folder whose pages are all unpublished — shows nothing, so the
 * tree stays a tree.
 */
function KbNodeBadge({
  node,
  syncStates,
}: {
  node: KbNode;
  syncStates: Map<string, KbSyncState> | undefined;
}) {
  if (!syncStates || syncStates.size === 0) return null;
  const summary = summariseKbSync(pageStates(node, syncStates));
  if (!summary || summary.state === 'unlinked') return null;
  const isFolder = node.kind === 'dir';
  return (
    <KbSyncChip
      status={{ path: node.path, linked: true, state: summary.state }}
      {...(isFolder ? { detail: String(summary.count) } : {})}
      className="ml-auto shrink-0"
      size="sm"
    />
  );
}
