import { useQuery } from '@tanstack/react-query';
import { Link, Outlet } from '@tanstack/react-router';
import {
  BookOpen,
  Boxes,
  ChartLine,
  LayoutDashboard,
  ListChecks,
  Lock,
  NotebookPen,
  Plug,
  RefreshCw,
  Settings,
  X,
} from 'lucide-react';
import { useEffect, type ReactNode } from 'react';

import { useOptionalProvider } from '@/api/provider-context';
import { useAppStore, type AppMode } from '@/app/store';
import { Button } from '@/components/ui/button';
import { Logo } from '@/components/ui/logo';
import { ThemeToggle } from '@/components/ui/theme-toggle';
import { cn } from '@/lib/cn';

type NavItem = {
  to: string;
  label: string;
  icon: ReactNode;
};

const navItems: NavItem[] = [
  { to: '/', label: 'Workspace', icon: <LayoutDashboard aria-hidden="true" className="h-4 w-4" /> },
  { to: '/boards', label: 'Boards', icon: <Boxes aria-hidden="true" className="h-4 w-4" /> },
  {
    to: '/retros',
    label: 'Retros',
    icon: <NotebookPen aria-hidden="true" className="h-4 w-4" />,
  },
  {
    to: '/metrics',
    label: 'Metrics',
    icon: <ChartLine aria-hidden="true" className="h-4 w-4" />,
  },
  { to: '/sync', label: 'Sync', icon: <RefreshCw aria-hidden="true" className="h-4 w-4" /> },
  { to: '/settings', label: 'Settings', icon: <Settings aria-hidden="true" className="h-4 w-4" /> },
];

/**
 * Sidebar link skin. Navigation is quiet by default and copper when active:
 * one accent on the screen, spent on "where am I", which is the only thing a
 * person needs from a sidebar they have already read a hundred times.
 */
const navLinkClass =
  'flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted-foreground transition-colors duration-fast hover:bg-secondary hover:text-foreground';
const navLinkActiveClass = 'bg-accent-subtle font-medium text-foreground [&_svg]:text-accent';

/** Where the companion binary is published (docs/09-ci-cd-and-releases.md). */
const COMPANION_DOWNLOAD_URL = 'https://github.com/digiogithub/git-in-track/releases';

/** Application shell: skip link, sidebar navigation and the routed main region. */
export function AppShell() {
  const mode = useAppStore((state) => state.mode);
  const companionVersion = useAppStore((state) => state.companionVersion);
  const companionUrl = useAppStore((state) => state.companionUrl);
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
  // `provider.capabilities` may be a getter returning a fresh object, so the
  // effect keys on the mounted provider and the last repo refresh, not on the
  // object identity, and the store ignores value-equal snapshots.
  const reposUpdatedAt = repos.dataUpdatedAt;
  useEffect(() => {
    if (!provider) return;
    setCapabilities(provider.capabilities);
  }, [provider, reposUpdatedAt, setCapabilities]);

  const rows = repos.data ?? [];

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <a
        href="#main"
        className="sr-only rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:left-3 focus:top-3 focus:z-50"
      >
        Skip to content
      </a>

      <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-sidebar-border bg-sidebar md:flex">
        <div className="flex flex-col gap-2 px-4 pb-4 pt-5">
          <div className="flex items-center gap-2">
            <Logo className="h-5 w-5 text-foreground" />
            <span className="font-semibold tracking-tight">git-in-track</span>
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            <span
              data-testid="mode-badge"
              className="rounded-full bg-secondary px-2 py-0.5 text-2xs uppercase tracking-[0.08em] text-muted-foreground"
              title={modeTooltip(mode, companionVersion, companionUrl)}
            >
              {mode}
            </span>
            {capabilities.write ? null : (
              <span
                className="bg-destructive/12 flex items-center gap-1 rounded-full px-2 py-0.5 text-2xs uppercase tracking-[0.08em] text-destructive"
                title="This browser cannot save changes back to the folder"
              >
                <Lock aria-hidden="true" className="h-3 w-3" />
                Read-only
              </span>
            )}
          </div>
        </div>

        <div className="flex-1 overflow-y-auto px-3 pb-4">
          <nav aria-label="Main">
            <ul className="space-y-0.5">
              {navItems.map((item) => (
                <li key={item.to}>
                  <Link
                    to={item.to}
                    activeOptions={{ exact: item.to === '/' }}
                    className={navLinkClass}
                    activeProps={{ className: navLinkActiveClass }}
                  >
                    {item.icon}
                    {item.label}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>

          {rows.length > 0 ? (
            <nav aria-label="Repositories" className="mt-7 space-y-3">
              <h2 className="section-label px-2.5">Repositories</h2>
              <ul className="space-y-4">
                {rows.map((repo) => (
                  <li key={repo.id} className="space-y-1">
                    <p className="truncate px-2.5 text-sm font-medium" title={repo.name}>
                      {repo.name}
                    </p>
                    {repo.state === 'needs-permission' ? (
                      <p className="px-2.5 text-xs text-destructive">Needs permission</p>
                    ) : null}
                    <ul className="space-y-0.5 border-l border-sidebar-border pl-2">
                      {repo.projects.map((project) => (
                        <li key={project} className="space-y-0.5">
                          <Link
                            to="/p/$project/items"
                            params={{ project }}
                            className={cn(navLinkClass, 'py-1 text-[0.8125rem]')}
                            activeProps={{ className: navLinkActiveClass }}
                          >
                            <ListChecks aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
                            <span className="truncate">{project} backlog</span>
                          </Link>
                          <Link
                            to="/p/$project/kb/$"
                            params={{ project, _splat: '' }}
                            className={cn(navLinkClass, 'py-1 text-[0.8125rem]')}
                            activeProps={{ className: navLinkActiveClass }}
                          >
                            <BookOpen aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
                            <span className="truncate">{project} docs</span>
                          </Link>
                        </li>
                      ))}
                    </ul>
                  </li>
                ))}
              </ul>
            </nav>
          ) : null}
        </div>

        <div className="flex items-center justify-between gap-2 border-t border-sidebar-border px-4 py-3">
          <span className="section-label">Theme</span>
          <ThemeToggle />
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <ModeNoticeBanner />
        <TokenRequiredBanner />
        <ReadOnlyBanner />
        <main id="main" className="flex-1 px-6 py-6 lg:px-8">
          <div className="mx-auto w-full max-w-[100rem]">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}

/** Badge tooltip: which runtime, which companion version, and where it runs. */
function modeTooltip(
  mode: AppMode,
  companionVersion: string | null,
  companionUrl: string | null,
): string {
  if (mode === 'detecting') return 'Looking for the gintrack companion…';
  if (mode === 'browser') {
    return 'Browser-only mode: File System Access and the WebAssembly core.';
  }
  const version = companionVersion === null ? 'unknown version' : `version ${companionVersion}`;
  const where = companionUrl === null || companionUrl === '' ? 'this origin' : companionUrl;
  return `Companion ${version} at ${where} — native indexing and file watching enabled.`;
}

/**
 * The one banner skin. A banner is a tinted strip under the top of the content
 * column, never a floating card: it belongs to the page it is explaining.
 */
function Banner({
  tone,
  icon,
  children,
  action,
  role,
}: {
  tone: 'neutral' | 'danger';
  icon: ReactNode;
  children: ReactNode;
  action?: ReactNode;
  role: 'status' | 'alert';
}) {
  return (
    <div
      role={role}
      className={cn(
        'flex items-start gap-3 border-b px-6 py-2.5 text-sm lg:px-8',
        tone === 'danger'
          ? 'border-destructive/30 bg-destructive/10 text-destructive'
          : 'border-border bg-surface-muted/70 text-foreground',
      )}
    >
      <span className={cn('mt-0.5 shrink-0', tone === 'danger' ? '' : 'text-muted-foreground')}>
        {icon}
      </span>
      <p className="flex-1 leading-relaxed">{children}</p>
      {action}
    </div>
  );
}

/**
 * Non-blocking notice for a mode flip while the tab is open: the companion
 * appearing (upgrade) or going away (downgrade), per docs/05-web-app.md §4.3.
 */
function ModeNoticeBanner() {
  const notice = useAppStore((state) => state.modeNotice);
  const dismiss = useAppStore((state) => state.dismissModeNotice);

  if (notice === null) return null;

  const upgraded = notice === 'companion-detected';

  return (
    <Banner
      role="status"
      tone="neutral"
      icon={<Plug aria-hidden="true" className="h-4 w-4" />}
      action={
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Dismiss companion notice"
          onClick={dismiss}
        >
          <X aria-hidden="true" className="h-4 w-4" />
        </Button>
      }
    >
      {upgraded ? (
        <strong className="font-medium">
          Companion detected — native indexing and file watching enabled.
        </strong>
      ) : (
        <>
          <strong className="font-medium">Companion disconnected.</strong> Back to browser-only
          mode: the WebAssembly core keeps everything working from this tab.
        </>
      )}
    </Banner>
  );
}

/** A missing or rejected token is an actionable state, never a silent failure. */
function TokenRequiredBanner() {
  const mode = useAppStore((state) => state.mode);
  const auth = useAppStore((state) => state.companionAuth);

  if (mode !== 'companion' || auth !== 'required') return null;

  return (
    <Banner role="alert" tone="danger" icon={<Lock aria-hidden="true" className="h-4 w-4" />}>
      <strong className="font-medium">The companion needs an access token.</strong> Copy the token
      printed by{' '}
      <code className="rounded-sm bg-code px-1 py-0.5 font-mono text-xs">gintrack serve</code> and
      paste it in{' '}
      <Link to="/settings" className="font-medium underline underline-offset-4">
        Settings
      </Link>
      .
    </Banner>
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
    <Banner
      role="status"
      tone="neutral"
      icon={<Lock aria-hidden="true" className="h-4 w-4" />}
      action={
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Dismiss read-only notice"
          onClick={dismiss}
        >
          <X aria-hidden="true" className="h-4 w-4" />
        </Button>
      }
    >
      <strong className="font-medium">Read-only session.</strong> This browser has no File System
      Access API, so folders are loaded into memory and changes cannot be saved back. Browsing,
      filtering, search and the knowledge base all work. Use a Chromium browser (Chrome, Edge,
      Brave, Opera), or{' '}
      <a
        className="font-medium underline underline-offset-4"
        href={COMPANION_DOWNLOAD_URL}
        target="_blank"
        rel="noreferrer"
      >
        install the gintrack companion
      </a>{' '}
      to edit from any browser.
    </Banner>
  );
}
