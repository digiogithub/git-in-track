import type { ReactNode } from 'react';

import type { DataProvider } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';

/** Makes a `DataProvider` available to the component tree. */
export function DataProviderProvider({
  provider,
  children,
}: {
  provider: DataProvider;
  children: ReactNode;
}) {
  return <ProviderContext.Provider value={provider}>{children}</ProviderContext.Provider>;
}
