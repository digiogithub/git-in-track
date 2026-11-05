/**
 * The per-project figures of the workspace home (GIT-US-0205, docs/05-web-app.md
 * §3.1.2). The numbers are the core's `project.kpis` answer — the same in
 * browser-only and companion mode; this module only fetches them and turns them
 * into labels.
 */

import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';

import type { ProjectKpis } from '@/api/provider';
import { useProvider } from '@/api/provider-context';

export const kpiKeys = {
  all: () => ['kpis'] as const,
  project: (project: string) => ['kpis', project] as const,
};

export function useProjectKpis(project: string, enabled = true) {
  const provider = useProvider();
  return useQuery({
    queryKey: kpiKeys.project(project),
    queryFn: () => provider.getProjectKpis(project),
    enabled: enabled && project !== '',
  });
}

/**
 * Refetches the figures whenever an item, a page or a whole index changes, in
 * either mode: the workspace home sits outside any project, so no project
 * layout invalidates them for it.
 */
export function useKpiEvents(): void {
  const provider = useProvider();
  const queryClient = useQueryClient();
  useEffect(
    () =>
      provider.subscribe((event) => {
        if (
          event.kind === 'items' ||
          event.kind === 'kb' ||
          event.kind === 'index' ||
          event.kind === 'repo'
        ) {
          void queryClient.invalidateQueries({ queryKey: kpiKeys.all() });
        }
      }),
    [provider, queryClient],
  );
}

/** The closed share as a whole percentage, 0–100. */
export function progressPercent(kpis: ProjectKpis): number {
  const ratio = Number.isFinite(kpis.progress.ratio) ? kpis.progress.ratio : 0;
  return Math.round(Math.min(1, Math.max(0, ratio)) * 100);
}

function points(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

/** The formula behind the progress bar, with this project's own numbers. */
export function progressFormula(kpis: ProjectKpis): string {
  const { progress, stories } = kpis;
  if (progress.scope === 0) {
    return 'No open or done stories yet: progress is done stories ÷ non-cancelled stories.';
  }
  if (progress.weighted) {
    return (
      `Estimated ${progressPercent(kpis)}% closed: ${points(progress.done)} of ` +
      `${points(progress.scope)} story points done. Points of done stories ÷ points of ` +
      'non-cancelled stories; a story without an estimate counts as the average estimate ' +
      `(${stories.estimated} of ${stories.total - stories.cancelled} stories are estimated).`
    );
  }
  return (
    `Estimated ${progressPercent(kpis)}% closed: ${progress.done} of ${progress.scope} ` +
    'stories done. Done stories ÷ non-cancelled stories; no story carries an estimate, ' +
    'so each counts the same.'
  );
}

/** Where a KPI leads, by kind. */
export type KpiKind = 'docs' | 'milestones' | 'epics' | 'stories' | 'specs';

export type KpiEntry = {
  kind: KpiKind;
  /** Short visible label, e.g. "Epics". */
  label: string;
  /** The figure as shown, e.g. "3 / 7 open". */
  value: string;
  /** Accessible name of the link, spelled out. */
  description: string;
  /** Whether the figure calls for attention (unverified specs). */
  attention?: boolean;
};

function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** The five linked figures of one project, in display order. */
export function kpiEntries(kpis: ProjectKpis): KpiEntry[] {
  const { kbPages, milestones, epics, stories, specs } = kpis;
  return [
    {
      kind: 'docs',
      label: 'Docs',
      value: String(kbPages),
      description: `${plural(kbPages, 'knowledge-base doc')} in ${kpis.project}`,
    },
    {
      kind: 'milestones',
      label: 'Milestones',
      value: `${milestones.open} open / ${milestones.total}`,
      description: `${plural(milestones.total, 'milestone')}, ${milestones.open} open, in ${kpis.project}`,
    },
    {
      kind: 'epics',
      label: 'Epics',
      value: `${epics.open} open / ${epics.total}`,
      description: `${plural(epics.total, 'epic')}, ${epics.open} open, in ${kpis.project}`,
    },
    {
      kind: 'stories',
      label: 'Stories',
      value: `${stories.open} left / ${stories.total}`,
      description: `${plural(stories.total, 'story', 'stories')}, ${stories.open} remaining, in ${kpis.project}`,
    },
    {
      kind: 'specs',
      label: 'Specs',
      value:
        specs.unverified > 0
          ? `${specs.total} · ${specs.unverified} unverified`
          : String(specs.total),
      description:
        `${plural(specs.total, 'spec')}, ${specs.unverified} with unverified requirements, ` +
        `in ${kpis.project}`,
      attention: specs.unverified > 0,
    },
  ];
}
