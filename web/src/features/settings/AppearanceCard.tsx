/**
 * Settings — Appearance (GIT-US-0206): the mode (light, dark or system, the
 * same preference as the sidebar toggle) and the colour palette, two
 * independent choices that combine. Both are per-browser `localStorage`
 * preferences applied before first paint by `public/theme-boot.js`.
 *
 * The choices are native radio inputs so arrow keys, focus order and the
 * accessible "1 of 3" announcement come from the browser. The swatches are the
 * `--swatch-*` tokens of `index.css`, so a preview shows each palette in the
 * current mode whichever palette is active.
 */

import type { ReactNode } from 'react';

import { usePalettePreference, type PalettePreference } from '@/app/palette';
import { useThemePreference, type ThemePreference } from '@/app/theme';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { cn } from '@/lib/cn';

const MODES: Array<{ value: ThemePreference; label: string }> = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'system', label: 'System' },
];

const PALETTE_OPTIONS: Array<{ value: PalettePreference; label: string; hint: string }> = [
  { value: 'default', label: 'Default', hint: 'Warm paper with a copper accent.' },
  {
    value: 'high-contrast',
    label: 'High contrast',
    hint: 'Near-black on white, AAA text and strong borders.',
  },
  { value: 'cool', label: 'Cool', hint: 'Crisp blue-grey surfaces with a blue accent.' },
  {
    value: 'colour-blind',
    label: 'Colour-blind safe',
    hint: 'Status and chart colours that never rely on red against green.',
  },
];

const SWATCH_ROLES = ['accent', 'progress', 'done'] as const;

const optionClass =
  'relative flex cursor-pointer gap-3 rounded-md border border-border bg-surface p-3 transition-colors duration-fast hover:border-border-strong has-[:checked]:border-accent has-[:checked]:bg-accent-subtle/40 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring';

export function AppearanceCard() {
  const [mode, setMode] = useThemePreference();
  const [palette, setPalette] = usePalettePreference();

  return (
    <Card>
      <CardHeader>
        <CardTitle>Appearance</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5 text-sm">
        <Fieldset legend="Mode" hint="System follows the operating system's light or dark setting.">
          <div className="grid gap-2 sm:grid-cols-3">
            {MODES.map((option) => (
              <label key={option.value} className={cn(optionClass, 'items-center')}>
                <input
                  type="radio"
                  name="appearance-mode"
                  value={option.value}
                  checked={mode === option.value}
                  onChange={() => {
                    setMode(option.value);
                  }}
                  className="accent-accent"
                />
                <span className="font-medium">{option.label}</span>
              </label>
            ))}
          </div>
        </Fieldset>

        <Fieldset
          legend="Colour palette"
          hint="Every palette has a light and a dark variant; the mode above picks which."
        >
          <div className="grid gap-2 sm:grid-cols-2">
            {PALETTE_OPTIONS.map((option) => (
              <label key={option.value} className={optionClass}>
                <input
                  type="radio"
                  name="appearance-palette"
                  value={option.value}
                  checked={palette === option.value}
                  onChange={() => {
                    setPalette(option.value);
                  }}
                  aria-describedby={`palette-hint-${option.value}`}
                  className="mt-0.5 self-start accent-accent"
                />
                <span className="min-w-0 flex-1 space-y-1">
                  <span className="block font-medium">{option.label}</span>
                  <span
                    id={`palette-hint-${option.value}`}
                    className="block text-xs text-muted-foreground"
                  >
                    {option.hint}
                  </span>
                </span>
                <Swatch palette={option.value} />
              </label>
            ))}
          </div>
        </Fieldset>
      </CardContent>
    </Card>
  );
}

function Fieldset({
  legend,
  hint,
  children,
}: {
  legend: string;
  hint: string;
  children: ReactNode;
}) {
  return (
    <fieldset className="space-y-2">
      <legend className="section-label">{legend}</legend>
      {children}
      <p className="text-xs text-muted-foreground">{hint}</p>
    </fieldset>
  );
}

/** A thumbnail of the palette: its page and text, then accent, in progress and done. */
function Swatch({ palette }: { palette: PalettePreference }) {
  const token = (role: string) => `hsl(var(--swatch-${palette}-${role}))`;
  return (
    <span
      aria-hidden="true"
      data-swatch={palette}
      className="flex h-10 shrink-0 items-center gap-1 self-center rounded-sm border border-border-strong px-2"
      style={{ background: token('background') }}
    >
      <span className="text-sm font-semibold" style={{ color: token('foreground') }}>
        Aa
      </span>
      {SWATCH_ROLES.map((role) => (
        <span key={role} className="h-3 w-3 rounded-full" style={{ background: token(role) }} />
      ))}
    </span>
  );
}
