import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { useIdentity } from '@/features/backlog/identity';
import { quickViews, type ItemSearch } from '@/features/backlog/search';
import { useSetItemSearch } from '@/features/backlog/use-search';

const IDENTITY_HINT = 'Type your handle in the assignee filter once and "Mine" remembers it.';

/** Saved views. Each one is just a URL state, so it can be shared as a link. */
export function QuickViews({ search }: { search: ItemSearch }) {
  const setSearch = useSetItemSearch();
  const { identity } = useIdentity();

  return (
    <nav aria-label="Saved views" className="flex flex-wrap items-center gap-1">
      {quickViews.map((view) => {
        const disabled = view.needsIdentity === true && identity === null;
        const active = search.view === view.id;
        return (
          <Tooltip key={view.id}>
            <TooltipTrigger asChild>
              <Button
                variant={active ? 'secondary' : 'ghost'}
                size="sm"
                aria-pressed={active}
                disabled={disabled}
                {...(disabled ? { title: IDENTITY_HINT } : {})}
                onClick={() => {
                  setSearch(view.search(identity));
                }}
              >
                {view.name}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{disabled ? IDENTITY_HINT : view.description}</TooltipContent>
          </Tooltip>
        );
      })}
    </nav>
  );
}
