import { Link, Outlet } from '@tanstack/react-router';
import { Boxes, LayoutDashboard, Settings } from 'lucide-react';
import type { ReactNode } from 'react';

import { useAppStore } from '@/app/store';
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

/** Application shell: skip link, sidebar navigation and the routed main region. */
export function AppShell() {
  const mode = useAppStore((state) => state.mode);

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <a
        href="#main"
        className="sr-only rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:left-3 focus:top-3"
      >
        Skip to content
      </a>

      <aside className="hidden w-60 shrink-0 border-r border-border p-4 md:block">
        <div className="mb-6 flex items-center gap-2">
          <span className="font-semibold tracking-tight">git-in-track</span>
          <span className="rounded-full bg-secondary px-2 py-0.5 text-[11px] uppercase text-secondary-foreground">
            {mode}
          </span>
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
      </aside>

      <main id="main" className="flex-1 p-6">
        <Outlet />
      </main>
    </div>
  );
}
