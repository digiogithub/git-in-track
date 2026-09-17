import { useQuery } from '@tanstack/react-query';
import { Search, Sparkles } from 'lucide-react';
import { useDeferredValue, useId, useMemo, useState } from 'react';

import { useProvider } from '@/api/provider-context';
import { useUiPrefs } from '@/app/ui-prefs';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { useProjects } from '@/features/backlog/queries';
import { hitKey, MIN_QUERY, queryTerms } from '@/features/search/search-hits';
import { HitRow } from '@/features/search/SearchHits';

import { useTeams } from './active-team';
import { SearchProjectFilter, type SearchProjectOption } from './SearchProjectFilter';

/** How many hits the panel asks the core for. */
const SEARCH_LIMIT = 20;

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

  // The project scope (GIT-US-0102). Team keys are offered next to project
  // keys because a team knowledge-base hit is labelled with its team key.
  const projects = useProjects();
  const teams = useTeams();
  const options = useMemo<SearchProjectOption[]>(() => {
    const seen = new Map<string, SearchProjectOption>();
    for (const scope of [...(projects.data ?? []), ...(teams.data ?? [])]) {
      if (!seen.has(scope.key)) seen.set(scope.key, { key: scope.key, name: scope.name });
    }
    return [...seen.values()];
  }, [projects.data, teams.data]);
  const [excluded, setExcluded] = useState<ReadonlySet<string>>(() => new Set());
  const selectedKeys = options.filter((option) => !excluded.has(option.key)).map((o) => o.key);
  const noneSelected = options.length > 0 && selectedKeys.length === 0;
  // Every project selected sends no scope at all: the same answer, cheaper.
  const projectKeys = selectedKeys.length === options.length ? undefined : selectedKeys;

  const results = useQuery({
    queryKey: ['workspace-search', query, projectKeys ?? null],
    queryFn: () =>
      provider.search({
        text: query,
        limit: SEARCH_LIMIT,
        ...(projectKeys ? { projectKeys } : {}),
      }),
    enabled: enabled && !noneSelected,
  });

  const hits = results.data?.hits ?? [];
  const exact = hits.filter((hit) => hit.source !== 'pando');
  const semantic = showSemantic ? hits.filter((hit) => hit.source === 'pando') : [];
  const terms = useMemo(() => queryTerms(query), [query]);
  const degraded = showSemantic && results.data?.degraded === true;
  const searching = enabled && !noneSelected;
  const nothing = searching && !results.isPending && exact.length === 0 && semantic.length === 0;

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
            Items, knowledge-base pages and — where Pando indexes the repository — source files,
            each labelled with the project it belongs to and the index it came from.
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

          {options.length > 1 ? (
            <SearchProjectFilter options={options} excluded={excluded} onChange={setExcluded} />
          ) : null}

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

          {noneSelected ? (
            <p className="text-sm text-muted-foreground">Select at least one project to search.</p>
          ) : null}

          {searching && results.isPending ? (
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
