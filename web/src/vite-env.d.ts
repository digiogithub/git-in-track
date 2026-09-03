/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Overrides provider auto-detection during development. */
  readonly VITE_FORCE_PROVIDER?: 'browser' | 'companion';
  /** Base URL of the companion API. Defaults to the same origin. */
  readonly VITE_COMPANION_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
