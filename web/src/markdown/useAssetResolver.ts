/**
 * Per-page object URL registry (docs/05-web-app.md §7, "Images and assets").
 *
 * Browser-only mode has no URL for a file inside a directory handle, so image
 * bytes are read through the provider and wrapped in an object URL. Those URLs
 * leak until revoked, so the registry is owned by the page: one map per mounted
 * viewer, revoked on unmount, capped so a very long page cannot pin unbounded
 * memory.
 */

import { useCallback, useEffect, useRef } from 'react';

import type { ResolveAsset } from '@/markdown/types';

const DEFAULT_LIMIT = 64;

export type AssetLoader = (vaultPath: string) => Promise<Blob>;

export function useAssetResolver(load: AssetLoader, limit = DEFAULT_LIMIT): ResolveAsset {
  const urls = useRef(new Map<string, string>());
  const pending = useRef(new Map<string, Promise<string>>());
  const loader = useRef(load);
  loader.current = load;

  useEffect(() => {
    const registry = urls.current;
    return () => {
      for (const url of registry.values()) URL.revokeObjectURL(url);
      registry.clear();
    };
  }, []);

  return useCallback(
    (vaultPath: string) => {
      const cached = urls.current.get(vaultPath);
      if (cached) return Promise.resolve(cached);

      const inFlight = pending.current.get(vaultPath);
      if (inFlight) return inFlight;

      const promise = loader
        .current(vaultPath)
        .then((blob) => {
          const url = URL.createObjectURL(blob);
          urls.current.set(vaultPath, url);
          if (urls.current.size > limit) {
            const oldest = urls.current.keys().next().value;
            if (oldest !== undefined && oldest !== vaultPath) {
              const stale = urls.current.get(oldest);
              if (stale) URL.revokeObjectURL(stale);
              urls.current.delete(oldest);
            }
          }
          pending.current.delete(vaultPath);
          return url;
        })
        .catch((error: unknown) => {
          // A failed read must not poison the cache: the next render retries.
          pending.current.delete(vaultPath);
          throw error;
        });
      pending.current.set(vaultPath, promise);
      return promise;
    },
    [limit],
  );
}
