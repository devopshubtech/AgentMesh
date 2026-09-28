import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { api } from './client';
import { qk } from './keys';
import type {
  AuditEntry,
  Device,
  DeviceListParams,
  DeviceSummary,
  ListResponse,
  UpdateDeviceRequest,
} from './types';

export type DeviceListData = InfiniteData<ListResponse<Device>, string | null>;

export function useDevices(params: DeviceListParams) {
  return useInfiniteQuery({
    queryKey: qk.devices.list(params),
    queryFn: ({ pageParam, signal }) =>
      api<ListResponse<Device>>('/devices', {
        query: { ...params, limit: params.limit ?? 50, cursor: pageParam },
        signal,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useDeviceSummary() {
  return useQuery({
    queryKey: qk.devices.summary(),
    queryFn: ({ signal }) => api<DeviceSummary>('/devices/summary', { signal }),
  });
}

export function useDevice(id: string) {
  return useQuery({
    queryKey: qk.devices.detail(id),
    queryFn: ({ signal }) => api<Device>(`/devices/${encodeURIComponent(id)}`, { signal }),
  });
}

export function useDeviceActivity(id: string, enabled = true) {
  return useInfiniteQuery({
    queryKey: qk.devices.activity(id),
    queryFn: ({ pageParam, signal }) =>
      api<ListResponse<AuditEntry>>(`/devices/${encodeURIComponent(id)}/activity`, {
        query: { limit: 50, cursor: pageParam },
        signal,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled,
  });
}

/** Writes a fresh Device into every cache that holds it. */
export function useApplyDevice() {
  const qc = useQueryClient();
  return (device: Device) => {
    qc.setQueryData(qk.devices.detail(device.id), device);
    qc.setQueriesData<DeviceListData>({ queryKey: qk.devices.lists() }, (old) =>
      old
        ? {
            ...old,
            pages: old.pages.map((p) => ({
              ...p,
              items: p.items.map((d) => (d.id === device.id ? device : d)),
            })),
          }
        : old,
    );
    void qc.invalidateQueries({ queryKey: qk.devices.summary() });
    void qc.invalidateQueries({ queryKey: qk.devices.activity(device.id) });
  };
}

export type DeviceAction = 'approve' | 'disable' | 'enable' | 'revoke';

export function useDeviceAction() {
  const apply = useApplyDevice();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, action }: { id: string; action: DeviceAction }) =>
      api<Device | undefined>(`/devices/${encodeURIComponent(id)}/${action}`, { method: 'POST' }),
    onSuccess: (device, vars) => {
      // The contract does not pin the response body of these endpoints; if a Device
      // comes back we patch caches, otherwise we just refetch.
      if (device && typeof device === 'object' && 'id' in device && device.id === vars.id) {
        apply(device);
      } else {
        void qc.invalidateQueries({ queryKey: qk.devices.detail(vars.id) });
        void qc.invalidateQueries({ queryKey: qk.devices.lists() });
        void qc.invalidateQueries({ queryKey: qk.devices.summary() });
        void qc.invalidateQueries({ queryKey: qk.devices.activity(vars.id) });
      }
      if (vars.action === 'revoke') {
        void qc.invalidateQueries({ queryKey: qk.commands.byDevice(vars.id) });
      }
    },
  });
}

export function useRenameDevice() {
  const apply = useApplyDevice();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: UpdateDeviceRequest }) =>
      api<Device>(`/devices/${encodeURIComponent(id)}`, { method: 'PATCH', body }),
    onSuccess: (device) => apply(device),
  });
}
