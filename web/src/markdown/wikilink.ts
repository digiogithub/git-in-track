/**
 * `remark-wikilink` — Obsidian-style `[[…]]` links (docs/03-data-model.md §14).
 *
 * The plugin only *parses and shapes* links; resolution is injected, because
 * the link graph lives in the Go core and reaches the UI through the provider.
 * Unresolved links are rendered, never dropped (R-WIKI-2).
 */

import GithubSlugger from 'github-slugger';
import type { Image, Link, Parent, PhrasingContent, Root, Text } from 'mdast';
import type { Plugin } from 'unified';

// Loads the `hName`/`hProperties` augmentation of `mdast`'s `Data`.
import type {} from 'mdast-util-to-hast';

import type { LinkResolution, ResolveLink, WikiTarget } from '@/markdown/types';

/** `<KEY>-<TYPECODE>-<NUMBER>` — docs/03-data-model.md §3.3. */
const ITEM_ID = /^([A-Z][A-Z0-9]{1,9})-(?:EP|US|T|M)-\d{4,}$/;
const PROJECT_KEY = /^[A-Z][A-Z0-9]{1,9}$/;
/** `[[target]]` and `![[target]]`; targets never span lines. */
const WIKILINK = /(!?)\[\[([^\][\n]+)\]\]/g;

const IMAGE_EXTENSION = /\.(png|jpe?g|gif|svg|webp|avif|bmp|ico)$/i;

/** True when a string is a valid item id. Exported for the KB resolver. */
export function isItemId(value: string): boolean {
  return ITEM_ID.test(value);
}

/**
 * Parses the text between the brackets. Returns `null` for an empty target so
 * that `[[]]` stays literal text.
 */
export function parseWikiTarget(raw: string, embed = false): WikiTarget | null {
  const pipe = raw.indexOf('|');
  const aliasPart = pipe === -1 ? '' : raw.slice(pipe + 1).trim();
  let head = (pipe === -1 ? raw : raw.slice(0, pipe)).trim();

  const hash = head.indexOf('#');
  const anchorPart = hash === -1 ? '' : head.slice(hash + 1).trim();
  head = (hash === -1 ? head : head.slice(0, hash)).trim();

  if (head === '' && anchorPart === '') return null;

  const optional = {
    ...(aliasPart ? { alias: aliasPart } : {}),
    ...(anchorPart ? { anchor: anchorPart } : {}),
  };

  // `KEY:page` — a page in another project's docs folder.
  const scoped = /^([A-Z][A-Z0-9]{1,9}):(.+)$/.exec(head);
  if (scoped) {
    return {
      raw,
      kind: 'page',
      target: normalizeTarget(scoped[2] ?? ''),
      projectKey: scoped[1] as string,
      embed,
      ...optional,
    };
  }

  // `KEY/ITEM-ID` — a cross-project item (§14.1, soft reference).
  const slash = head.indexOf('/');
  if (slash > 0) {
    const key = head.slice(0, slash);
    const rest = head.slice(slash + 1);
    if (PROJECT_KEY.test(key) && ITEM_ID.test(rest)) {
      return { raw, kind: 'item', target: rest, projectKey: key, embed, ...optional };
    }
  }

  // R-WIKI-1: a target matching the ID grammar is an item, anything else a page.
  const asItem = ITEM_ID.exec(head);
  if (asItem) {
    return { raw, kind: 'item', target: head, projectKey: asItem[1] as string, embed, ...optional };
  }

  return { raw, kind: 'page', target: normalizeTarget(head), embed, ...optional };
}

/** Strips a leading `./` and a trailing `.md`; keeps the rest verbatim. */
function normalizeTarget(target: string): string {
  let out = target.trim();
  while (out.startsWith('./')) out = out.slice(2);
  return out.replace(/\.md$/i, '');
}

/** Fallback resolver: keeps the document self-describing when none is injected. */
export function defaultResolveLink(target: WikiTarget): LinkResolution {
  return { href: `#${encodeURIComponent(target.target)}`, kind: target.kind };
}

/** The visible text when neither an alias nor a resolver label is available. */
export function defaultLabel(target: WikiTarget): string {
  if (target.alias) return target.alias;
  if (!target.target) return `#${target.anchor ?? ''}`;
  return target.anchor ? `${target.target} › ${target.anchor}` : target.target;
}

function anchorFragment(target: WikiTarget): string {
  if (!target.anchor) return '';
  // Page anchors are heading slugs (they must match rehype-slug); item anchors
  // are comment file stems and are used verbatim (R-ID-4).
  if (target.kind === 'item') return `#${encodeURIComponent(target.anchor)}`;
  return `#${new GithubSlugger().slug(target.anchor)}`;
}

function buildNode(target: WikiTarget, resolution: LinkResolution): PhrasingContent {
  const href = resolution.href.includes('#')
    ? resolution.href
    : `${resolution.href}${anchorFragment(target)}`;
  const label = target.alias ?? resolution.label ?? defaultLabel(target);

  if (target.embed && IMAGE_EXTENSION.test(target.target)) {
    const image: Image = {
      type: 'image',
      url: href,
      alt: label,
      title: resolution.title ?? null,
      data: { hProperties: { className: ['wikilink-embed'] } },
    };
    return image;
  }

  const className = ['wikilink', `wikilink-${resolution.kind}`];
  if (target.embed) className.push('wikilink-embed');

  const link: Link = {
    type: 'link',
    url: href,
    title: resolution.title ?? null,
    children: [{ type: 'text', value: label }],
    data: {
      hProperties: {
        className,
        dataWikilink: target.target,
        dataKind: resolution.kind,
        ...(target.kind === 'item' ? { dataItemRef: target.target } : {}),
        ...(resolution.kind === 'missing' ? { dataUnresolved: 'true' } : {}),
      },
    },
  };
  return link;
}

export type RemarkWikilinkOptions = {
  resolveLink?: ResolveLink;
  /** Every target found, in document order. The caller owns the array. */
  collect?: WikiTarget[];
};

/**
 * Splits text nodes on `[[…]]`. Text inside an existing link is left alone so
 * `[label]([[x]])` and nested links cannot produce an `<a>` inside an `<a>`.
 */
export const remarkWikilink: Plugin<[RemarkWikilinkOptions?], Root> = (options = {}) => {
  const resolve = options.resolveLink ?? defaultResolveLink;

  const walk = (parent: Parent, insideLink: boolean): void => {
    const next: typeof parent.children = [];
    let changed = false;

    for (const child of parent.children) {
      if (child.type === 'text' && !insideLink) {
        const replacement = splitText(child, resolve, options.collect);
        if (replacement) {
          changed = true;
          next.push(...replacement);
          continue;
        }
      } else if ('children' in child) {
        walk(child, insideLink || child.type === 'link' || child.type === 'linkReference');
      }
      next.push(child);
    }

    if (changed) parent.children = next;
  };

  return (tree: Root) => {
    walk(tree, false);
  };
};

function splitText(
  node: Text,
  resolve: ResolveLink,
  collect: WikiTarget[] | undefined,
): PhrasingContent[] | null {
  const value = node.value;
  WIKILINK.lastIndex = 0;
  if (!WIKILINK.test(value)) return null;
  WIKILINK.lastIndex = 0;

  const out: PhrasingContent[] = [];
  let cursor = 0;
  let match: RegExpExecArray | null;

  while ((match = WIKILINK.exec(value)) !== null) {
    const target = parseWikiTarget(match[2] ?? '', match[1] === '!');
    if (!target) continue;

    if (match.index > cursor) {
      out.push({ type: 'text', value: value.slice(cursor, match.index) });
    }
    collect?.push(target);
    out.push(buildNode(target, resolve(target)));
    cursor = match.index + match[0].length;
  }

  if (out.length === 0) return null;
  if (cursor < value.length) out.push({ type: 'text', value: value.slice(cursor) });
  return out;
}
