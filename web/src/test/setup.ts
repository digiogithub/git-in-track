import '@testing-library/jest-dom/vitest';

import { cleanup, configure } from '@testing-library/react';
import { afterEach } from 'vitest';

// Parallel suites can starve a single test; 1 s is too tight for findBy* under load.
configure({ asyncUtilTimeout: 5000 });

afterEach(() => {
  cleanup();
});
