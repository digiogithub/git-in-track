import { useQuery } from '@tanstack/react-query';
import { Search, Sparkles } from 'lucide-react';
import { useDeferredValue, useId, useMemo, useState } from 'react';

import type { SearchHit } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { useUiPrefs } from '@/app/ui-prefs';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';

/** Nothing is queried below this length: one letter matches everything. */
const MIN_QUERY = 2;

/** How many hits the panel asks the core for. */
const SEARCH_LIMIT = 20;

/** Snippets longer than this are clamped behind an expand control. */
const SNIPPET_CLAMP = 180;

/** Terms shorter than this are not highlighted: they match almost any word. */
const MIN_TERM = 2;

/**
 * The query, split into the terms worth highlighting inside a snippet. The
 * terms are lower-cased and de-duplicated; matching happens on the snippet
 * itself, so a term the passage does not contain simply highlights nothing.
 */
function queryTerms(query: string): string[] {
  const seen = new Set<string>();
  for (const raw of query.split(/\s+/)) {
    const term = raw.trim().toLowerCase();
    if (term.length >= MIN_TERM) seen.add(term);
  }
  return [...seen];
}

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

/** A stable key for a hit: two repositories can hold the same path. */
function hitKey(hit: SearchHit): string {
  return `${hit.vaultId ?? ''}:${hit.path ?? hit.id ?? hit.title}`;
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
          onClick={() => {
            setExpanded(!expanded);
          }}
        >
          {expanded ? 'Show less' : 'Show more'}
        </button>
      ) : null}
    </p>
  );
}

/** One result row, with the snippet and relevance only on a semantic hit. */
function HitRow({ hit, terms }: { hit: SearchHit; terms: string[] }) {
  const semantic = hit.source === 'pando';
  const relevance = semantic ? relevanceLabel(hit.score) : null;
  return (
    <li className="flex flex-wrap items-baseline gap-2 rounded-md border border-border px-3 py-2 text-sm">
      <span className="font-medium">{hit.title || hit.path}</span>
      {hit.project ? <Badge variant="outline">{hit.project}</Badge> : null}
      <Badge size="sm">{hit.kind}</Badge>
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

/**
 * Search across every open repository (story GIT-US-0016).
 *
 * The core ranks each repository and merges the results, so one query covers
 * the team knowledge base and every project clone at once. Each row says which
 * project it came from, because in a workspace the same title can exist in two
 * repositories and the answer is only useful with its source attached.
 *
 * Where the companion reports a Pando index (`fullTextSearch: 'pando'`), the
 * passages it considered related follow the exact matches as their own
 * labelled group, so a guess is never mistaken for a hit (GIT-US-0086).
 */
export function WorkspaceSearch() {
  const provider = useProvider();
  const inputId = useId();
  const [text, setText] = useState('');
  const query = useDeferredValue(text.trim());
  const enabled = query.length >= MIN_QUERY;
  const semanticSupported = provider.capabilities.fullTextSearch === 'pando';
  const semanticEnabled = useUiPrefs((state) => state.semanticResults);
  const setSemanticResults = useUiPrefs((state) => state.setSemanticResults);
  const showSemantic = semanticSupported && semanticEnabled;

  const results = useQuery({
    queryKey: ['workspace-search', query],
    queryFn: () => provider.search({ text: query, limit: SEARCH_LIMIT }),
    enabled,
  });

  const hits = results.data?.hits ?? [];
  const exact = hits.filter((hit) => hit.source !== 'pando');
  const semantic = showSemantic ? hits.filter((hit) => hit.source === 'pando') : [];
  const terms = useMemo(() => queryTerms(query), [query]);
  const degraded = showSemantic && results.data?.degraded === true;
  const nothing = enabled && !results.isPending && exact.length === 0 && semantic.length === 0;

  return (
    <section aria-labelledby="workspace-search-heading" className="space-y-3">
      <h2 id="workspace-search-heading" className="text-base font-semibold tracking-tight">
        Search
      </h2>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Search aria-hidden="true" className="h-4 w-4" />
            Search every open repository
          </CardTitle>
          <CardDescription>
            Items and knowledge-base pages from every folder you have open, each labelled with the
            project it belongs to.
          </CardDescription>
        </CardHeader>

        <CardContent className="space-y-3">
          <label className="sr-only" htmlFor={inputId}>
            Search the workspace
          </label>
          <Input
            id={inputId}
            type="search"
            value={text}
            placeholder="Search items and pages…"
            onChange={(event) => {
              setText(event.target.value);
            }}
          />

          {semanticSupported ? (
            <div className="flex items-center gap-2">
              <Switch
                checked={semanticEnabled}
                onCheckedChange={setSemanticResults}
                aria-label="Show related by meaning"
              />
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <Sparkles aria-hidden="true" className="h-3 w-3" />
                Show related by meaning
              </span>
            </div>
          ) : null}

          {enabled && results.isPending ? (
            <p className="text-sm text-muted-foreground">Searching…</p>
          ) : null}

          {nothing ? <p className="empty-state">Nothing matched “{query}”.</p> : null}

          {exact.length > 0 ? (
            <div className="space-y-2">
              <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Exact matches
              </h3>
              <ul aria-label="Search results" className="space-y-2">
                {exact.map((hit) => (
                  <HitRow key={hitKey(hit)} hit={hit} terms={terms} />
                ))}
              </ul>
            </div>
          ) : null}

          {semantic.length > 0 ? (
            <div className="space-y-2">
              <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Related by meaning
              </h3>
              <ul aria-label="Related by meaning" className="space-y-2">
                {semantic.map((hit) => (
                  <HitRow key={hitKey(hit)} hit={hit} terms={terms} />
                ))}
              </ul>
            </div>
          ) : null}

          {degraded ? (
            <p className="text-xs text-muted-foreground">
              Semantic search unavailable — exact matches only.
            </p>
          ) : null}
        </CardContent>
      </Card>
    </section>
  );
}
