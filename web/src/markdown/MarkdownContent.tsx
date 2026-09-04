/**
 * hast → React. The tree is already sanitised (`renderMarkdown`), so this layer
 * only maps three elements onto components:
 *
 * - `a`   → the host's router link, so in-app navigation never reloads the page
 * - `img` → an asset request, because a file behind a directory handle has no URL
 * - `pre` → the lazy Mermaid renderer when the block is a diagram
 */

import type { Element } from 'hast';
import { toJsxRuntime, type Components } from 'hast-util-to-jsx-runtime';
import { Fragment, useEffect, useMemo, useState, type ComponentProps, type ReactNode } from 'react';
import { Fragment as JsxFragment, jsx, jsxs } from 'react/jsx-runtime';

import { cn } from '@/lib/cn';
import { classNames, textContent } from '@/markdown/code';
import { MarkdownContext, useMarkdownContext, type MarkdownContextValue } from '@/markdown/context';
import { MermaidBlock } from '@/markdown/MermaidBlock';
import '@/markdown/markdown.css';
import { isExternalUrl } from '@/markdown/paths';
import type { RenderResult } from '@/markdown/types';

type WithNode<T> = T & { node?: Element };

function stringProperty(node: Element | undefined, name: string): string | undefined {
  const value = node?.properties?.[name];
  return typeof value === 'string' ? value : undefined;
}

function MarkdownAnchor({ node, href, children, ...rest }: WithNode<ComponentProps<'a'>>) {
  const { renderLink } = useMarkdownContext();
  const external = stringProperty(node, 'dataExternal') !== undefined || isExternalUrl(href ?? '');

  if (renderLink && href && !external && !href.startsWith('#')) {
    const kind = stringProperty(node, 'dataKind');
    const itemRef = stringProperty(node, 'dataItemRef');
    const wikilink = stringProperty(node, 'dataWikilink');
    return renderLink({
      href,
      children,
      ...(rest.className ? { className: rest.className } : {}),
      ...(rest.title ? { title: rest.title } : {}),
      ...(kind ? { kind } : {}),
      ...(itemRef ? { itemRef } : {}),
      ...(wikilink ? { wikilink } : {}),
    });
  }

  return (
    <a href={href} {...rest}>
      {children}
    </a>
  );
}

function MarkdownImage({ node, src, alt, ...rest }: WithNode<ComponentProps<'img'>>) {
  const assetPath = stringProperty(node, 'dataAssetPath');
  const blocked = stringProperty(node, 'dataBlockedImage');

  if (blocked) {
    return <span className="asset-blocked">Image blocked by repository settings: {alt}</span>;
  }
  if (!assetPath) {
    return <img src={src} alt={alt ?? ''} {...rest} />;
  }
  return <AssetImage path={assetPath} alt={alt ?? ''} {...rest} />;
}

function AssetImage({ path, alt, ...rest }: { path: string } & ComponentProps<'img'>) {
  const { resolveAsset } = useMarkdownContext();
  const [src, setSrc] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!resolveAsset) return;
    let cancelled = false;
    resolveAsset(path).then(
      (url) => {
        if (!cancelled) setSrc(url);
      },
      () => {
        if (!cancelled) setFailed(true);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [path, resolveAsset]);

  if (failed || !resolveAsset) {
    return <span className="asset-missing">Missing image: {path}</span>;
  }
  if (!src) {
    return <span className="asset-loading" aria-busy="true" aria-label={`Loading ${path}`} />;
  }
  return <img src={src} alt={alt ?? ''} {...rest} />;
}

/** Wide tables scroll in their own container instead of widening the page. */
function MarkdownTable({ node: _node, children, ...rest }: WithNode<ComponentProps<'table'>>) {
  return (
    <div className="table-scroll">
      <table {...rest}>{children}</table>
    </div>
  );
}

function MarkdownPre({ node, children, ...rest }: WithNode<ComponentProps<'pre'>>) {
  if (node && classNames(node).includes('mermaid')) {
    return <MermaidBlock source={textContent(node)} />;
  }
  return <pre {...rest}>{children}</pre>;
}

const components = {
  a: MarkdownAnchor,
  img: MarkdownImage,
  pre: MarkdownPre,
  table: MarkdownTable,
  // Props carry the document's own `data-*` attributes, which `JSX
  // .IntrinsicElements` cannot describe; the components read them off `node`.
} as Partial<Components>;

export type MarkdownContentProps = {
  result: RenderResult;
  className?: string;
} & MarkdownContextValue;

/** Renders an already-rendered `RenderResult`. */
export function MarkdownContent({
  result,
  className,
  resolveAsset,
  renderLink,
}: MarkdownContentProps) {
  const context = useMemo<MarkdownContextValue>(
    () => ({
      ...(resolveAsset ? { resolveAsset } : {}),
      ...(renderLink ? { renderLink } : {}),
    }),
    [resolveAsset, renderLink],
  );

  const content = useMemo<ReactNode>(
    () =>
      toJsxRuntime(result.root, {
        Fragment: JsxFragment,
        jsx,
        jsxs,
        components,
        passNode: true,
      }),
    [result],
  );

  return (
    <MarkdownContext.Provider value={context}>
      <div className={cn('markdown-body', className)}>
        <Fragment>{content}</Fragment>
      </div>
    </MarkdownContext.Provider>
  );
}
