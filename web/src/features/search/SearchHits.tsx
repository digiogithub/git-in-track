import { useMemo, useState, type LiHTMLAttributes } from 'react';

import type { SearchHit } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/cn';

/**
 * The search hit rendering shared by the workspace search panel and the
 * project search overlay (story GIT-US-0103): the snippet highlighting, the
 * relevance and the semantic badges look the same wherever a hit is listed.
 * The query helpers that go with it live in `search-hits.ts`.
 */

/** Snippets longer than this are clamped behind an expand control. */
const SNIPPET_CLAMP = 180;

/** One run of snippet text, either a highlighted term or the prose around it. */
type SnippetPart = { text: string; match: boolean };

/**
 * Splits a snippet into highlighted and plain runs.
 *
 * The snippet is repository content and agent-adjacent output, so it never
 * becomes HTML: the caller renders each part as a text node, which is why this
 * returns parts rather than a marked-up string.
 */
function splitSnippet(snippet: string, terms: string[]): SnippetPart[] {
  if (terms.length === 0 || snippet === '') return [{ text: snippet, match: false }];
  const parts: SnippetPart[] = [];
  const lower = snippet.toLowerCase();
  let cursor = 0;
  while (cursor < snippet.length) {
    let at = -1;
    let length = 0;
    for (const term of terms) {
      const found = lower.indexOf(term, cursor);
      // The earliest match wins, and the longest one at the same offset, so
      // overlapping terms produce one run instead of a nested pair.
      if (found !== -1 && (at === -1 || found < at || (found === at && term.length > length))) {
        at = found;
        length = term.length;
      }
    }
    if (at === -1) {
      parts.push({ text: snippet.slice(cursor), match: false });
      break;
    }
    if (at > cursor) parts.push({ text: snippet.slice(cursor, at), match: false });
    parts.push({ text: snippet.slice(at, at + length), match: true });
    cursor = at + length;
  }
  return parts;
}

/**
 * The relevance of a semantic hit, as words rather than a bare float. Scores
 * between zero and one are read as a fraction; anything larger is an engine's
 * own scale and is shown rounded.
 */
function relevanceLabel(score: number | undefined): string | null {
  if (score === undefined || !Number.isFinite(score)) return null;
  if (score >= 0 && score <= 1) return `${Math.round(score * 100)}% match`;
  return `score ${score.toFixed(1)}`;
}

/**
 * The "why matched" passage: escaped text with the query terms it does contain
 * highlighted, clamped to a few lines behind a keyboard-reachable control.
 */
function Snippet({ text, terms }: { text: string; terms: string[] }) {
  const [expanded, setExpanded] = useState(false);
  const long = text.length > SNIPPET_CLAMP;
  const shown = long && !expanded ? `${text.slice(0, SNIPPET_CLAMP).trimEnd()}…` : text;
  const parts = useMemo(() => splitSnippet(shown, terms), [shown, terms]);

  return (
    <p className="w-full text-xs text-muted-foreground">
      {parts.map((part, index) =>
        part.match ? (
          // Text nodes only: a snippet carrying markup is shown, never run.
          <mark key={index} className="rounded-sm bg-accent/20 px-0.5 text-foreground">
            {part.text}
          </mark>
        ) : (
          <span key={index}>{part.text}</span>
        ),
      )}
      {long ? (
        <button
          type="button"
          aria-expanded={expanded}
          className="ml-1 underline underline-offset-2 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onClick={(event) => {
            // A row can be clickable (the overlay); expanding is not choosing.
            event.stopPropagation();
            setExpanded(!expanded);
          }}
        >
          {expanded ? 'Show less' : 'Show more'}
        </button>
      ) : null}
    </p>
  );
}

/**
 * What a semantic hit is labelled with: the Pando indexation it came from.
 *
 * Pando keeps two and they answer different questions — the knowledge base
 * holds the backlog and the documentation, the code index holds the source and
 * the Markdown outside it — so a row says which one found it and a code hit is
 * never read as a backlog item (story GIT-US-0098).
 */
function indexLabel(hit: SearchHit): string | null {
  switch (hit.index) {
    case 'code':
      return 'code index';
    case 'kb':
      return 'knowledge base';
    default:
      return null;
  }
}

/** What a row adds to a list item: the hit, and whether it is the selected one. */
type HitRowProps = LiHTMLAttributes<HTMLLIElement> & {
  hit: SearchHit;
  terms: string[];
  /** Set by a keyboard-driven list (the overlay); the workspace panel has none. */
  selected?: boolean;
};

/** One result row, with the snippet and relevance only on a semantic hit. */
export function HitRow({ hit, terms, selected = false, className, ...rest }: HitRowProps) {
  const semantic = hit.source === 'pando';
  const relevance = semantic ? relevanceLabel(hit.score) : null;
  const origin = semantic ? indexLabel(hit) : null;
  return (
    <li
      className={cn(
        'flex flex-wrap items-baseline gap-2 rounded-md border border-border px-3 py-2 text-sm',
        selected && 'border-accent bg-secondary',
        className,
      )}
      {...rest}
    >
      <span className="font-medium">{hit.title || hit.path}</span>
      {hit.project ? <Badge variant="outline">{hit.project}</Badge> : null}
      <Badge size="sm">{hit.kind}</Badge>
      {origin ? (
        <Badge size="sm" variant="info" title="Which Pando index found this">
          {origin}
        </Badge>
      ) : null}
      <span className="text-xs text-muted-foreground">{hit.path}</span>
      {relevance ? (
        <span className="text-xs text-muted-foreground/70" title="How close the passage is">
          {relevance}
        </span>
      ) : null}
      {semantic && hit.snippet ? <Snippet text={hit.snippet} terms={terms} /> : null}
    </li>
  );
}
