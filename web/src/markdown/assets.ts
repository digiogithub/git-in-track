/**
 * `rehype-resolve-assets` — repo-relative links and images.
 *
 * Images: the `src` is replaced by `data-asset-path`, because reading a file
 * from a File System Access handle is asynchronous and the pipeline is not.
 * The React `KbImage` component asks the provider for the bytes and manages the
 * object URL lifetime (docs/05-web-app.md §7, "Images and assets").
 *
 * Links: relative hrefs go through `resolveHref` so they become in-app routes;
 * external links get `rel="noopener noreferrer"`.
 */

import type { Element, Root } from 'hast';
import type { Plugin } from 'unified';
import { visit } from 'unist-util-visit';

import { isExternalUrl, isFragmentUrl, resolveFrom } from '@/markdown/paths';
import type { ResolveHref } from '@/markdown/types';

export type RehypeResolveAssetsOptions = {
  basePath?: string;
  resolveHref?: ResolveHref;
  externalImages?: boolean;
};

export const rehypeResolveAssets: Plugin<[RehypeResolveAssetsOptions?], Root> =
  (options = {}) =>
  (tree: Root) => {
    const basePath = options.basePath ?? '';
    const externalImages = options.externalImages ?? true;

    visit(tree, 'element', (node: Element) => {
      if (node.tagName === 'img') resolveImage(node, basePath, externalImages);
      else if (node.tagName === 'a') resolveAnchor(node, basePath, options.resolveHref);
    });
  };

function resolveImage(node: Element, basePath: string, externalImages: boolean): void {
  const properties = (node.properties ??= {});
  const src = properties['src'];
  if (typeof src !== 'string' || src === '') return;

  if (isExternalUrl(src)) {
    if (!externalImages) {
      delete properties['src'];
      properties['dataBlockedImage'] = src;
      return;
    }
    properties['referrerPolicy'] = 'no-referrer';
    properties['loading'] = 'lazy';
    return;
  }

  properties['dataAssetPath'] = resolveFrom(basePath, src);
  delete properties['src'];
}

function resolveAnchor(node: Element, basePath: string, resolveHref?: ResolveHref): void {
  const properties = (node.properties ??= {});
  const href = properties['href'];
  if (typeof href !== 'string' || href === '') return;

  // Wikilinks were already resolved by `remark-wikilink`.
  if (properties['dataWikilink'] !== undefined) return;

  if (isExternalUrl(href)) {
    properties['rel'] = ['noopener', 'noreferrer'];
    properties['target'] = '_blank';
    properties['dataExternal'] = 'true';
    return;
  }
  if (isFragmentUrl(href)) return;

  const [pathPart = '', fragment] = splitFragment(href);
  const vaultPath = resolveFrom(basePath, decodeURI(pathPart));
  const resolution = resolveHref?.(vaultPath);
  if (!resolution) return;

  properties['href'] = fragment ? `${resolution.href}#${fragment}` : resolution.href;
  properties['dataKbLink'] = vaultPath;
  properties['dataKind'] = resolution.kind;
  if (resolution.kind === 'missing') properties['dataUnresolved'] = 'true';
}

function splitFragment(href: string): [string, string | undefined] {
  const i = href.indexOf('#');
  return i === -1 ? [href, undefined] : [href.slice(0, i), href.slice(i + 1)];
}
