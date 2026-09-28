import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './keys';
import type { ExitSession, ListResponse } from './types';

/**
 * Exit-node sessions of a device, newest first. While a session is live the
 * list is refreshed periodically so durations stay current; status changes
 * also arrive instantly over SSE (session.updated).
 */
export function useDeviceSessions(deviceId: string, enabled = true) {
  return useQuery({
    queryKey: qk.sessions.byDevice(deviceId),
    queryFn: ({ signal }) =>
      api<ListResponse<ExitSession>>('/exit-sessions', {
        query: { device_id: deviceId, limit: 50 },
        signal,
      }),
    enabled,
    refetchInterval: (q) => (q.state.data?.items.some((s) => s.status !== 'ended') ? 15_000 : false),
  });
}

export function useTerminateSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<ExitSession>(`/exit-sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: (s) => void qc.invalidateQueries({ queryKey: qk.sessions.byDevice(s.device_id) }),
  });
}
