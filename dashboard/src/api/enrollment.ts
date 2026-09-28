import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './keys';
import type {
  CreateEnrollmentTokenRequest,
  CreatedEnrollmentToken,
  EnrollmentToken,
  ListResponse,
} from './types';

export function useEnrollmentTokens() {
  return useInfiniteQuery({
    queryKey: qk.enrollment.all,
    queryFn: ({ pageParam, signal }) =>
      api<ListResponse<EnrollmentToken>>('/enrollment-tokens', {
        query: { limit: 50, cursor: pageParam },
        signal,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useCreateEnrollmentToken() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateEnrollmentTokenRequest) =>
      api<CreatedEnrollmentToken>('/enrollment-tokens', { method: 'POST', body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.enrollment.all }),
  });
}

export function useRevokeEnrollmentToken() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      api<void>(`/enrollment-tokens/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.enrollment.all }),
  });
}
