/**
 * The docs folder as a collapsible tree with a filter box.
 *
 * Filtering keeps the ancestors of every match so the hierarchy stays readable,
 * and auto-expands while a filter is active. Expansion is keyed by folder path;
 * persisting it per repository is `useUiStore` work in a later story.
 */

import { ChevronDown, ChevronRight, FileText } from 'lucide-react';
import { useMemo, useState } from 'react';

import type { KbNode } from '@/api/provider';
import { Input } from '@/components/ui/input';
import { kbHref } from '@/features/kb/kb-links';
import { RouterLink } from '@/features/kb/KbLink';
import { cn } from '@/lib/cn';

export type KbTreeProps = {
  project: string;
  nodes: KbNode[];
  currentPath: string;
};

function matches(node: KbNode, needle: string): boolean {
  if (!needle) return true;
  if (node.name.toLowerCase().includes(needle)) return true;
  if ((node.title ?? '').toLowerCase().includes(needle)) return true;
  return (node.children ?? []).some((child) => matches(child, needle));
}

function defaultExpanded(nodes: KbNode[], currentPath: string): Set<string> {
  const open = new Set<string>();
  const parts = currentPath.split('/');
  for (let i = 1; i < parts.length; i += 1) open.add(parts.slice(0, i).join('/'));
  for (const node of nodes) if (node.kind === 'dir') open.add(node.path);
  return open;
}

export function KbTree({ project, nodes, currentPath }: KbTreeProps) {
  const [filter, setFilter] = useState('');
  const [expanded, setExpanded] = useState<Set<string>>(() => defaultExpanded(nodes, currentPath));
  const needle = filter.trim().toLowerCase();

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
