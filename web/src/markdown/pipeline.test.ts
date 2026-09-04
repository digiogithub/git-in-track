import { toHtml } from 'hast-util-to-html';
import { beforeEach, describe, expect, it } from 'vitest';

import { clearMarkdownCache, readFrontMatter, renderMarkdown } from '@/markdown/pipeline';
import type { RenderOptions } from '@/markdown/types';

async function html(source: string, options: RenderOptions = {}): Promise<string> {
  const result = await renderMarkdown(source, { highlight: false, ...options });
  return toHtml(result.root);
}

beforeEach(() => {
  clearMarkdownCache();
});

describe('GFM', () => {
  it('renders tables', async () => {
    const out = await html('| a | b |\n|---|---|\n| 1 | 2 |');
    expect(out).toContain('<table>');
    expect(out).toContain('<th>a</th>');
    expect(out).toContain('<td>2</td>');
  });

  it('renders task lists with a checked, disabled checkbox', async () => {
    const out = await html('- [x] done\n- [ ] pending');
    expect(out).toContain('class="contains-task-list"');
    expect(out).toContain('<input type="checkbox" checked disabled>');
    expect(out).toContain('<input type="checkbox" disabled>');
  });

  it('renders strikethrough and autolinks', async () => {
    const out = await html('~~gone~~ and www.example.com');
    expect(out).toContain('<del>gone</del>');
    expect(out).toContain('href="http://www.example.com"');
  });

  it('renders footnotes with matching ids and back references', async () => {
    const out = await html('Text[^1]\n\n[^1]: The note.');
    expect(out).toContain('data-footnotes');
    expect(out).toContain('id="user-content-fn-1"');
    expect(out).toContain('href="#user-content-fn-1"');
    expect(out).toContain('The note.');
  });
});

describe('front matter', () => {
  it('strips the block from the output and exposes it', async () => {
    const source = '---\ntitle: Overview\ntags: [a]\n---\n\n# Overview\n';
    const result = await renderMarkdown(source, { highlight: false });
    expect(result.frontMatter).toBe('title: Overview\ntags: [a]');
    expect(toHtml(result.root)).not.toContain('title: Overview');
    expect(toHtml(result.root)).toContain('Overview');
  });

  it('returns null when there is none', () => {
    expect(readFrontMatter('# Just a heading')).toBeNull();
  });
});

describe('headings', () => {
  it('slugs headings, adds a permalink and reports the outline', async () => {
    const result = await renderMarkdown('# Title\n\n## Session revocation\n\n### Deep', {
      highlight: false,
    });
    expect(result.headings).toEqual([
      { depth: 1, id: 'title', text: 'Title' },
      { depth: 2, id: 'session-revocation', text: 'Session revocation' },
      { depth: 3, id: 'deep', text: 'Deep' },
    ]);
    expect(toHtml(result.root)).toContain('<a href="#session-revocation" class="heading-anchor"');
  });
});

describe('sanitisation', () => {
  it('drops raw script tags', async () => {
    const out = await html('<script>alert(1)</script>\n\nSafe.');
    expect(out).not.toContain('<script');
    expect(out).not.toContain('alert(1)');
    expect(out).toContain('Safe.');
  });

  it('drops raw HTML event handlers', async () => {
    const out = await html('<img src="x" onerror="alert(1)">');
    expect(out).not.toContain('onerror');
    expect(out).not.toContain('alert(1)');
  });

  it('drops javascript: URLs', async () => {
    const out = await html('[click](javascript:alert(1))');
    expect(out).not.toContain('javascript:');
  });

  it('drops data: URLs on images', async () => {
    const out = await html('![x](data:text/html;base64,PHN2Zz4=)');
    expect(out).not.toContain('data:text/html');
  });

  it('drops iframes and inline styles that no plugin produced', async () => {
    const out = await html('<iframe src="https://evil.example"></iframe>\n\n<b style="x">hi</b>');
    expect(out).not.toContain('<iframe');
    expect(out).not.toContain('style=');
  });

  it('keeps an external link but forces a safe rel', async () => {
    const out = await html('[docs](https://example.com/a)');
    expect(out).toContain('rel="noopener noreferrer"');
    expect(out).toContain('target="_blank"');
  });
});

describe('code fences', () => {
  it('keeps the language class when highlighting is off', async () => {
    const out = await html('```ts\nconst a: number = 1;\n```');
    expect(out).toContain('<pre><code class="language-ts">');
  });

  it('highlights with shiki and keeps both theme colours', async () => {
    const result = await renderMarkdown('```ts\nconst a: number = 1;\n```');
    const out = toHtml(result.root);
    expect(out).toContain('class="shiki shiki-themes github-light github-dark"');
    expect(out).toContain('--shiki-light');
    expect(out).toContain('--shiki-dark');
    expect(out).toContain('const');
  });

  it('leaves an unknown language as a plain block', async () => {
    const out = await html('```brainfuck\n+++\n```', { highlight: true });
    expect(out).toContain('language-brainfuck');
  });
});

describe('mermaid', () => {
  it('emits a placeholder holding the diagram source, never a rendered diagram', async () => {
    const result = await renderMarkdown('```mermaid\ngraph TD; A-->B;\n```');
    expect(result.hasMermaid).toBe(true);
    const out = toHtml(result.root);
    expect(out).toBe('<pre class="mermaid" data-mermaid="true">graph TD; A-->B;</pre>');
  });

  it('reports no mermaid for a page without a diagram', async () => {
    const result = await renderMarkdown('# Plain', { highlight: false });
    expect(result.hasMermaid).toBe(false);
  });
});

describe('relative links and images', () => {
  const options: RenderOptions = {
    basePath: 'docs/architecture/overview.md',
    highlight: false,
    resolveHref: (path) => ({ href: `/p/ACME/kb/${path}`, kind: 'page' }),
  };

  it('turns a relative image into an asset request', async () => {
    const out = await html('![diagram](../assets/flow.png)', options);
    expect(out).toContain('data-asset-path="docs/assets/flow.png"');
    expect(out).not.toContain('src=');
  });

  it('routes a relative link through the resolver, keeping the fragment', async () => {
    const out = await html('[sibling](./sso.md#session-revocation)', options);
    expect(out).toContain('href="/p/ACME/kb/docs/architecture/sso.md#session-revocation"');
    expect(out).toContain('data-kb-link="docs/architecture/sso.md"');
  });

  it('leaves an in-page fragment alone', async () => {
    const out = await html('[top](#title)', options);
    expect(out).toContain('href="#title"');
  });

  it('keeps external images but strips the referrer', async () => {
    const out = await html('![x](https://example.com/a.png)', options);
    expect(out).toContain('referrerpolicy="no-referrer"');
    expect(out).toContain('loading="lazy"');
  });
});

describe('math', () => {
  it('is off by default and opt-in per render', async () => {
    expect(await html('$a^2$')).toContain('$a^2$');
    expect(await html('$a^2$', { math: true })).toContain('math-inline');
  });
});

describe('caching', () => {
  it('returns the same result object for the same key', async () => {
    const first = await renderMarkdown('# A', { cacheKey: 'p@1', highlight: false });
    const second = await renderMarkdown('# A', { cacheKey: 'p@1', highlight: false });
    expect(second).toBe(first);

    const third = await renderMarkdown('# B', { cacheKey: 'p@2', highlight: false });
    expect(third).not.toBe(first);
  });
});
