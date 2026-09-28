import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './keys';
import type { PublicConfig } from './types';

/** GET /v1/config: gateway_url (for install commands) and server version. */
export function useConfig() {
  return useQuery({
    queryKey: qk.config,
    queryFn: ({ signal }) => api<PublicConfig>('/config', { signal }),
    staleTime: 10 * 60_000,
  });
}
