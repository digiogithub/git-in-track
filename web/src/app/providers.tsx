import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { useEffect, useState, type ReactNode } from 'react';

import { CompanionProvider, type CompanionProviderOptions } from '@/api/companion-provider';
import { DataProviderProvider } from '@/api/DataProviderProvider';
import { detectCompanion, watchCompanion } from '@/api/detect';
import type { DataProvider, Unsubscribe } from '@/api/provider';
import { createDataProvider, disposeProvider, whenProviderReady } from '@/api/provider-factory';
import { captureTokenFromUrl, hasToken, onTokenChange } from '@/api/token';
import { createQueryClient } from '@/app/queryClient';
import { useAppStore, type AppMode } from '@/app/store';

export type DetectionOptions = {
  /** Injected by tests; production probes with the global `fetch`. */
  fetchImpl?: typeof fetch;
  /** Injected by tests; production re-probes every 30 s. */
  intervalMs?: number;
  /** Injected by tests; production builds the companion from the page URL. */
  companion?: CompanionProviderOptions;
};

export type AppProvidersProps = {
  children: ReactNode;
  /** Injected by tests; production creates its own client. */
  queryClient?: QueryClient;
  /** Injected by tests; production builds one from the detected mode. */
  provider?: DataProvider;
  detection?: DetectionOptions;
};

/**
 * Every app-wide provider in one place: the query cache and the data provider
 * today, theme and i18n next.
 *
 * Mode detection runs once on mount (docs/05-web-app.md §4.3) and its result
 * decides which provider implementation is built. The shell waits for that one
 * probe instead of rendering features against a provider that may be replaced
 * a tick later.
 *
 * Detection does not stop there: a cheap `GET /health` runs every 30 s, so
 * starting `gintrack serve` upgrades a tab that is already open — the provider
 * is rebuilt, the query cache is invalidated and a non-blocking notice
 * explains what changed. Stopping the companion downgrades the same way.
 */
export function AppProviders({ children, queryClient, provider, detection }: AppProvidersProps) {
  const [client] = useState(() => queryClient ?? createQueryClient());
  const [options] = useState<DetectionOptions>(() => detection ?? {});
  const [dataProvider, setDataProvider] = useState<DataProvider | null>(provider ?? null);

  useEffect(() => {
    if (provider) {
      setDataProvider(provider);
      return;
    }

    let cancelled = false;
    let current: DataProvider | null = null;
    let detach: Unsubscribe | null = null;
    let stopWatching: Unsubscribe | null = null;

    /** Builds the provider for `mode` and publishes it to the tree. */
    const attach = async (mode: AppMode): Promise<void> => {
      const built = createDataProvider({
        mode,
        ...(options.companion === undefined ? {} : { companion: options.companion }),
      });
      // The companion reads `GET /capabilities` before the UI branches on it.
      await whenProviderReady(built);
      if (cancelled) {
        disposeProvider(built);
        return;
      }

      detach?.();
      detach = null;
      const previous = current;
      current = built;

      const store = useAppStore.getState();
      store.setCapabilities(built.capabilities);

      if (built instanceof CompanionProvider) {
        store.setCompanionUrl(built.baseUrl);
        store.setCompanionAuth(hasToken() ? 'ok' : 'required');
        // One app-level subscription keeps the event socket (and the connection
        // state Settings shows) alive even when no feature is listening.
        const stopKeepAlive = built.subscribe(() => undefined);
        const stopState = built.onConnectionStateChange((state) => {
          useAppStore.getState().setConnection(state);
        });
        detach = () => {
          stopKeepAlive();
          stopState();
        };
      } else {
        store.setConnection('idle');
        store.setCompanionAuth('ok');
      }

      setDataProvider(built);
      disposeProvider(previous);
    };

    // A companion that serves the app hands the token over in the URL once.
    captureTokenFromUrl();
    const stopTokenWatch = onTokenChange((token) => {
      const store = useAppStore.getState();
      if (store.mode !== 'companion') return;
      store.setCompanionAuth(token === null ? 'required' : 'ok');
      if (token !== null) void client.invalidateQueries();
    });

    const probeOptions = options.fetchImpl === undefined ? {} : { fetchImpl: options.fetchImpl };

    void detectCompanion(probeOptions).then(async (status) => {
      await attach(status.mode);
      if (cancelled) return;

      stopWatching = watchCompanion({
        ...probeOptions,
        ...(options.intervalMs === undefined ? {} : { intervalMs: options.intervalMs }),
        onChange: (next) => {
          useAppStore
            .getState()
            .setModeNotice(next.mode === 'companion' ? 'companion-detected' : 'companion-lost');
          void attach(next.mode).then(() => {
            // Everything cached came from the previous runtime.
            void client.invalidateQueries();
          });
        },
      });
    });

    return () => {
      cancelled = true;
      stopWatching?.();
      stopTokenWatch();
      detach?.();
      disposeProvider(current);
    };
  }, [provider, client, options]);

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
