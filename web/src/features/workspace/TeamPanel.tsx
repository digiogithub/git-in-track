import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { BookOpen, CloudOff, ExternalLink, ListChecks, Users } from 'lucide-react';

import type { TeamProjectSummary } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

/**
 * The team repository of the workspace (docs/04-team-repository.md §3, story
 * GIT-US-0016): who is on the team, which project repositories it owns, and
 * which of those are actually cloned on this machine.
 *
 * A project the workspace has not opened is listed and marked "not cloned"; it
 * is never hidden. Rendering its cards from a committed snapshot is
 * GIT-US-0019's job, so this panel shows the declaration and the way in.
 */
export function TeamPanel() {
  const provider = useProvider();
  const team = useQuery({ queryKey: ['team'], queryFn: () => provider.getTeam() });

  if (team.isPending || !team.data) return null;

  const { key, name, description, members, projects, knowledgePath, diagnostics } = team.data;
  const active = members.filter((member) => member.active);
  const errors = diagnostics.filter((d) => d.severity === 'error');

  return (
    <section aria-labelledby="team-heading" className="space-y-3">
      <h2 id="team-heading" className="text-lg font-semibold tracking-tight">
        Team
      </h2>

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2">
            <Users aria-hidden="true" className="h-4 w-4" />
            {name}
            <Badge variant="outline">{key}</Badge>
          </CardTitle>
          <CardDescription>
            {description ? `${description} · ` : ''}
            knowledge base: <code>{knowledgePath}</code>
          </CardDescription>
        </CardHeader>

        <CardContent className="space-y-4">
          {errors.length > 0 ? (
            <ul role="alert" className="space-y-1 text-sm text-destructive">
              {errors.map((diagnostic) => (
                <li key={`${diagnostic.code}-${diagnostic.message}`}>
                  <code>{diagnostic.code}</code> {diagnostic.message}
                </li>
              ))}
            </ul>
          ) : null}

          <div>
            <Link
              to="/p/$project/kb/$"
              params={{ project: key, _splat: '' }}
              className="inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs hover:bg-secondary"
            >
              <BookOpen aria-hidden="true" className="h-3 w-3" />
              Team knowledge base
            </Link>
          </div>

          <div className="space-y-2">
            <h3 className="text-sm font-medium">Members ({active.length} active)</h3>
            <ul className="flex flex-wrap gap-2">
              {members.map((member) => (
                <li key={member.handle}>
                  <Badge variant={member.active ? 'default' : 'outline'}>
                    {member.name ?? member.handle}
                    {member.role ? ` · ${member.role}` : ''}
                    {member.active ? '' : ' · inactive'}
                  </Badge>
                </li>
              ))}
            </ul>
          </div>

          <div className="space-y-2">
            <h3 className="text-sm font-medium">Projects ({projects.length})</h3>
            <ul className="space-y-2">
              {projects.map((project) => (
                <TeamProjectRow key={project.key} project={project} />
              ))}
            </ul>
          </div>
        </CardContent>
      </Card>
    </section>
  );
}

/** One declared project: cloned and browsable, or remote and marked as such. */
function TeamProjectRow({ project }: { project: TeamProjectSummary }) {
  return (
    <li className="flex flex-wrap items-center gap-2 rounded-md border border-border px-3 py-2 text-sm">
      <span className="font-medium">{project.name}</span>
      <Badge variant="outline">{project.key}</Badge>
      {project.archived ? <Badge variant="outline">archived</Badge> : null}

      {project.cloned ? (
        <>
          <Badge>cloned</Badge>
          <Link
            to="/p/$project/items"
            params={{ project: project.key }}
            className="inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs hover:bg-secondary"
          >
            <ListChecks aria-hidden="true" className="h-3 w-3" />
            {project.key} backlog
          </Link>
        </>
      ) : (
        <>
          <Badge variant="accent">
            <CloudOff aria-hidden="true" className="h-3 w-3" />
            not cloned
          </Badge>
          <span className="text-xs text-muted-foreground">
            Clone <code>{project.repo}</code> and open it to work on this project.
          </span>
        </>
      )}

      {project.webUrl ? (
        <a
          href={project.webUrl}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-xs underline"
        >
          <ExternalLink aria-hidden="true" className="h-3 w-3" />
          Open on the git host
        </a>
      ) : null}
    </li>
  );
}
