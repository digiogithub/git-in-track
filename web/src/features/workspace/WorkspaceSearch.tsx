import { useQuery } from '@tanstack/react-query';
import { Search } from 'lucide-react';
import { useDeferredValue, useId, useState } from 'react';

import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';

/** Nothing is queried below this length: one letter matches everything. */
const MIN_QUERY = 2;

/** How many hits the panel asks the core for. */
const SEARCH_LIMIT = 20;

/**
 * Search across every open repository (story GIT-US-0016).
 *
 * The core ranks each repository and merges the results, so one query covers
 * the team knowledge base and every project clone at once. Each row says which
 * project it came from, because in a workspace the same title can exist in two
 * repositories and the answer is only useful with its source attached.
 */
export function WorkspaceSearch() {
  const provider = useProvider();
  const inputId = useId();
  const [text, setText] = useState('');
  const query = useDeferredValue(text.trim());
  const enabled = query.length >= MIN_QUERY;

  const results = useQuery({
    queryKey: ['workspace-search', query],
    queryFn: () => provider.search({ text: query, limit: SEARCH_LIMIT }),
    enabled,
  });

  const hits = results.data ?? [];

  return (
    <section aria-labelledby="workspace-search-heading" className="space-y-3">
      <h2 id="workspace-search-heading" className="text-lg font-semibold tracking-tight">
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

          {enabled && results.isPending ? (
            <p className="text-sm text-muted-foreground">Searching…</p>
          ) : null}

          {enabled && !results.isPending && hits.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing matched “{query}”.</p>
          ) : null}

          {hits.length > 0 ? (
            <ul aria-label="Search results" className="space-y-2">
              {hits.map((hit) => (
                <li
                  key={`${hit.vaultId ?? ''}:${hit.path}`}
                  className="flex flex-wrap items-baseline gap-2 rounded-md border border-border px-3 py-2 text-sm"
                >
                  <span className="font-medium">{hit.title || hit.path}</span>
                  {hit.project ? <Badge variant="outline">{hit.project}</Badge> : null}
                  <Badge size="sm">{hit.kind}</Badge>
                  <span className="text-xs text-muted-foreground">{hit.path}</span>
                </li>
              ))}
            </ul>
          ) : null}
        </CardContent>
      </Card>
    </section>
  );
}
