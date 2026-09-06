/**
 * Live contrast readout for the styleguide.
 *
 * The numbers are computed from the *rendered* tokens, not from a table that
 * could go stale: switch the theme and every ratio on the page recomputes. The
 * same maths, in a form that can fail a build, lives in
 * `scripts/check-design-tokens.mjs`.
 */

export type Rgb = [number, number, number];

export function tokenRgb(token: string): Rgb | null {
  if (typeof document === 'undefined') return null;
  const raw = getComputedStyle(document.documentElement).getPropertyValue(token).trim();
  const match = /^(\d+(?:\.\d+)?)\s+(\d+(?:\.\d+)?)%\s+(\d+(?:\.\d+)?)%$/.exec(raw);
  if (!match) return null;
  return hslToRgb(Number(match[1]), Number(match[2]), Number(match[3]));
}

export function hslToRgb(h: number, s: number, l: number): Rgb {
  const S = s / 100;
  const L = l / 100;
  const k = (n: number) => (n + h / 30) % 12;
  const a = S * Math.min(L, 1 - L);
  const f = (n: number) => L - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  return [f(0) * 255, f(8) * 255, f(4) * 255];
}

function channel(value: number): number {
  const v = value / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
}

export function luminance([r, g, b]: Rgb): number {
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

export function contrastRatio(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

/** `4.83` and its WCAG grade for a token pair, or `null` if either is unset. */
export function tokenContrast(
  foreground: string,
  background: string,
): { ratio: number; grade: 'AAA' | 'AA' | 'AA large' | 'fail' } | null {
  const fg = tokenRgb(foreground);
  const bg = tokenRgb(background);
  if (!fg || !bg) return null;
  const ratio = contrastRatio(fg, bg);
  const grade = ratio >= 7 ? 'AAA' : ratio >= 4.5 ? 'AA' : ratio >= 3 ? 'AA large' : 'fail';
  return { ratio, grade };
}
