/**
 * `renderMarkdown` — the one unified processor of the app (docs/05-web-app.md §7).
 *
 *   remark-parse
 *     → remark-frontmatter(['yaml','toml'])   strip + expose front matter
 *     → remark-gfm                            tables, task lists, strikethrough,
 *                                             autolinks, footnotes
 *     → remark-math                           optional, off by default
 *     → remark-wikilink                       [[page]] / [[ITEM-ID]] / ![[embed]]
 *     → remark-callout                        > [!NOTE] and Obsidian folds
 *     → remark-rehype({ allowDangerousHtml: false })
 *     → rehype-slug + heading anchors
 *     → rehype-mermaid-placeholder            pre.mermaid, rendered client-side
 *     → rehype-resolve-assets                 repo-relative images and links
 *     → shiki (lazy, code-split)
 *     → rehype-sanitize(kbSanitizeSchema)     always last
 *
 * The result is a hast tree rather than an HTML string or React elements:
 * sanitising stays the last transform, tests can assert on the tree, and the
 * React layer (`MarkdownContent`) owns the component map and the object-URL
 * lifetimes.
 */

import rehypeSanitize from 'rehype-sanitize';
import rehypeSlug from 'rehype-slug';
import remarkFrontmatter from 'remark-frontmatter';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import remarkParse from 'remark-parse';
import remarkRehype from 'remark-rehype';
import { unified } from 'unified';

import { rehypeResolveAssets } from '@/markdown/assets';
import { remarkCallout } from '@/markdown/callout';
import { hasHighlightableCode } from '@/markdown/code';
import { collectHeadings, rehypeHeadingAnchors } from '@/markdown/headings';
import { rehypeMermaid } from '@/markdown/mermaid';
import { kbSanitizeSchema } from '@/markdown/sanitize';
import type { RenderOptions, RenderResult, WikiTarget } from '@/markdown/types';
import { remarkWikilink } from '@/markdown/wikilink';

const FRONT_MATTER = /^(?:---|\+\+\+)\r?\n([\s\S]*?)\r?\n(?:---|\+\+\+)(?:\r?\n|$)/;

/** Reads the raw front matter block. Parsing it is the provider's job. */
export function readFrontMatter(source: string): string | null {
  return FRONT_MATTER.exec(source)?.[1] ?? null;
}

/**
 * Memoises rendered pages, keyed by whatever the caller passes as `cacheKey`
 * (the KB viewer passes `path@rev`), so a page re-renders exactly when its
 * content or its options change.
 */
const CACHE_LIMIT = 50;
const cache = new Map<string, RenderResult>();

function cacheGet(key: string): RenderResult | undefined {
  const hit = cache.get(key);
  if (hit) {
    cache.delete(key);
    cache.set(key, hit);
  }
  return hit;
}

function cacheSet(key: string, value: RenderResult): void {
  cache.set(key, value);
  if (cache.size > CACHE_LIMIT) {
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) cache.delete(oldest);
  }
}

/** Drops the render cache (per-repo settings changed, or a test needs a clean slate). */
export function clearMarkdownCache(): void {
  cache.clear();
}

function optionsKey(options: RenderOptions): string {
  return [
    options.basePath ?? '',
    options.wikilinks === false ? 'w0' : 'w1',
    options.math ? 'm1' : 'm0',
    options.highlight === false ? 'h0' : 'h1',
    options.externalImages === false ? 'x0' : 'x1',
  ].join('|');
}

/** Renders one Markdown document into a sanitised hast tree plus its metadata. */
export async function renderMarkdown(
  source: string,
  options: RenderOptions = {},
): Promise<RenderResult> {
  const key = options.cacheKey ? `${options.cacheKey}|${optionsKey(options)}` : null;
  if (key) {
    const hit = cacheGet(key);
    if (hit) return hit;
  }

  const wikilinks: WikiTarget[] = [];
  const mermaid = { found: false };

  const processor = unified()
    .use(remarkParse)
    .use(remarkFrontmatter, ['yaml', 'toml'])
    .use(remarkGfm)
    .use(options.math ? [remarkMath] : [])
    .use(
      options.wikilinks === false
        ? []
        : [
            [
              remarkWikilink,
              {
                collect: wikilinks,
                ...(options.resolveLink ? { resolveLink: options.resolveLink } : {}),
              },
            ],
          ],
    )
    .use(remarkCallout)
    .use(remarkRehype, { allowDangerousHtml: false })
    .use(rehypeSlug)
    .use(rehypeHeadingAnchors)
    .use(rehypeMermaid, mermaid)
    .use(rehypeResolveAssets, {
      ...(options.basePath ? { basePath: options.basePath } : {}),
      ...(options.resolveHref ? { resolveHref: options.resolveHref } : {}),
      ...(options.externalImages === false ? { externalImages: false } : {}),
    });

  const tree = await processor.run(processor.parse(source));

  if (options.highlight !== false && hasHighlightableCode(tree)) {
    // The dynamic import is what makes Shiki a separate chunk: a prose-only
    // page never downloads it.
    const { highlightTree } = await import('@/markdown/highlight');
    await highlightTree(tree);
  }

  const root = unified().use(rehypeSanitize, kbSanitizeSchema).runSync(tree);

  const result: RenderResult = {
    root,
    headings: collectHeadings(root),
    wikilinks,
    frontMatter: readFrontMatter(source),
    hasMermaid: mermaid.found,
  };

  if (key) cacheSet(key, result);
  return result;
}
