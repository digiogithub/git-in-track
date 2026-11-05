import { Link } from '@tanstack/react-router';
import { AlertTriangle, BookOpen, FileCheck2, Flag, Info, Layers, ListChecks } from 'lucide-react';
import type { ReactNode } from 'react';

import type { ProjectKpis } from '@/api/provider';
import type { WorkspaceLayout } from '@/app/ui-prefs';
import { Progress } from '@/components/ui/progress';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/cn';

import { kpiEntries, progressFormula, progressPercent, useProjectKpis, type KpiKind } from './kpis';

const ICONS: Record<KpiKind, ReactNode> = {
  docs: <BookOpen aria-hidden="true" className="h-3.5 w-3.5" />,
  milestones: <Flag aria-hidden="true" className="h-3.5 w-3.5" />,
  epics: <Layers aria-hidden="true" className="h-3.5 w-3.5" />,
  stories: <ListChecks aria-hidden="true" className="h-3.5 w-3.5" />,
  specs: <FileCheck2 aria-hidden="true" className="h-3.5 w-3.5" />,
};

const LINK_CLASS =
  'inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-xs hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring';

/**
 * The figures of one project on the workspace home (GIT-US-0205): docs,
 * milestones, epics, stories and specs, each a link into the matching section,
 * and the estimated progress. Its own loading and error states keep one
 * failing project from blanking the page.
 */
export function ProjectKpiPanel({ project, layout }: { project: string; layout: WorkspaceLayout }) {
  const kpis = useProjectKpis(project);

  if (kpis.isPending) {
    return (
      <p className="text-xs text-muted-foreground" aria-live="polite">
        Loading {project} figures…
      </p>
    );
  }
  if (kpis.isError) {
    return (
      <p role="alert" className="flex items-center gap-1 text-xs text-destructive">
        <AlertTriangle aria-hidden="true" className="h-3.5 w-3.5" />
        The figures of {project} could not be read: {kpis.error.message}
      </p>
    );
  }
  return <KpiFigures kpis={kpis.data} layout={layout} />;
}

function KpiFigures({ kpis, layout }: { kpis: ProjectKpis; layout: WorkspaceLayout }) {
  const project = kpis.project;
  const percent = progressPercent(kpis);
  const formula = progressFormula(kpis);

  const entries = kpiEntries(kpis).map((entry) => {
    const content = (
      <>
        {ICONS[entry.kind]}
        <span className="text-muted-foreground">{entry.label}</span>
        <span className={cn('font-medium tabular-nums', entry.attention ? 'text-destructive' : '')}>
          {entry.value}
        </span>
      </>
    );
    const common = { className: LINK_CLASS, 'aria-label': entry.description } as const;
    switch (entry.kind) {
      case 'docs':
        return (
          <Link key={entry.kind} to="/p/$project/kb/$" params={{ project, _splat: '' }} {...common}>
            {content}
          </Link>
        );
      case 'milestones':
        return (
          <Link key={entry.kind} to="/p/$project/milestones" params={{ project }} {...common}>
            {content}
          </Link>
        );
      case 'epics':
        // An explicit type wins over any filter the backlog remembers.
        return (
          <Link
            key={entry.kind}
            to="/p/$project/items"
            params={{ project }}
            search={{ type: 'epic' }}
            {...common}
          >
            {content}
          </Link>
        );
      case 'stories':
        return (
          <Link
            key={entry.kind}
            to="/p/$project/items"
            params={{ project }}
            search={{ type: 'story' }}
            {...common}
          >
            {content}
          </Link>
        );
      case 'specs':
        return (
          <Link key={entry.kind} to="/p/$project/specs" params={{ project }} {...common}>
            {content}
          </Link>
        );
    }
  });

  return (
    <div className="space-y-2" data-testid={`kpis-${project}`}>
      <div
        className={cn(layout === 'cards' ? 'grid grid-cols-2 gap-1.5' : 'flex flex-wrap gap-1.5')}
      >
        {entries}
      </div>
      <div className="flex items-center gap-2">
        <Progress
          value={percent}
          max={100}
          label={`Estimated progress of ${project}`}
          className={layout === 'cards' ? 'flex-1' : 'max-w-xs flex-1'}
        />
        <TooltipProvider delayDuration={200}>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label={`${percent}% closed. ${formula}`}
                className="inline-flex items-center gap-1 rounded-sm text-xs tabular-nums text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                {percent}% closed
                <Info aria-hidden="true" className="h-3 w-3" />
              </button>
            </TooltipTrigger>
            <TooltipContent>{formula}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </div>
  );
}
