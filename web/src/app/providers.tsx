import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { useEffect, useState, type ReactNode } from 'react';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { detectMode } from '@/api/detect';
import type { DataProvider } from '@/api/provider';
import { createDataProvider } from '@/api/provider-factory';
import { createQueryClient } from '@/app/queryClient';
import { useAppStore } from '@/app/store';

export type AppProvidersProps = {
  children: ReactNode;
  /** Injected by tests; production creates its own client. */
  queryClient?: QueryClient;
  /** Injected by tests; production builds one from the detected mode. */
  provider?: DataProvider;
};

/**
 * Every app-wide provider in one place: the query cache and the data provider
 * today, theme and i18n next.
 *
 * Mode detection runs once on mount (docs/05-web-app.md §4.3) and its result
 * decides which provider implementation is built. The shell waits for that one
 * probe instead of rendering features against a provider that may be replaced
 * a tick later.
 */
export function AppProviders({ children, queryClient, provider }: AppProvidersProps) {
  const [client] = useState(() => queryClient ?? createQueryClient());
  const [dataProvider, setDataProvider] = useState<DataProvider | null>(provider ?? null);

  useEffect(() => {
    if (provider) {
      setDataProvider(provider);
      return;
    }

    let cancelled = false;
    void detectMode().then((mode) => {
      if (cancelled) return;
      const built = createDataProvider({ mode });
      useAppStore.getState().setCapabilities(built.capabilities);
      setDataProvider(built);
    });

    return () => {
      cancelled = true;
    };
  }, [provider]);

  return (
    <QueryClientProvider client={client}>
      {dataProvider ? (
        <DataProviderProvider provider={dataProvider}>{children}</DataProviderProvider>
      ) : (
        <BootScreen />
      )}
    </QueryClientProvider>
  );
}

/** Shown for the length of one companion probe, then replaced by the shell. */
function BootScreen() {
  return (
    <div
      className="flex min-h-screen items-center justify-center bg-background text-sm text-muted-foreground"
      role="status"
    >
      Starting git-in-track…
    </div>
  );
}
