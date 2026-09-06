/**
 * Mermaid's palette, in the app's own colours.
 *
 * Mermaid renders to SVG with the colours baked in, so it cannot read a CSS
 * variable the way everything else does. This file is therefore the one place
 * in the app where token values are written out as literals; keep it in step
 * with `src/index.css` (docs/13-design-system.md lists the pairs).
 *
 * `theme: 'base'` plus these variables, rather than mermaid's own `default` and
 * `dark`: both of those ship a saturated blue-and-lilac diagram that would be
 * the only cold thing in the document.
 */

import type { ThemeMode } from '@/markdown/theme';

type MermaidThemeVariables = Record<string, string>;

const SHARED: MermaidThemeVariables = {
  fontFamily: "ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
  fontSize: '14px',
};

const LIGHT: MermaidThemeVariables = {
  ...SHARED,
  background: '#F7F4EF',
  primaryColor: '#EFEAE1',
  primaryTextColor: '#2B2723',
  primaryBorderColor: '#C6BBA6',
  secondaryColor: '#E7DFD2',
  tertiaryColor: '#FCFAF6',
  mainBkg: '#EFEAE1',
  nodeBorder: '#C6BBA6',
  lineColor: '#8A8072',
  textColor: '#2B2723',
  clusterBkg: '#F3EFE7',
  clusterBorder: '#E3DDD1',
  edgeLabelBackground: '#F7F4EF',
  labelBoxBkgColor: '#FCFAF6',
  labelBoxBorderColor: '#C6BBA6',
  /* The accent is reserved for "this one matters" marks (active task, critical
     path); every ordinary node stays neutral. */
  altBackground: '#F3EFE7',
  activeTaskBkgColor: '#9C5F2E',
  activeTaskBorderColor: '#84501F',
  doneTaskBkgColor: '#2E6449',
  doneTaskBorderColor: '#2E6449',
  critBorderColor: '#A8443E',
  critBkgColor: '#A8443E',
};

const DARK: MermaidThemeVariables = {
  ...SHARED,
  background: '#191714',
  primaryColor: '#2E2A25',
  primaryTextColor: '#EDE7DC',
  primaryBorderColor: '#4A443B',
  secondaryColor: '#37322B',
  tertiaryColor: '#211E1A',
  mainBkg: '#2E2A25',
  nodeBorder: '#4A443B',
  lineColor: '#877E71',
  textColor: '#EDE7DC',
  clusterBkg: '#211E1A',
  clusterBorder: '#37322B',
  edgeLabelBackground: '#191714',
  labelBoxBkgColor: '#282420',
  labelBoxBorderColor: '#4A443B',
  altBackground: '#211E1A',
  activeTaskBkgColor: '#E39C63',
  activeTaskBorderColor: '#E39C63',
  doneTaskBkgColor: '#86C2A0',
  doneTaskBorderColor: '#86C2A0',
  critBorderColor: '#E88980',
  critBkgColor: '#E88980',
};

export function mermaidThemeVariables(mode: ThemeMode): MermaidThemeVariables {
  return mode === 'dark' ? DARK : LIGHT;
}
