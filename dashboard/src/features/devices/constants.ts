import type { SelectOption } from '@/components/ui/select';

export const STATUS_OPTIONS: SelectOption[] = [
  { value: 'pending', label: 'Pending' },
  { value: 'active', label: 'Active' },
  { value: 'disabled', label: 'Disabled' },
  { value: 'revoked', label: 'Revoked' },
];

export const CONNECTIVITY_OPTIONS: SelectOption[] = [
  { value: 'online', label: 'Online' },
  { value: 'offline', label: 'Offline' },
];

export const PLATFORM_OPTIONS: SelectOption[] = [
  { value: 'linux', label: 'Linux' },
  { value: 'windows', label: 'Windows' },
  { value: 'darwin', label: 'macOS' },
  { value: 'android', label: 'Android' },
  { value: 'ios', label: 'iOS' },
];

export function platformLabel(p: string): string {
  return PLATFORM_OPTIONS.find((o) => o.value === p)?.label ?? p;
}

export function osLabel(d: { os_name: string; os_version: string }): string {
  return [d.os_name, d.os_version].filter(Boolean).join(' ') || '—';
}
