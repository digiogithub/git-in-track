/**
 * The Markdown pipeline (docs/05-web-app.md §7). Feature code imports from here
 * and never reaches into a plugin module directly.
 */

export { MarkdownContent, type MarkdownContentProps } from '@/markdown/MarkdownContent';
export { MermaidBlock } from '@/markdown/MermaidBlock';
export {
  MarkdownContext,
  useMarkdownContext,
  type MarkdownContextValue,
  type MarkdownLinkProps,
  type MarkdownLinkRenderer,
} from '@/markdown/context';
export { clearMarkdownCache, readFrontMatter, renderMarkdown } from '@/markdown/pipeline';
export {
  basename,
  dirname,
  isExternalUrl,
  normalizePath,
  resolveFrom,
  stem,
} from '@/markdown/paths';
export { kbSanitizeSchema } from '@/markdown/sanitize';
export { readThemeMode, useThemeMode, type ThemeMode } from '@/markdown/theme';
export type {
  Heading,
  LinkResolution,
  RenderOptions,
  RenderResult,
  ResolveAsset,
  ResolveHref,
  ResolveLink,
  WikilinkKind,
  WikiTarget,
} from '@/markdown/types';
export { useAssetResolver, type AssetLoader } from '@/markdown/useAssetResolver';
export { useMarkdown, type MarkdownState } from '@/markdown/useMarkdown';
export { isItemId, parseWikiTarget } from '@/markdown/wikilink';
