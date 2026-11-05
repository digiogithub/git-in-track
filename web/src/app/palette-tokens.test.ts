/// <reference types="node" />

/**
 * The contrast contract of every colour palette (GIT-US-0206), measured from
 * the token values `src/index.css` actually ships, per palette and per mode.
 *
 * `npm run tokens:check` already guards the default palette in depth; this
 * suite holds the extra palettes to the same rules and to their own promises:
 * high contrast reaches AAA (7:1) for body and muted text and 3:1 for every UI
 * boundary, and colour-blind safe keeps statuses, priorities and chart series
 * apart under protanopia and deuteranopia. It also checks the structure the
 * cascade depends on: a palette's dark blocks (media query and attribute) are
 * identical, and override every token its light block overrides — otherwise a
 * light value would leak into dark mode.
 */

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, it } from 'vitest';

import { PALETTES } from '@/app/palette';

type Hsl = [number, number, number];
type Rgb = [number, number, number];
type Tokens = Map<string, Hsl>;

const css = readFileSync(resolve(process.cwd(), 'src/index.css'), 'utf8');

/* ---------------------------------------------------------------- parsing */

function tokensOf(block: string): Tokens {
  const out: Tokens = new Map();
  for (const match of block.matchAll(
    /(--[\w-]+):\s*(\d+(?:\.\d+)?)\s+(\d+(?:\.\d+)?)%\s+(\d+(?:\.\d+)?)%\s*;/g,
  )) {
    out.set(match[1] ?? '', [Number(match[2]), Number(match[3]), Number(match[4])]);
  }
  return out;
}

/** The body of the balanced `{ … }` that opens at `from`. */
function bodyAt(from: number): string {
  const start = css.indexOf('{', from);
  let depth = 0;
  for (let i = start; i < css.length; i += 1) {
    if (css[i] === '{') depth += 1;
    else if (css[i] === '}') {
      depth -= 1;
      if (depth === 0) return css.slice(start + 1, i);
    }
  }
  throw new Error('unbalanced braces in index.css');
}

/** The tokens of the first block written for `selector`. */
function block(selector: string): Tokens {
  const at = css.indexOf(`${selector} {`);
  if (at < 0) throw new Error(`index.css has no block for ${selector}`);
  return tokensOf(bodyAt(at));
}

/** The tokens of every block written for `selector`, later ones winning. */
function allBlocks(selector: string): Tokens {
  const out: Tokens = new Map();
  for (let at = css.indexOf(`${selector} {`); at >= 0; at = css.indexOf(`${selector} {`, at + 1)) {
    for (const [token, value] of tokensOf(bodyAt(at))) out.set(token, value);
  }
  return out;
}

const baseLight = block(':root');
const baseDark = block(":root[data-theme='dark']");

/** The light and dark blocks of one palette, as written. */
function paletteBlocks(palette: string) {
  const sel = `:root[data-palette='${palette}']`;
  return {
    light: block(sel),
    darkMedia: block(`${sel}:not([data-theme='light'])`),
    darkAttr: block(`${sel}[data-theme='dark']`),
  };
}

/** What a page in this palette and mode resolves every token to. */
function resolved(palette: string, mode: 'light' | 'dark'): Tokens {
  if (palette === 'default') {
    return mode === 'light' ? baseLight : new Map([...baseLight, ...baseDark]);
  }
  const own = paletteBlocks(palette);
  return mode === 'light'
    ? new Map([...baseLight, ...own.light])
    : new Map([...baseLight, ...baseDark, ...own.darkAttr]);
}

/* ------------------------------------------------------------ colour math */

function hslToRgb([h, s, l]: Hsl): Rgb {
  const S = s / 100;
  const L = l / 100;
  const k = (n: number) => (n + h / 30) % 12;
  const a = S * Math.min(L, 1 - L);
  const f = (n: number) => L - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  return [f(0) * 255, f(8) * 255, f(4) * 255];
}

const linear = (c: number) => {
  const v = c / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
};

const luminance = ([r, g, b]: Rgb) => 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b);

function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

/** `fg` at `alpha` over `bg`: what a tinted chip's background really is. */
function over(fg: Rgb, alpha: number, bg: Rgb): Rgb {
  return [0, 1, 2].map((i) => alpha * (fg[i] ?? 0) + (1 - alpha) * (bg[i] ?? 0)) as Rgb;
}

/* Viénot/Brettel-style dichromacy matrices, the same ones tokens:check uses. */
const CVD: Record<'protanopia' | 'deuteranopia', number[][]> = {
  protanopia: [
    [0.152286, 1.052583, -0.204868],
    [0.114503, 0.786281, 0.099216],
    [-0.003882, -0.048116, 1.051998],
  ],
  deuteranopia: [
    [0.367322, 0.860646, -0.227968],
    [0.280085, 0.672501, 0.047413],
    [-0.01182, 0.04294, 0.968881],
  ],
};

function simulate(rgb: Rgb, kind: keyof typeof CVD): Rgb {
  const lin = rgb.map(linear);
  return CVD[kind].map((row) => {
    const c = row.reduce((sum, k, i) => sum + k * (lin[i] ?? 0), 0);
    const v = Math.min(1, Math.max(0, c));
    return 255 * (v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055);
  }) as Rgb;
}

function lab(rgb: Rgb): Rgb {
  const [r, g, b] = rgb.map(linear) as Rgb;
  const x = (0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047;
  const y = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  const z = (0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883;
  const f = (t: number) => (t > 0.008856 ? Math.cbrt(t) : 7.787 * t + 16 / 116);
  return [116 * f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))];
}

const deltaE = (a: Rgb, b: Rgb) => {
  const [la, lb] = [lab(a), lab(b)];
  return Math.hypot(la[0] - lb[0], la[1] - lb[1], la[2] - lb[2]);
};

/** The smallest ΔE between two colours under normal vision, protanopia and deuteranopia. */
function worstDeltaE(a: Rgb, b: Rgb): number {
  return Math.min(
    deltaE(a, b),
    deltaE(simulate(a, 'protanopia'), simulate(b, 'protanopia')),
    deltaE(simulate(a, 'deuteranopia'), simulate(b, 'deuteranopia')),
  );
}

/* --------------------------------------------------------------- the rules */

const SURFACES = ['--background', '--surface', '--elevated', '--sidebar'];

/** [text token, surfaces, AA target, high-contrast target]. */
const TEXT_ON: Array<[string, string[], number, number]> = [
  ['--foreground', SURFACES, 4.5, 7],
  ['--muted-foreground', SURFACES, 4.5, 7],
  ['--subtle-foreground', ['--background', '--surface'], 4.5, 7],
  ['--accent', ['--background', '--surface'], 4.5, 7],
  ['--destructive', ['--background', '--surface'], 4.5, 4.5],
  ['--success', ['--background', '--surface'], 4.5, 4.5],
  ['--warning', ['--background', '--surface'], 4.5, 4.5],
  ['--info', ['--background', '--surface'], 4.5, 4.5],
  ['--primary-foreground', ['--primary'], 4.5, 7],
  ['--accent-foreground', ['--accent'], 4.5, 7],
  ['--destructive-foreground', ['--destructive'], 4.5, 4.5],
  ['--secondary-foreground', ['--secondary'], 4.5, 7],
  /* A control's boundary is a graphical object: 3:1 (WCAG 1.4.11). */
  ['--input', ['--background', '--surface'], 3, 3],
];

/** Boundaries high contrast promises at 3:1 on top of the input edge. */
const HC_BOUNDARIES = ['--border', '--border-strong', '--sidebar-border', '--ring'];

/** Chip tokens and the alpha of their tint (`item-meta.ts`). */
const CHIPS: Array<[string, number]> = [
  ['--status-backlog', 0.15],
  ['--status-todo', 0.15],
  ['--status-in-progress', 0.18],
  ['--status-in-review', 0.15],
  ['--status-done', 0.15],
  ['--status-cancelled', 0.15],
  ['--priority-critical', 0.15],
  ['--priority-high', 0.15],
  ['--priority-medium', 0.15],
  ['--priority-low', 0.15],
];

const SERIES = ['--chart-todo', '--chart-progress', '--chart-done', '--chart-cancelled'];

/** Hues that must stay apart under protanopia and deuteranopia in colour-blind safe. */
const CVD_GROUPS = [
  ['--status-todo', '--status-in-progress', '--status-in-review', '--status-done'],
  ['--priority-critical', '--priority-high', '--priority-medium'],
  ['--success', '--destructive', '--warning', '--info'],
];
const CVD_MIN_DELTA_E = 12;

/** The pairs a red/green confusion would hurt most: held much further apart. */
const CVD_CRITICAL: Array<[string, string]> = [
  ['--success', '--destructive'],
  ['--status-done', '--status-in-progress'],
  ['--status-done', '--priority-critical'],
];
const CVD_CRITICAL_DELTA_E = 40;

const MODES = ['light', 'dark'] as const;

function measure(palette: string, mode: (typeof MODES)[number]) {
  const tokens = resolved(palette, mode);
  const rgb = (token: string) => {
    const value = tokens.get(token);
    if (!value) throw new Error(`${palette}/${mode}: token ${token} is missing`);
    return hslToRgb(value);
  };
  const failures: string[] = [];
  const hc = palette === 'high-contrast';

  for (const [token, surfaces, aa, aaa] of TEXT_ON) {
    const target = hc ? aaa : aa;
    for (const surface of surfaces) {
      const ratio = contrast(rgb(token), rgb(surface));
      if (ratio < target) failures.push(`${token} on ${surface}: ${ratio.toFixed(2)} < ${target}`);
    }
  }

  if (hc) {
    for (const token of HC_BOUNDARIES) {
      for (const surface of ['--background', '--surface']) {
        const ratio = contrast(rgb(token), rgb(surface));
        if (ratio < 3) failures.push(`${token} on ${surface}: ${ratio.toFixed(2)} < 3`);
      }
    }
  }

  // The default chips predate this suite and are guarded by tokens:check; the
  // extra palettes promise every chip 4.5:1, bar `cancelled`, which is context
  // rather than content and only has to read as a mark (3:1).
  if (palette !== 'default') {
    for (const [token, alpha] of CHIPS) {
      const target = token === '--status-cancelled' ? 3 : 4.5;
      const ratio = contrast(rgb(token), over(rgb(token), alpha, rgb('--surface')));
      if (ratio < target) failures.push(`${token} chip: ${ratio.toFixed(2)} < ${target}`);
    }
  }

  for (const token of SERIES) {
    const ratio = contrast(rgb(token), rgb('--surface'));
    if (ratio < 3) failures.push(`${token} on --surface: ${ratio.toFixed(2)} < 3`);
  }
  for (let i = 0; i < SERIES.length; i += 1) {
    for (let j = i + 1; j < SERIES.length; j += 1) {
      const a = SERIES[i] ?? '';
      const b = SERIES[j] ?? '';
      const worst = worstDeltaE(rgb(a), rgb(b));
      if (worst < 20) failures.push(`series ${a} vs ${b}: ΔE ${worst.toFixed(1)} < 20`);
    }
  }

  if (palette === 'colour-blind') {
    for (const group of CVD_GROUPS) {
      for (let i = 0; i < group.length; i += 1) {
        for (let j = i + 1; j < group.length; j += 1) {
          const a = group[i] ?? '';
          const b = group[j] ?? '';
          const worst = worstDeltaE(rgb(a), rgb(b));
          if (worst < CVD_MIN_DELTA_E) {
            failures.push(`${a} vs ${b}: ΔE ${worst.toFixed(1)} < ${CVD_MIN_DELTA_E} under CVD`);
          }
        }
      }
    }
    for (const [a, b] of CVD_CRITICAL) {
      const worst = worstDeltaE(rgb(a), rgb(b));
      if (worst < CVD_CRITICAL_DELTA_E) {
        failures.push(`${a} vs ${b}: ΔE ${worst.toFixed(1)} < ${CVD_CRITICAL_DELTA_E} under CVD`);
      }
    }
  }

  return {
    failures,
    body: contrast(rgb('--foreground'), rgb('--background')),
    muted: contrast(rgb('--muted-foreground'), rgb('--background')),
  };
}

describe('colour palettes', () => {
  it('declares a block per extra palette and mode in index.css', () => {
    for (const palette of PALETTES) {
      if (palette === 'default') continue;
      const own = paletteBlocks(palette);
      expect(own.light.size, palette).toBeGreaterThan(0);
    }
  });

  for (const palette of PALETTES) {
    if (palette === 'default') continue;

    it(`${palette}: the two dark blocks are identical`, () => {
      const own = paletteBlocks(palette);
      const media = [...own.darkMedia].map(([k, v]) => `${k}: ${v.join(' ')}`).sort();
      const attr = [...own.darkAttr].map(([k, v]) => `${k}: ${v.join(' ')}`).sort();
      expect(media).toEqual(attr);
    });

    it(`${palette}: dark overrides every token light overrides`, () => {
      const own = paletteBlocks(palette);
      const leaks = [...own.light.keys()].filter((token) => !own.darkAttr.has(token));
      expect(leaks).toEqual([]);
    });
  }

  it('previews every palette with swatches that match its tokens', () => {
    const swatches = {
      light: allBlocks(':root'),
      dark: allBlocks(":root[data-theme='dark']"),
    };
    const media = allBlocks(":root:not([data-theme='light'])");
    const roles: Array<[string, string]> = [
      ['background', '--background'],
      ['foreground', '--foreground'],
      ['accent', '--accent'],
      ['progress', '--status-in-progress'],
      ['done', '--status-done'],
    ];
    const drift: string[] = [];
    for (const palette of PALETTES) {
      for (const mode of MODES) {
        const tokens = resolved(palette, mode);
        for (const [role, token] of roles) {
          const name = `--swatch-${palette}-${role}`;
          const shown = swatches[mode].get(name)?.join(' ');
          if (shown !== tokens.get(token)?.join(' ')) drift.push(`${mode} ${name}`);
          if (mode === 'dark' && media.get(name)?.join(' ') !== shown) {
            drift.push(`media ${name}`);
          }
        }
      }
    }
    expect(drift).toEqual([]);
  });

  for (const palette of PALETTES) {
    for (const mode of MODES) {
      it(`${palette} / ${mode}: meets its contrast contract`, () => {
        const { failures } = measure(palette, mode);
        expect(failures).toEqual([]);
      });
    }
  }

  it('high contrast reaches AAA for body and muted text in both modes', () => {
    for (const mode of MODES) {
      const { body, muted } = measure('high-contrast', mode);
      expect(body).toBeGreaterThanOrEqual(7);
      expect(muted).toBeGreaterThanOrEqual(7);
    }
  });
});
