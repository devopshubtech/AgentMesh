import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import type { ListResponse } from './types';

/** A shareable, revocable link that connects phones to one exit-node device. */
export interface ConnectKey {
  id: string;
  device_id: string;
  label: string;
  created_at: string;
  expires_at: string | null;
  revoked_at: string | null;
  uses: number;
  last_used_at: string | null;
  created_by: string;
  active_sessions: number;
}

export interface CreatedConnectKey extends ConnectKey {
  key: string;
  link: string;
}

const keysKey = (deviceId: string) => ['connect-keys', deviceId] as const;

export function useConnectKeys(deviceId: string | undefined) {
  return useQuery({
    queryKey: keysKey(deviceId ?? ''),
    queryFn: ({ signal }) =>
      api<ListResponse<ConnectKey>>(`/devices/${encodeURIComponent(deviceId ?? '')}/connect-keys`, { signal }),
    enabled: !!deviceId,
    refetchInterval: 15_000,
  });
}

export function useCreateConnectKey() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ deviceId, label, expiresInS }: { deviceId: string; label: string; expiresInS: number }) =>
      api<CreatedConnectKey>(`/devices/${encodeURIComponent(deviceId)}/connect-keys`, {
        method: 'POST',
        body: { label, expires_in_s: expiresInS },
      }),
    onSuccess: (k) => qc.invalidateQueries({ queryKey: keysKey(k.device_id) }),
  });
}

export function useRevokeConnectKey(deviceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<void>(`/connect-keys/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keysKey(deviceId) }),
  });
}

/** A short-lived 6-digit code typed into the phone app instead of scanning a QR code. */
export interface PairCode {
  id: string;
  device_id: string;
  label: string;
  created_at: string;
  expires_at: string;
  revoked_at: string | null;
  uses: number;
  /** Only present in the create response. */
  code?: string;
}

export function useCreatePairCode() {
  return useMutation({
    mutationFn: ({ deviceId, label }: { deviceId: string; label: string }) =>
      api<PairCode>(`/devices/${encodeURIComponent(deviceId)}/pair-codes`, { method: 'POST', body: { label } }),
  });
}

export function useRevokePairCode() {
  return useMutation({
    mutationFn: (id: string) => api<void>(`/pair-codes/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  });
}
