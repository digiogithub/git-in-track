/**
 * Entry point of the styleguide (`npm run styleguide`).
 *
 * Dev-only by construction: Vite's production build starts from `index.html`,
 * so nothing in `src/dev/` is ever bundled into the app.
 */

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { Styleguide } from '@/dev/Styleguide';

import '../index.css';

const container = document.getElementById('root');
if (!container) throw new Error('#root is missing from styleguide.html');

createRoot(container).render(
  <StrictMode>
    <Styleguide />
  </StrictMode>,
);
