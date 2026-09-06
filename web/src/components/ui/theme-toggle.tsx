import { Monitor, Moon, Sun } from 'lucide-react';
import type { ReactNode } from 'react';

import { useThemePreference, type ThemePreference } from '@/app/theme';
import { cn } from '@/lib/cn';

const OPTIONS: Array<{ value: ThemePreference; label: string; icon: ReactNode }> = [
  { value: 'light', label: 'Light', icon: <Sun aria-hidden="true" className="h-3.5 w-3.5" /> },
  { value: 'dark', label: 'Dark', icon: <Moon aria-hidden="true" className="h-3.5 w-3.5" /> },
  {
    value: 'system',
    label: 'System',
    icon: <Monitor aria-hidden="true" className="h-3.5 w-3.5" />,
  },
];

/**
 * Three-way theme control, as a radio group rather than a cycling button: the
 * current choice — including "follow the system" — is readable without
 * clicking, which a single toggle can never show.
 */
export function ThemeToggle({ className }: { className?: string }) {
  const [preference, setPreference] = useThemePreference();

  return (
    <div
      role="radiogroup"
      aria-label="Colour theme"
      className={cn(
        'inline-flex items-center gap-0.5 rounded-md border border-border bg-surface-muted/60 p-0.5',
        className,
      )}
    >
      {OPTIONS.map((option) => {
        const selected = preference === option.value;
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={`${option.label} theme`}
            title={`${option.label} theme`}
            onClick={() => {
              setPreference(option.value);
            }}
            className={cn(
              'flex h-6 w-7 items-center justify-center rounded-sm transition-colors duration-fast focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
              selected
                ? 'bg-elevated text-accent shadow-xs'
                : 'text-subtle-foreground hover:text-foreground',
            )}
          >
            {option.icon}
          </button>
        );
      })}
    </div>
  );
}
