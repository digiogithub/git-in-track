/**
 * Syntax highlighting — Shiki, loaded lazily.
 *
 * Why Shiki over `rehype-highlight`
 * ---------------------------------
 * docs/05-web-app.md §7 makes Shiki the default: it uses the same TextMate
 * grammars as VS Code, and its dual-theme output (`--shiki-light` /
 * `--shiki-dark` custom properties) means a theme switch is a CSS variable
 * swap, never a re-highlight. The bundle-size objection is answered by *how*
 * it is imported rather than by dropping to highlight.js:
 *
 * - This module is only ever reached through `await import()` from
 *   `pipeline.ts`, and only when a document actually contains a fenced code
 *   block, so Rollup emits it as its own chunk that the KB route never loads
 *   for a prose-only page.
 * - It uses `shiki/core` with a curated grammar set instead of `shiki` (whose
 *   barrel pulls in ~200 languages) — the fence languages our docs, ADRs and
 *   item bodies actually use.
 * - It uses the JavaScript regex engine instead of the Oniguruma one, which
 *   removes a ~600 kB WebAssembly download; `forgiving: true` degrades a
 *   grammar the JS engine cannot compile to plain text instead of throwing.
 *
 * `rehype-highlight` stays a viable alternative if the chunk ever becomes a
 * problem: the sanitize schema already accepts both class conventions.
 */

import type { Element, Root } from 'hast';
import { createHighlighterCore, type HighlighterCore } from 'shiki/core';
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript';
import bash from 'shiki/langs/bash.mjs';
import diff from 'shiki/langs/diff.mjs';
import go from 'shiki/langs/go.mjs';
import json from 'shiki/langs/json.mjs';
import markdown from 'shiki/langs/markdown.mjs';
import sql from 'shiki/langs/sql.mjs';
import typescript from 'shiki/langs/typescript.mjs';
import yaml from 'shiki/langs/yaml.mjs';
import githubDark from 'shiki/themes/github-dark.mjs';
import githubLight from 'shiki/themes/github-light.mjs';
import { visit } from 'unist-util-visit';

import { codeLanguage, MERMAID_LANGUAGE, textContent } from '@/markdown/code';

/** Light/dark pair; the CSS variables are swapped by the app theme. */
const THEMES = { light: 'github-light', dark: 'github-dark' } as const;

/**
 * The default set of docs/05-web-app.md §7 — ts, js, go, json, yaml, bash, sql,
 * md, diff. Static imports on purpose: they land in this module's chunk, so the
 * highlighter is one extra request instead of ten.
 *
 * JavaScript, JSX and TSX are served by the TypeScript grammar through
 * `LANGUAGE_ALIASES` rather than by their own grammars: each of those files
 * re-embeds the whole JavaScript grammar, and three copies of it tripled the
 * chunk for no visible difference in a fenced snippet.
 */
const LANGUAGES = [bash, diff, go, json, markdown, sql, typescript, yaml];

const LANGUAGE_ALIASES: Record<string, string> = {
  console: 'bash',
  golang: 'go',
  javascript: 'typescript',
  js: 'typescript',
  jsonc: 'json',
  jsx: 'typescript',
  mjs: 'typescript',
  patch: 'diff',
  sh: 'bash',
  shell: 'bash',
  tsx: 'typescript',
  zsh: 'bash',
};

let highlighter: Promise<HighlighterCore> | null = null;

function getHighlighter(): Promise<HighlighterCore> {
  highlighter ??= createHighlighterCore({
    themes: [githubLight, githubDark],
    langs: LANGUAGES,
    langAlias: LANGUAGE_ALIASES,
    engine: createJavaScriptRegexEngine({ forgiving: true }),
  });
  return highlighter;
}

/**
 * Replaces every `<pre><code class="language-x">` with Shiki's markup, in
 * place. Unknown languages and grammar failures are left untouched, so a page
 * always renders even when a fence claims an exotic language.
 */
export async function highlightTree(tree: Root): Promise<void> {
  const shiki = await getHighlighter();
  const loaded = new Set(shiki.getLoadedLanguages());
  const targets: { node: Element; index: number; parent: Root | Element; language: string }[] = [];

  visit(tree, 'element', (node: Element, index, parent) => {
    if (!parent || index === undefined) return;
    if (parent.type !== 'root' && parent.type !== 'element') return;
    const language = codeLanguage(node);
    if (!language || language === MERMAID_LANGUAGE || !loaded.has(language)) return;
    targets.push({ node, index, parent, language });
  });

  for (const target of targets) {
    try {
      const hast = shiki.codeToHast(textContent(target.node).replace(/\n$/, ''), {
        lang: target.language,
        themes: THEMES,
        // No `defaultColor` means colours ship as custom properties only, so
        // light and dark are one render.
        defaultColor: false,
      });
      const pre = hast.children.find(
        (child): child is Element => child.type === 'element' && child.tagName === 'pre',
      );
      if (pre) {
        normalizeProperties(pre);
        target.parent.children[target.index] = pre;
      }
    } catch {
      // Keep the plain <pre><code> fallback.
    }
  }
}

/**
 * Shiki emits raw HTML attribute names (`class`, `tabindex`); the rest of the
 * pipeline — the sanitize schema included — speaks hast property names. Convert
 * once here so the schema never has to allow both spellings.
 */
function normalizeProperties(root: Element): void {
  visit(root, 'element', (node: Element) => {
    const properties = (node.properties ??= {});
    const className = properties['class'];
    if (typeof className === 'string') {
      properties['className'] = className.split(/\s+/).filter(Boolean);
      delete properties['class'];
    }
    const tabIndex = properties['tabindex'];
    if (tabIndex !== undefined) {
      properties['tabIndex'] = Number(tabIndex);
      delete properties['tabindex'];
    }
  });
}
