import { RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { AppProviders } from '@/app/providers';
import { router } from '@/app/router';
import { applyThemePreference, readThemePreference } from '@/app/theme';

import './index.css';

// `public/theme-boot.js` already did this before first paint; doing it again
// here costs nothing and keeps the page honest when that file is missing —
// a stale cache, a host that never copied it, an extension that blocked it.
applyThemePreference(readThemePreference());

const container = document.getElementById('root');
if (!container) {
  throw new Error('#root is missing from index.html');
}

// Mode detection and provider construction happen inside `AppProviders`
// (docs/05-web-app.md §4.3), so the tree always renders against one provider.
createRoot(container).render(
  <StrictMode>
    <AppProviders>
      <RouterProvider router={router} />
    </AppProviders>
  </StrictMode>,
);
