import { toHtml } from 'hast-util-to-html';
import { describe, expect, it } from 'vitest';

import { renderMarkdown } from '@/markdown/pipeline';

async function html(source: string): Promise<string> {
  const result = await renderMarkdown(source, { highlight: false });
  return toHtml(result.root);
}

describe('remarkCallout', () => {
  it.each(['NOTE', 'TIP', 'IMPORTANT', 'WARNING', 'CAUTION'])(
    'renders the GitHub alert %s',
    async (type) => {
      const out = await html(`> [!${type}]\n> Body text.`);
      expect(out).toContain(`data-callout="${type.toLowerCase()}"`);
      expect(out).toContain(`callout-${type.toLowerCase()}`);
      expect(out).toContain('<div class="callout-title">');
      expect(out).toContain('Body text.');
      expect(out).not.toContain('<blockquote>');
    },
  );

  it('uses the type as the default title and a custom title when given', async () => {
    expect(await html('> [!NOTE]\n> Body.')).toContain('>Note</div>');
    expect(await html('> [!WARNING] Do not do this\n> Body.')).toContain('>Do not do this</div>');
  });

  it('renders an Obsidian collapsible callout as details/summary', async () => {
    const collapsed = await html('> [!info]- Hidden by default\n> Body.');
    expect(collapsed).toContain('<details class="callout callout-info"');
    expect(collapsed).toContain('<summary class="callout-title">Hidden by default</summary>');
    expect(collapsed).not.toContain('<details class="callout callout-info" open');

    const open = await html('> [!info]+ Shown by default\n> Body.');
    expect(open).toContain('open');
  });

  it('keeps block content inside the callout body', async () => {
    const out = await html('> [!TIP]\n> - one\n> - two\n>\n> ```\n> code\n> ```');
    expect(out).toContain('<div class="callout-body">');
    expect(out).toContain('<li>one</li>');
    expect(out).toContain('<pre>');
  });

  it('leaves an ordinary blockquote alone', async () => {
    const out = await html('> Just a quote.');
    expect(out).toContain('<blockquote>');
    expect(out).not.toContain('callout');
  });

  it('leaves an unknown callout type alone, so the class can never be chosen by a document', async () => {
    const out = await html('> [!evil-onload]\n> Body.');
    expect(out).toContain('<blockquote>');
    expect(out).not.toContain('callout-evil-onload');
  });

  it('renders inline markup inside the body', async () => {
    const out = await html('> [!NOTE]\n> See **bold** and `code`.');
    expect(out).toContain('<strong>bold</strong>');
    expect(out).toContain('<code>code</code>');
  });
});
