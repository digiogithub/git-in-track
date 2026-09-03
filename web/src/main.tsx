import { RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { detectMode } from '@/api/detect';
import { AppProviders } from '@/app/providers';
import { router } from '@/app/router';

import './index.css';

const container = document.getElementById('root');
if (!container) {
  throw new Error('#root is missing from index.html');
}

// Detection runs beside the first render: the shell paints in `detecting` mode
// and switches as soon as the probe answers.
void detectMode();

createRoot(container).render(
  <StrictMode>
    <AppProviders>
      <RouterProvider router={router} />
    </AppProviders>
  </StrictMode>,
);
