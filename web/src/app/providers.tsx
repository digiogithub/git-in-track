import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';

import { createQueryClient } from '@/app/queryClient';

export type AppProvidersProps = {
  children: ReactNode;
  /** Injected by tests; production creates its own client. */
  queryClient?: QueryClient;
};

/** Every app-wide provider in one place: query cache today, theme and i18n next. */
export function AppProviders({ children, queryClient }: AppProvidersProps) {
  const [client] = useState(() => queryClient ?? createQueryClient());
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
