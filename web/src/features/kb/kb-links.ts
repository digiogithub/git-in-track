/**
 * Turning the KB tree into routes.
 *
 * Wikilink resolution follows docs/05-web-app.md §7: exact relative path →
 * path with `.md` appended → unique basename in the same scope →
 * case-insensitive match → unresolved. An ambiguous basename resolves to
 * nothing on purpose (`W-LINK-AMBIGUOUS`), so the link renders as broken rather
 * than pointing at an arbitrary page.
 */

import type { KbNode } from '@/api/provider';
import type { LinkResolution, WikiTarget } from '@/markdown';
import { basename, normalizePath, resolveFrom, stem } from '@/markdown';

export type KbIndex = {
  /** Every page path in the scope, in tree order. */
  pages: string[];
  /** Lowercased path (extension included) → the real path. */
  byLowerPath: Map<string, string>;
  /** Lowercased file stem → every page that has it. */
  byStem: Map<string, string[]>;
  /** Common top folder of the scope (`docs`), or `''` when pages live at the root. */
  root: string;
};

export const EMPTY_KB_INDEX: KbIndex = {
  pages: [],
  byLowerPath: new Map(),
  byStem: new Map(),
  root: '',
};

/** Depth-first list of every node in the tree. */
export function flattenTree(nodes: KbNode[]): KbNode[] {
  const out: KbNode[] = [];
  const walk = (list: KbNode[]) => {
    for (const node of list) {
      out.push(node);
      if (node.children) walk(node.children);
    }
  };
  walk(nodes);
  return out;
}

export function buildKbIndex(nodes: KbNode[]): KbIndex {
  const pages = flattenTree(nodes)
    .filter((node) => node.kind === 'page')
    .map((node) => node.path);

  const byLowerPath = new Map<string, string>();
  const byStem = new Map<string, string[]>();
  for (const path of pages) {
    byLowerPath.set(path.toLowerCase(), path);
    const key = stem(path).toLowerCase();
    byStem.set(key, [...(byStem.get(key) ?? []), path]);
  }

  const roots = new Set(nodes.map((node) => node.path.split('/')[0] ?? ''));
  const root = roots.size === 1 ? ([...roots][0] ?? '') : '';

  return { pages, byLowerPath, byStem, root };
}

/** Resolves a wikilink page target to a vault path, or `null` when unresolved. */
export function resolvePagePath(index: KbIndex, fromPath: string, target: string): string | null {
  const cleaned = target.replace(/^\.\//, '');
  const candidates = [
    resolveFrom(fromPath, cleaned),
    normalizePath(cleaned),
    index.root ? normalizePath(`${index.root}/${cleaned}`) : null,
  ].filter((value): value is string => value !== null && value !== '');

  const known = new Set(index.pages);
  for (const candidate of candidates) {
    if (known.has(candidate)) return candidate;
    if (known.has(`${candidate}.md`)) return `${candidate}.md`;
  }

  // Unique basename anywhere in the scope.
  const matches = index.byStem.get(stem(cleaned).toLowerCase()) ?? [];
  if (matches.length === 1) return matches[0] ?? null;
  if (matches.length > 1) return null;

  for (const candidate of candidates) {
    const insensitive =
      index.byLowerPath.get(candidate.toLowerCase()) ??
      index.byLowerPath.get(`${candidate}.md`.toLowerCase());
    if (insensitive) return insensitive;
  }

  return null;
}

export function kbHref(project: string, path: string): string {
  return `/p/${project}/kb/${path}`;
}

export function itemHref(project: string, id: string): string {
  return `/p/${project}/items/${id}`;
}

/** Where a "create page" affordance for an unresolved target would write. */
export function draftPagePath(fromPath: string, target: string): string {
  const cleaned = target.replace(/^\.\//, '');
  const withExtension = cleaned.endsWith('.md') ? cleaned : `${cleaned}.md`;
  return resolveFrom(fromPath, withExtension);
}

export type KbResolvers = {
  resolveLink: (target: WikiTarget) => LinkResolution;
  resolveHref: (vaultPath: string) => LinkResolution;
};

/** Builds the two resolvers the Markdown pipeline needs for one page. */
export function createKbResolvers(
  project: string,
  index: KbIndex,
  currentPath: string,
): KbResolvers {
  const resolveLink = (target: WikiTarget): LinkResolution => {
    if (target.kind === 'item') {
      const key = target.projectKey ?? project;
      return { href: itemHref(key, target.target), kind: 'item', label: target.target };
    }

    // A `KEY:page` reference names another project's docs folder; we cannot see
    // its tree from here, so it links optimistically.
    if (target.projectKey && target.projectKey !== project) {
      return {
        href: kbHref(target.projectKey, `${target.target}.md`),
        kind: 'page',
        label: target.target,
      };
    }

    const resolved = resolvePagePath(index, currentPath, target.target);
    if (resolved) {
      return {
        href: kbHref(project, resolved),
        kind: 'page',
        label: pageLabel(target, resolved),
        title: resolved,
      };
    }
    return {
      href: kbHref(project, draftPagePath(currentPath, target.target)),
      kind: 'missing',
    };
  };

  const resolveHref = (vaultPath: string): LinkResolution => {
    const known = index.pages.includes(vaultPath);
    return { href: kbHref(project, vaultPath), kind: known ? 'page' : 'missing' };
  };

  return { resolveLink, resolveHref };
}

function pageLabel(target: WikiTarget, resolved: string): string {
  if (target.alias) return target.alias;
  if (target.anchor) return `${stem(resolved)} › ${target.anchor}`;
  return target.target;
}

/**
 * Maps the route splat onto a real page. An empty splat opens the scope's index
 * page; a path without an extension gets `.md` appended, so
 * `/p/ACME/kb/docs/architecture/overview` works as a shareable URL.
 */
export function resolveRequestedPath(splat: string, index: KbIndex): string {
  const requested = normalizePath(splat);
  if (requested) {
    if (index.pages.includes(requested)) return requested;
    if (index.pages.includes(`${requested}.md`)) return `${requested}.md`;
    const insensitive =
      index.byLowerPath.get(requested.toLowerCase()) ??
      index.byLowerPath.get(`${requested}.md`.toLowerCase());
    return insensitive ?? requested;
  }
  return defaultPagePath(index);
}

/** `index.md`, then `README.md`, then the shallowest page in the scope. */
export function defaultPagePath(index: KbIndex): string {
  const preferred = ['index.md', 'readme.md'];
  const byDepth = [...index.pages].sort(
    (a, b) => a.split('/').length - b.split('/').length || a.localeCompare(b),
  );
  for (const name of preferred) {
    const hit = byDepth.find((path) => basename(path).toLowerCase() === name);
    if (hit) return hit;
  }
  return byDepth[0] ?? '';
}

export type Crumb = { name: string; path: string; isPage: boolean };

/** Breadcrumb trail for a page path; folders are not navigable on their own. */
export function breadcrumbs(path: string): Crumb[] {
  const parts = path.split('/').filter(Boolean);
  return parts.map((name, i) => ({
    name,
    path: parts.slice(0, i + 1).join('/'),
    isPage: i === parts.length - 1,
  }));
}
