import { useState, type ReactNode } from 'react';
import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { isApiError } from '@/api/client';
import { AuthProvider } from '@/auth/AuthProvider';
import { Toaster } from '@/components/ui/toaster';
import { toastError } from '@/components/ui/toast-store';

declare module '@tanstack/react-query' {
  interface Register {
    mutationMeta: {
      /** Title for the automatic error toast; set `silent: true` to handle errors yourself. */
      errorTitle?: string;
      silent?: boolean;
    };
  }
}

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 15_000,
        refetchOnWindowFocus: true,
        retry: (failureCount, error) => {
          // Never retry client errors (4xx); retry transient failures twice.
          if (isApiError(error) && error.status >= 400 && error.status < 500) return false;
          return failureCount < 2;
        },
      },
      mutations: { retry: false },
    },
    mutationCache: new MutationCache({
      onError: (error, _vars, _ctx, mutation) => {
        if (mutation.meta?.silent) return;
        toastError(error, mutation.meta?.errorTitle ?? 'Action failed');
      },
    }),
  });
}

export function AppProviders({ children }: { children: ReactNode }) {
  const [client] = useState(makeQueryClient);
  return (
    <QueryClientProvider client={client}>
      <AuthProvider>{children}</AuthProvider>
      <Toaster />
    </QueryClientProvider>
  );
}
