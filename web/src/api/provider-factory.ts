/**
 * Builds the `DataProvider` for the detected runtime (docs/05-web-app.md §4.3).
 *
 * Phase 1 ships one implementation. The companion REST client is Phase 2, so
 * companion mode currently also gets the `BrowserProvider`: the app keeps
 * working against local folders while the probe result only drives the mode
 * badge.
 */

import { BrowserProvider } from '@/api/browser-provider';
import type { DataProvider } from '@/api/provider';
import type { AppMode } from '@/app/store';
import type { CoreClient } from '@/core-bridge/client';

export type ProviderFactoryOptions = {
  mode: AppMode;
  /** Injected by tests. */
  client?: CoreClient;
};

export function createDataProvider({ mode, client }: ProviderFactoryOptions): DataProvider {
  // TODO(GIT-US-0014): `mode === 'companion'` must return a `CompanionProvider`
  // backed by the REST client and the WebSocket event stream once the Phase 2
  // API lands; until then every mode is served by the browser provider.
  void mode;
  return new BrowserProvider(client ? { client } : {});
}
