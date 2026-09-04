import { useQuery } from '@tanstack/react-query';
import { Link, Outlet } from '@tanstack/react-router';
import { BookOpen, Boxes, LayoutDashboard, ListChecks, Lock, Settings, X } from 'lucide-react';
import { useEffect, type ReactNode } from 'react';

import { useOptionalProvider } from '@/api/provider-context';
import { useAppStore } from '@/app/store';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/cn';

type NavItem = {
  to: string;
  label: string;
  icon: ReactNode;
};

const navItems: NavItem[] = [
  { to: '/', label: 'Workspace', icon: <LayoutDashboard aria-hidden="true" className="h-4 w-4" /> },
  { to: '/boards', label: 'Boards', icon: <Boxes aria-hidden="true" className="h-4 w-4" /> },
  { to: '/settings', label: 'Settings', icon: <Settings aria-hidden="true" className="h-4 w-4" /> },
];

/** Where the companion binary is published (docs/09-ci-cd-and-releases.md). */
const COMPANION_DOWNLOAD_URL = 'https://github.com/digiogithub/git-in-track/releases';

/** Application shell: skip link, sidebar navigation and the routed main region. */
export function AppShell() {
  const mode = useAppStore((state) => state.mode);
  const capabilities = useAppStore((state) => state.capabilities);
  const setCapabilities = useAppStore((state) => state.setCapabilities);
  const provider = useOptionalProvider();

  const repos = useQuery({
    queryKey: ['repos'],
    queryFn: () => provider?.listRepos() ?? Promise.resolve([]),
    enabled: provider !== null,
  });

  // The capability snapshot follows the mounted vault: opening a folder with
  // the read-only fallback flips `write` to false (story GIT-US-0011).
  const providerCapabilities = provider?.capabilities;
  const reposUpdatedAt = repos.dataUpdatedAt;
  useEffect(() => {
    if (!providerCapabilities) return;
    setCapabilities(providerCapabilities);
  }, [providerCapabilities, reposUpdatedAt, setCapabilities]);

  const rows = repos.data ?? [];

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <a
        href="#main"
        className="sr-only rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:left-3 focus:top-3"
      >
        Skip to content
      </a>

      <aside className="hidden w-60 shrink-0 border-r border-border p-4 md:block">
        <div className="mb-6 flex flex-wrap items-center gap-2">
          <span className="font-semibold tracking-tight">git-in-track</span>
          <span className="rounded-full bg-secondary px-2 py-0.5 text-[11px] uppercase text-secondary-foreground">
            {mode}
          </span>
          {capabilities.write ? null : (
            <span
              className="flex items-center gap-1 rounded-full bg-destructive/10 px-2 py-0.5 text-[11px] uppercase text-destructive"
              title="This browser cannot save changes back to the folder"
            >
              <Lock aria-hidden="true" className="h-3 w-3" />
              Read-only
            </span>
          )}
        </div>

        <nav aria-label="Main">
          <ul className="space-y-1">
            {navItems.map((item) => (
              <li key={item.to}>
                <Link
                  to={item.to}
                  activeOptions={{ exact: item.to === '/' }}
                  className={cn(
                    'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors hover:bg-secondary',
                  )}
                  activeProps={{ className: 'bg-secondary font-medium' }}
                >
                  {item.icon}
                  {item.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>

        {rows.length > 0 ? (
          <nav aria-label="Repositories" className="mt-6 space-y-3">
            <h2 className="px-3 text-[11px] uppercase tracking-wide text-muted-foreground">
              Repositories
            </h2>
            <ul className="space-y-3">
              {rows.map((repo) => (
                <li key={repo.id} className="space-y-1">
                  <p className="px-3 text-sm font-medium">{repo.name}</p>
                  {repo.state === 'needs-permission' ? (
                    <p className="px-3 text-xs text-destructive">Needs permission</p>
                  ) : null}
                  <ul className="space-y-1">
                    {repo.projects.map((project) => (
                      <li key={project} className="space-y-1">
                        <Link
                          to="/p/$project/items"
                          params={{ project }}
                          className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm hover:bg-secondary"
                          activeProps={{ className: 'bg-secondary font-medium' }}
                        >
                          <ListChecks aria-hidden="true" className="h-3.5 w-3.5" />
                          {project} backlog
                        </Link>
                        <Link
                          to="/p/$project/kb/$"
                          params={{ project, _splat: '' }}
                          className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm hover:bg-secondary"
                          activeProps={{ className: 'bg-secondary font-medium' }}
                        >
                          <BookOpen aria-hidden="true" className="h-3.5 w-3.5" />
                          {project} docs
                        </Link>
                      </li>
                    ))}
                  </ul>
                </li>
              ))}
            </ul>
          </nav>
        ) : null}
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <ReadOnlyBanner />
        <main id="main" className="flex-1 p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

/**
 * One unobtrusive banner explaining the read-only fallback, dismissible for
 * the session (story GIT-US-0011).
 */
function ReadOnlyBanner() {
  const canWrite = useAppStore((state) => state.capabilities.write);
  const dismissed = useAppStore((state) => state.readOnlyNoticeDismissed);
  const dismiss = useAppStore((state) => state.dismissReadOnlyNotice);

  if (canWrite || dismissed) return null;

  return (
    <div
      role="status"
      className="flex items-start gap-3 border-b border-border bg-secondary px-6 py-3 text-sm"
    >
      <Lock aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
      <p className="flex-1">
        <strong className="font-medium">Read-only session.</strong> This browser has no File System
        Access API, so folders are loaded into memory and changes cannot be saved back. Browsing,
        filtering, search and the knowledge base all work. Use a Chromium browser (Chrome, Edge,
        Brave, Opera), or{' '}
        <a
          className="underline underline-offset-4"
          href={COMPANION_DOWNLOAD_URL}
          target="_blank"
          rel="noreferrer"
        >
          install the gintrack companion
        </a>{' '}
        to edit from any browser.
      </p>
      <Button variant="ghost" size="icon" aria-label="Dismiss read-only notice" onClick={dismiss}>
        <X aria-hidden="true" className="h-4 w-4" />
      </Button>
    </div>
  );
}
