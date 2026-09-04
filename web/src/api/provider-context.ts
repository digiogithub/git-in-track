import { createContext, useContext } from 'react';

import type { DataProvider } from '@/api/provider';

export const ProviderContext = createContext<DataProvider | null>(null);

/** Returns the active provider. Throws when rendered outside `DataProviderProvider`. */
export function useProvider(): DataProvider {
  const provider = useContext(ProviderContext);
  if (!provider) {
    throw new Error('useProvider must be used inside <DataProviderProvider>');
  }
  return provider;
}

/** Returns the active provider or `null` when none is mounted yet. */
export function useOptionalProvider(): DataProvider | null {
  return useContext(ProviderContext);
}
