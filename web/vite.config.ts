import { fileURLToPath, URL } from 'node:url';

import react from '@vitejs/plugin-react';
import { searchForWorkspaceRoot } from 'vite';
import { defineConfig } from 'vitest/config';

/**
 * The spec body templates ship once, in the Go core (internal/core/templates,
 * GIT-US-0111); the editor imports the very same files with `?raw`.
 */
const coreTemplates = fileURLToPath(new URL('../internal/core/templates', import.meta.url));

/**
 * Vite configuration for the git-in-track web app.
 *
 * - `base: '/'` so the bundle works both from the embedded Go server and from a
 *   static host.
 * - `build.outDir: 'dist'` because `internal/server` embeds `web/dist`.
 * - `server.proxy` forwards `/api` to the companion (`gintrack serve`) during
 *   development, so the dev server behaves like the embedded build.
 * - `worker.format: 'es'` so the WASM core worker can use ES module imports.
 * - `@core-templates` points at the core's body templates, outside `web/`, so
 *   the dev server is allowed to read that one folder as well.
 */
export default defineConfig({
  base: '/',
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@core-templates': coreTemplates,
    },
  },
  server: {
    port: 5173,
    fs: {
      allow: [searchForWorkspaceRoot(process.cwd()), coreTemplates],
    },
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:7317',
        changeOrigin: true,
        // The companion exposes `/api/v1/events` as a WebSocket upgrade.
        ws: true,
      },
    },
  },
  worker: {
    format: 'es',
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
    sourcemap: true,
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    restoreMocks: true,
    // Testing Library waits up to `asyncUtilTimeout` (5 s, see src/test/setup.ts)
    // per findBy*; a test chaining two of them needs a larger budget than the
    // 5 s default, or it fails the whole test before its own wait expires.
    testTimeout: 20_000,
    hookTimeout: 20_000,
  },
});
