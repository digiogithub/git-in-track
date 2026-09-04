/**
 * The sanitisation schema (docs/05-web-app.md §7, "Sanitisation").
 *
 * Repository content is untrusted input. Raw HTML never reaches the tree
 * (`remark-rehype` runs with `allowDangerousHtml: false`), so everything the
 * schema sees was produced by our own plugins — but the schema is still written
 * as an allowlist, because a plugin bug must not become an XSS bug.
 *
 * Class names are allowed per element and per value, so a document cannot
 * choose an arbitrary class; `data-*` attributes are limited to the handful the
 * React renderer reads back.
 *
 * `clobberPrefix` is emptied on purpose: `mdast-util-to-hast` already emits
 * `user-content-` prefixed footnote ids together with matching `href`s, and a
 * second prefix here would break every footnote and heading anchor.
 */

import { defaultSchema } from 'rehype-sanitize';

type Schema = typeof defaultSchema;
type Attributes = NonNullable<Schema['attributes']>;
type AttributeValues = Attributes[string];

const CALLOUT_CLASS = /^callout-[a-z]+$/;
const LANGUAGE_CLASS = /^language-[\w+#.-]+$/;
const SHIKI_CLASS = /^shiki/;
/** Shiki's dual-theme markup names the themes in the class list. */
const SHIKI_THEME_CLASS = /^github-(light|dark)$/;

/**
 * Inherits the GitHub schema's attributes for one element minus its
 * `className` rule: `hast-util-sanitize` keeps only the first rule it sees for
 * a property, so every element that needs classes must declare a single,
 * complete `className` entry.
 */
function inherit(name: string): AttributeValues {
  const values: AttributeValues = defaultSchema.attributes?.[name] ?? [];
  return values.filter((value) => !(Array.isArray(value) && value[0] === 'className'));
}

const attributes: Attributes = {
  ...defaultSchema.attributes,
  a: [
    ...inherit('a'),
    'rel',
    'target',
    'referrerPolicy',
    'ariaHidden',
    'dataWikilink',
    'dataKind',
    'dataItemRef',
    'dataUnresolved',
    'dataKbLink',
    'dataExternal',
    [
      'className',
      'wikilink',
      'wikilink-page',
      'wikilink-item',
      'wikilink-missing',
      'wikilink-embed',
      'heading-anchor',
      'data-footnote-backref',
    ],
  ],
  code: [['className', LANGUAGE_CLASS, 'shiki', 'math-inline', 'math-display'], 'style'],
  pre: [
    ['className', SHIKI_CLASS, SHIKI_THEME_CLASS, 'mermaid'],
    'style',
    'tabIndex',
    'dataMermaid',
  ],
  span: [['className', SHIKI_CLASS, 'line', 'callout-icon', 'math-inline'], 'style'],
  div: [
    ...inherit('div'),
    'dataCallout',
    ['className', 'callout', 'callout-title', 'callout-body', 'mermaid', 'math-display', CALLOUT_CLASS],
  ],
  details: ['open', 'dataCallout', ['className', 'callout', CALLOUT_CLASS]],
  summary: [['className', 'callout-title']],
  section: [...inherit('section'), 'dataFootnotes', ['className', 'footnotes']],
  ul: [...inherit('ul'), ['className', 'contains-task-list']],
  ol: [...inherit('ol'), ['className', 'contains-task-list']],
  li: [...inherit('li'), 'id', ['className', 'task-list-item']],
  // Task-list checkboxes: `checked` is not in the GitHub schema but is exactly
  // what makes `- [x]` render as ticked.
  input: [...inherit('input'), 'checked'],
  img: [
    ...inherit('img'),
    'alt',
    'title',
    'width',
    'height',
    'loading',
    'referrerPolicy',
    'dataAssetPath',
    'dataBlockedImage',
    ['className', 'wikilink-embed'],
  ],
  h1: [['className', 'heading']],
  h2: [['className', 'heading']],
  h3: [['className', 'heading']],
  h4: [['className', 'heading']],
  h5: [['className', 'heading']],
  h6: [['className', 'heading']],
};

/** The hardened schema used for every KB page, item body and editor preview. */
export const kbSanitizeSchema: Schema = {
  ...defaultSchema,
  clobberPrefix: '',
  tagNames: [...(defaultSchema.tagNames ?? []), 'figure', 'figcaption'],
  protocols: {
    ...defaultSchema.protocols,
    // No `javascript:`, no `data:` (docs/05-web-app.md §7).
    href: ['http', 'https', 'mailto'],
    src: ['http', 'https'],
  },
  attributes,
};
