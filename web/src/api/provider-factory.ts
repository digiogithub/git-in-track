/**
 * Builds the `DataProvider` for the detected runtime (docs/05-web-app.md §4.3).
 *
 * Companion mode gets the REST client over `/api/v1`; every other mode gets the
 * browser provider (File System Access plus the WASM core). Nothing else in the
 * app knows which one it is talking to.
 */

import { BrowserProvider } from '@/api/browser-provider';
import { CompanionProvider, type CompanionProviderOptions } from '@/api/companion-provider';
import type { DataProvider } from '@/api/provider';
import type { AppMode } from '@/app/store';
import type { CoreClient } from '@/core-bridge/client';

export type ProviderFactoryOptions = {
  mode: AppMode;
  /** Injected by tests; the browser provider shares the app-wide worker client. */
  client?: CoreClient;
  /** Injected by tests; production reads the base URL and token from the page. */
  companion?: CompanionProviderOptions;
};

export function createDataProvider({
  mode,
  client,
  companion,
}: ProviderFactoryOptions): DataProvider {
  if (mode === 'companion') return new CompanionProvider(companion ?? {});
  return new BrowserProvider(client ? { client } : {});
}

/**
 * Waits for whatever a freshly built provider needs before the UI reads its
 * capabilities. Only the companion has such a step (`GET /capabilities`).
 */
export async function whenProviderReady(provider: DataProvider): Promise<void> {
  if (provider instanceof CompanionProvider) await provider.ready;
}

/** Releases a provider that is being replaced by a mode flip. */
export function disposeProvider(provider: DataProvider | null): void {
  if (provider instanceof CompanionProvider) provider.dispose();
}
