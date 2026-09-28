import type { AuditListParams, DeviceListParams } from './types';

/** Centralised TanStack Query keys so SSE handlers and mutations can target them. */
export const qk = {
  me: ['me'] as const,
  config: ['config'] as const,
  devices: {
    all: ['devices'] as const,
    lists: () => ['devices', 'list'] as const,
    list: (p: DeviceListParams) => ['devices', 'list', p] as const,
    summary: () => ['devices', 'summary'] as const,
    detail: (id: string) => ['devices', 'detail', id] as const,
    activity: (id: string) => ['devices', 'activity', id] as const,
  },
  commands: {
    all: ['commands'] as const,
    byDevice: (deviceId: string) => ['commands', 'device', deviceId] as const,
    detail: (id: string) => ['commands', 'detail', id] as const,
  },
  sessions: {
    all: ['exit-sessions'] as const,
    byDevice: (deviceId: string) => ['exit-sessions', 'device', deviceId] as const,
  },
  enrollment: {
    all: ['enrollment-tokens'] as const,
  },
  audit: {
    all: ['audit'] as const,
    list: (p: AuditListParams) => ['audit', 'list', p] as const,
  },
  users: {
    all: ['users'] as const,
    roles: ['roles'] as const,
  },
};
