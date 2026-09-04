/**
 * Shared types for the knowledge base Markdown pipeline (docs/05-web-app.md §7).
 *
 * The pipeline is deliberately free of routing and provider knowledge: callers
 * inject `resolveLink` / `resolveHref` / `resolveAsset`, so the same processor
 * renders project pages, team pages, item bodies and editor previews.
 */

import type { Root as HastRoot } from 'hast';

/** What a wikilink target points at, before resolution. */
export type WikilinkKind = 'page' | 'item';

/**
 * A parsed `[[…]]` target. All forms of docs/03-data-model.md §14.1 map onto
 * this shape:
 *
 * | Source                          | Result                                                              |
 * |---------------------------------|---------------------------------------------------------------------|
 * | `[[page]]`                      | `{ kind: 'page', target: 'page' }`                                   |
 * | `[[dir/page]]`                  | `{ kind: 'page', target: 'dir/page' }`                               |
 * | `[[ACME-US-0042]]`              | `{ kind: 'item', target: 'ACME-US-0042', projectKey: 'ACME' }`       |
 * | `[[WEB/WEB-US-0031]]`           | `{ kind: 'item', target: 'WEB-US-0031', projectKey: 'WEB' }`         |
 * | `[[WEB:architecture/overview]]` | `{ kind: 'page', target: 'architecture/overview', projectKey: 'WEB' }` |
 * | `[[target\|alias]]`             | `{ alias: 'alias' }`                                                 |
 * | `[[target#anchor]]`             | `{ anchor: 'anchor' }`                                               |
 * | `![[embed]]`                    | `{ embed: true }`                                                    |
 */
export type WikiTarget = {
  /** The raw text between the brackets, alias and anchor included. */
  raw: string;
  kind: WikilinkKind;
  /** Item id or page path: no anchor, no alias, no project prefix. */
  target: string;
  /** Set for `KEY/ITEM-ID` and `KEY:page`, and derived from the id prefix for items. */
  projectKey?: string;
  /** Heading slug for pages, comment file stem for items. */
  anchor?: string;
  /** Custom link text from `[[target|alias]]`. */
  alias?: string;
  /** True for `![[…]]` transclusion syntax. */
  embed: boolean;
};

/**
 * The caller's answer for one link. `kind: 'missing'` renders the "broken link"
 * styling required by R-WIKI-2; it is never an error.
 */
export type LinkResolution = {
  href: string;
  kind: 'page' | 'item' | 'missing';
  /** Overrides the default link text (e.g. `ACME-US-0042 — Login with SSO`). */
  label?: string;
  /** `title` attribute, e.g. the resolved vault path. */
  title?: string;
};

export type ResolveLink = (target: WikiTarget) => LinkResolution;

/** Resolves a plain relative Markdown link (`./sibling.md`) to an app route. */
export type ResolveHref = (vaultPath: string) => LinkResolution;

/** Turns a vault-relative asset path into a URL (object URL or companion URL). */
export type ResolveAsset = (vaultPath: string) => Promise<string>;

export type Heading = {
  depth: number;
  id: string;
  text: string;
};

export type RenderOptions = {
  /**
   * Vault path of the page being rendered (`docs/architecture/overview.md`).
   * Relative links and images are resolved against its directory.
   */
  basePath?: string;
  /** Resolution for `[[…]]` targets. Defaults to a self-referential resolver. */
  resolveLink?: ResolveLink;
  /** Resolution for relative Markdown links. Defaults to leaving the href alone. */
  resolveHref?: ResolveHref;
  /** `docs.wikilinks: false` renders `[[…]]` literally (R-WIKI-4). Default true. */
  wikilinks?: boolean;
  /** `$…$` / `$$…$$` support. Off by default; KaTeX is not bundled. */
  math?: boolean;
  /** Syntax highlighting through the lazily loaded Shiki chunk. Default true. */
  highlight?: boolean;
  /** Allow `http(s)` images from outside the vault. Default true. */
  externalImages?: boolean;
  /** When set, the result is memoised under this key (use `path@rev`). */
  cacheKey?: string;
};

export type RenderResult = {
  /** Sanitised hast tree, ready for `hast-util-to-jsx-runtime`. */
  root: HastRoot;
  /** Document outline, in document order, for the table of contents. */
  headings: Heading[];
  /** Every `[[…]]` found, in document order. */
  wikilinks: WikiTarget[];
  /** Raw YAML front matter text, stripped from the rendered output. */
  frontMatter: string | null;
  /** True when at least one ```mermaid fence is present. */
  hasMermaid: boolean;
};
