#!/usr/bin/env node
/**
 * Design-token guard (docs/13-design-system.md).
 *
 * A palette is a set of *relationships*, and every one of them is checkable, so
 * none of them is left to memory:
 *
 *  1. the two dark blocks in `src/index.css` (media query and `data-theme`) are
 *     identical — they are the same theme written twice, and drift is silent;
 *  2. every text token clears WCAG AA on the surfaces it is used on;
 *  3. every chart series stays separable from its neighbours under normal
 *     vision and under protanopia, deuteranopia and tritanopia, and clears 3:1
 *     against the chart surface.
 *
 * Run with `npm run tokens:check`. Change a colour, run this, and it tells you
 * what the change cost — which is the only way a palette survives edits.
 */

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(join(here, '..', 'src', 'index.css'), 'utf8');

/* ---------------------------------------------------------------- parsing */

/** Every `--token: H S% L%;` declaration inside one block. */
function tokensOf(block) {
  const out = new Map();
  for (const match of block.matchAll(
    /(--[\w-]+):\s*(\d+(?:\.\d+)?)\s+(\d+(?:\.\d+)?)%\s+(\d+(?:\.\d+)?)%\s*;/g,
  )) {
    out.set(match[1], [Number(match[2]), Number(match[3]), Number(match[4])]);
  }
  return out;
}

/** The body of the first balanced `{ … }` that follows `index`. */
function blockAt(source, index) {
  const start = source.indexOf('{', index);
  let depth = 0;
  for (let i = start; i < source.length; i += 1) {
    if (source[i] === '{') depth += 1;
    else if (source[i] === '}') {
      depth -= 1;
      if (depth === 0) return source.slice(start + 1, i);
    }
  }
  throw new Error('unbalanced braces in index.css');
}

const light = tokensOf(blockAt(css, css.indexOf(':root {')));
const darkMedia = tokensOf(blockAt(css, css.indexOf(":root:not([data-theme='light'])")));
const darkAttr = tokensOf(blockAt(css, css.indexOf(":root[data-theme='dark']")));

/* ------------------------------------------------------------ colour math */

const hslToRgb = ([h, s, l]) => {
  const S = s / 100;
  const L = l / 100;
  const k = (n) => (n + h / 30) % 12;
  const a = S * Math.min(L, 1 - L);
  const f = (n) => L - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  return [f(0) * 255, f(8) * 255, f(4) * 255];
};

const linear = (c) => {
  const v = c / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
};

const luminance = (rgb) => {
  const [r, g, b] = rgb.map(linear);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};

const contrast = (a, b) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

const CVD = {
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
  tritanopia: [
    [1.255528, -0.076749, -0.178779],
    [-0.078411, 0.930809, 0.147602],
    [0.004733, 0.691367, 0.3039],
  ],
};

const simulate = (rgb, kind) => {
  const lin = rgb.map(linear);
  const m = CVD[kind];
  const out = m.map((row) => row.reduce((sum, k, i) => sum + k * lin[i], 0));
  return out.map((c) => {
    const v = Math.min(1, Math.max(0, c));
    return 255 * (v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055);
  });
};

const lab = (rgb) => {
  const [r, g, b] = rgb.map(linear);
  const x = (0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047;
  const y = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  const z = (0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883;
  const f = (t) => (t > 0.008856 ? Math.cbrt(t) : 7.787 * t + 16 / 116);
  return [116 * f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))];
};

const deltaE = (a, b) => {
  const [la, lb] = [lab(a), lab(b)];
  return Math.hypot(la[0] - lb[0], la[1] - lb[1], la[2] - lb[2]);
};

/* --------------------------------------------------------------- the rules */

/** Text tokens and the surfaces they are allowed to sit on. AA is 4.5:1. */
const TEXT_ON = [
  ['--foreground', ['--background', '--surface', '--elevated', '--sidebar'], 4.5],
  ['--muted-foreground', ['--background', '--surface', '--elevated', '--sidebar'], 4.5],
  ['--subtle-foreground', ['--background', '--surface'], 4.5],
  ['--accent', ['--background', '--surface'], 4.5],
  ['--destructive', ['--background', '--surface'], 4.5],
  ['--success', ['--background', '--surface'], 4.5],
  ['--warning', ['--background', '--surface'], 4.5],
  ['--info', ['--background', '--surface'], 4.5],
  ['--primary-foreground', ['--primary'], 4.5],
  ['--accent-foreground', ['--accent'], 4.5],
  ['--destructive-foreground', ['--destructive'], 4.5],
  /* A control's boundary is a graphical object: 3:1 (WCAG 1.4.11). */
  ['--input', ['--background', '--surface'], 3],
];

const SERIES = ['--chart-todo', '--chart-progress', '--chart-done', '--chart-cancelled'];
const SERIES_MIN_DELTA_E = 20;
const SERIES_MIN_CONTRAST = 3;

const failures = [];
const report = [];

function checkTheme(name, tokens) {
  const rgb = (token) => {
    const value = tokens.get(token);
    if (!value) throw new Error(`${name}: token ${token} is missing`);
    return hslToRgb(value);
  };

  for (const [token, surfaces, target] of TEXT_ON) {
    for (const surface of surfaces) {
      const ratio = contrast(rgb(token), rgb(surface));
      const ok = ratio >= target;
      report.push(
        `${ok ? '  ok ' : '  XX '}${name.padEnd(5)} ${token} on ${surface}: ${ratio.toFixed(2)} (needs ${target})`,
      );
      if (!ok)
        failures.push(`${name}: ${token} on ${surface} is ${ratio.toFixed(2)}, needs ${target}`);
    }
  }

  for (const token of SERIES) {
    const ratio = contrast(rgb(token), rgb('--surface'));
    if (ratio < SERIES_MIN_CONTRAST) {
      failures.push(`${name}: ${token} is ${ratio.toFixed(2)} on the card surface, needs 3`);
    }
  }

  for (let i = 0; i < SERIES.length; i += 1) {
    for (let j = i + 1; j < SERIES.length; j += 1) {
      const [a, b] = [rgb(SERIES[i]), rgb(SERIES[j])];
      const worst = Math.min(
        deltaE(a, b),
        ...Object.keys(CVD).map((kind) => deltaE(simulate(a, kind), simulate(b, kind))),
      );
      const ok = worst >= SERIES_MIN_DELTA_E;
      report.push(
        `${ok ? '  ok ' : '  XX '}${name.padEnd(5)} series ${SERIES[i]} vs ${SERIES[j]}: ΔE ${worst.toFixed(1)} (needs ${SERIES_MIN_DELTA_E})`,
      );
      if (!ok) {
        failures.push(
          `${name}: chart series ${SERIES[i]} and ${SERIES[j]} collapse to ΔE ${worst.toFixed(1)} for someone with colour-vision deficiency`,
        );
      }
    }
  }
}

/* 1. the two dark blocks are the same theme */
for (const [token, value] of darkMedia) {
  const other = darkAttr.get(token);
  if (!other || other.join(' ') !== value.join(' ')) {
    failures.push(
      `dark: ${token} is "${value.join(' ')}" under prefers-color-scheme but "${other?.join(' ') ?? 'missing'}" under [data-theme=dark]`,
    );
  }
}
for (const token of darkAttr.keys()) {
  if (!darkMedia.has(token)) failures.push(`dark: ${token} is missing from the media-query block`);
}

checkTheme('light', light);
checkTheme('dark', new Map([...light, ...darkAttr]));

console.log(report.join('\n'));
if (failures.length > 0) {
  console.error(`\n${failures.length} design-token check(s) failed:\n`);
  for (const failure of failures) console.error(`  - ${failure}`);
  process.exit(1);
}
console.log(`\nAll design-token checks passed (${report.length} assertions).`);
