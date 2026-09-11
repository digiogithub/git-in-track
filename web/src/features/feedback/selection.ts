/**
 * Turns a DOM text selection inside rendered Markdown into what a feedback
 * note records: the quoted text and the source lines it came from.
 *
 * The lines come from the `data-line-start` / `data-line-end` stamps the
 * Markdown pipeline puts on block elements when rendered with `sourceLines`.
 * A selection whose ends sit in no stamped block (a highlighted code block, a
 * diagram) falls back to searching the quote in the source.
 */

export type SelectionAnchor = {
  quote: string;
  startLine?: number;
  endLine?: number;
  /** Where the selection sits, relative to the viewport. */
  rect: { top: number; bottom: number; left: number; right: number };
};

type LineRange = { start: number; end: number };

/** Collapses the whitespace of a selection into one line of text. */
export function collapseWhitespace(text: string): string {
  return text.replace(/\s+/g, ' ').trim();
}

function linesOf(element: Element | null): LineRange | undefined {
  if (!element) return undefined;
  const start = Number.parseInt(element.getAttribute('data-line-start') ?? '', 10);
  const end = Number.parseInt(element.getAttribute('data-line-end') ?? '', 10);
  if (!Number.isFinite(start) || start < 1) return undefined;
  return { start, end: Number.isFinite(end) && end >= start ? end : start };
}

/** The element a range boundary points into. */
function boundaryElement(node: Node, offset: number): Element | null {
  if (node.nodeType === Node.ELEMENT_NODE) {
    const element = node as Element;
    // A boundary between children (a triple-click selects a whole block this
    // way) points at the child after the offset.
    const child = element.childNodes[Math.min(offset, element.childNodes.length - 1)];
    if (child && child.nodeType === Node.ELEMENT_NODE) return child as Element;
    return element;
  }
  return node.parentElement;
}

function stampedAncestor(element: Element | null, container: HTMLElement): Element | null {
  const found = element?.closest('[data-line-start]') ?? null;
  return found && container.contains(found) ? found : null;
}

/**
 * Finds the source lines a quote came from when no stamp says so. Markdown
 * syntax and line breaks are ignored by comparing letters and digits only, so
 * a quote that wraps over several source lines is still found; when the whole
 * quote is not, its first words are enough to pin the start.
 */
export function locateQuote(quote: string, source: string): LineRange | undefined {
  const squash = (text: string) => text.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '');
  const wanted = squash(quote);
  if (wanted === '') return undefined;

  // One squashed string for the whole source, and where each line starts in it.
  const starts: number[] = [];
  let full = '';
  for (const line of source.split('\n')) {
    starts.push(full.length);
    full += squash(line);
  }

  let at = full.indexOf(wanted);
  let length = wanted.length;
  if (at < 0) {
    const head = wanted.slice(0, 24);
    at = full.indexOf(head);
    length = head.length;
  }
  if (at < 0) return undefined;

  const lineAt = (offset: number) => {
    let index = 0;
    while (index + 1 < starts.length && (starts[index + 1] ?? Infinity) <= offset) index += 1;
    return index + 1;
  };
  return { start: lineAt(at), end: lineAt(at + length - 1) };
}

function rectOf(range: Range, container: HTMLElement): SelectionAnchor['rect'] {
  // jsdom implements no layout, and neither `Range.getBoundingClientRect` nor
  // a real box exists there; the container is a sound fallback.
  const box =
    typeof range.getBoundingClientRect === 'function'
      ? range.getBoundingClientRect()
      : container.getBoundingClientRect();
  return { top: box.top, bottom: box.bottom, left: box.left, right: box.right };
}

/**
 * Reads the current selection when it lies inside `container`, or `null` when
 * there is none, it is empty, or it reaches outside the rendered document.
 */
export function readSelection(container: HTMLElement, source: string): SelectionAnchor | null {
  const selection = globalThis.getSelection?.();
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return null;
  const range = selection.getRangeAt(0);
  if (!container.contains(range.commonAncestorContainer)) return null;
  const quote = collapseWhitespace(selection.toString());
  if (quote === '') return null;

  const first = linesOf(
    stampedAncestor(boundaryElement(range.startContainer, range.startOffset), container),
  );
  const last = linesOf(
    stampedAncestor(boundaryElement(range.endContainer, range.endOffset), container),
  );
  let lines: LineRange | undefined;
  if (first && last) {
    lines = { start: Math.min(first.start, last.start), end: Math.max(first.end, last.end) };
  } else {
    lines = first ?? last ?? locateQuote(quote, source);
  }

  return {
    quote,
    ...(lines ? { startLine: lines.start, endLine: lines.end } : {}),
    rect: rectOf(range, container),
  };
}

/** `Lines 12–14`, `Line 3`, or the empty string when the lines are unknown. */
export function lineLabel(startLine?: number, endLine?: number): string {
  if (startLine === undefined) return '';
  if (endLine === undefined || endLine === startLine) return `Line ${startLine}`;
  return `Lines ${startLine}–${endLine}`;
}
