import { useEffect, useRef, useState } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { startSse, type SseMessage, type SseStatus } from '@/lib/sse';
import { appendOutput } from '@/features/commands/outputStore';
import { API_BASE, getFreshAccessToken, refreshSession, isApiError } from './client';
import { qk } from './keys';
import type {
  CommandOutputEvent,
  CommandUpdatedEvent,
  Device,
  DeviceStatusEvent,
  DeviceUpdatedEvent,
} from './types';
import type { DeviceListData } from './devices';

function parse<T>(data: string): T | null {
  try {
    const v: unknown = JSON.parse(data);
    return v && typeof v === 'object' ? (v as T) : null;
  } catch {
    return null;
  }
}

function patchDevice(qc: QueryClient, ev: DeviceStatusEvent) {
  const patch = (d: Device): Device =>
    d.id === ev.device_id
      ? {
          ...d,
          status: ev.status ?? d.status,
          connectivity: ev.connectivity ?? d.connectivity,
          last_seen_at: ev.last_seen_at ?? d.last_seen_at,
        }
      : d;

  let known = false;
  qc.setQueryData<Device>(qk.devices.detail(ev.device_id), (old) => {
    if (!old) return old;
    known = true;
    return patch(old);
  });
  qc.setQueriesData<DeviceListData>({ queryKey: qk.devices.lists() }, (old) => {
    if (!old) return old;
    return {
      ...old,
      pages: old.pages.map((p) => ({
        ...p,
        items: p.items.map((d) => {
          if (d.id === ev.device_id) known = true;
          return patch(d);
        }),
      })),
    };
  });
  if (!known) {
    // A device we have not seen (e.g. freshly enrolled): refetch lists.
    void qc.invalidateQueries({ queryKey: qk.devices.lists() });
  }
  void qc.invalidateQueries({ queryKey: qk.devices.summary() });
}

export function handleEvent(qc: QueryClient, msg: SseMessage): void {
  switch (msg.event) {
    case 'device.status': {
      const ev = parse<DeviceStatusEvent>(msg.data);
      if (ev?.device_id) patchDevice(qc, ev);
      break;
    }
    case 'device.updated': {
      const ev = parse<DeviceUpdatedEvent>(msg.data);
      if (ev?.device_id) {
        void qc.invalidateQueries({ queryKey: qk.devices.detail(ev.device_id) });
        void qc.invalidateQueries({ queryKey: qk.devices.lists() });
      }
      break;
    }
    case 'command.updated': {
      const ev = parse<CommandUpdatedEvent>(msg.data);
      if (ev?.command_id) {
        void qc.invalidateQueries({ queryKey: qk.commands.detail(ev.command_id) });
        if (ev.device_id) {
          void qc.invalidateQueries({ queryKey: qk.commands.byDevice(ev.device_id) });
        }
      }
      break;
    }
    case 'command.output': {
      const ev = parse<CommandOutputEvent>(msg.data);
      if (ev?.command_id) appendOutput(ev);
      break;
    }
    default:
      break;
  }
}

/** Keeps one SSE connection open for the authenticated tab. */
export function useLiveEvents(enabled: boolean): SseStatus {
  const qc = useQueryClient();
  const [status, setStatus] = useState<SseStatus>('closed');

  useEffect(() => {
    if (!enabled) return;
    const stop = startSse({
      url: `${API_BASE}/events`,
      getToken: getFreshAccessToken,
      onUnauthorized: async () => {
        try {
          await refreshSession();
          return true;
        } catch (e) {
          // A rejected refresh means the session is gone; the API layer handles logout.
          return !(isApiError(e) && e.status === 401);
        }
      },
      onMessage: (m) => handleEvent(qc, m),
      onStatusChange: setStatus,
    });
    return stop;
  }, [enabled, qc]);

  // When the stream RE-opens, refetch active queries to catch up on missed events.
  const openedBefore = useRef(false);
  useEffect(() => {
    if (status !== 'open') return;
    if (openedBefore.current) {
      void qc.invalidateQueries({ queryKey: qk.devices.all, refetchType: 'active' });
      void qc.invalidateQueries({ queryKey: qk.commands.all, refetchType: 'active' });
    }
    openedBefore.current = true;
  }, [status, qc]);

  return status;
}
