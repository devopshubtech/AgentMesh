import { useInfiniteQuery, useMutation } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './keys';
import type { AuditEntry, AuditListParams, AuditVerifyResult, ListResponse } from './types';

export function useAuditLog(params: AuditListParams) {
  return useInfiniteQuery({
    queryKey: qk.audit.list(params),
    queryFn: ({ pageParam, signal }) =>
      api<ListResponse<AuditEntry>>('/audit', {
        query: { ...params, limit: params.limit ?? 50, cursor: pageParam },
        signal,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** Verification can be expensive, so it is triggered explicitly (mutation semantics). */
export function useVerifyAudit() {
  return useMutation({
    mutationFn: () => api<AuditVerifyResult>('/audit/verify'),
  });
}
