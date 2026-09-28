import type {
  AuditOutcome,
  CommandStatus,
  Connectivity,
  DeviceStatus,
} from '@/api/types';
import { Badge, type BadgeTone } from './ui/badge';
import { cn } from '@/lib/cn';

const deviceTones: Record<DeviceStatus, BadgeTone> = {
  pending: 'yellow',
  active: 'green',
  disabled: 'gray',
  revoked: 'red',
};

export function DeviceStatusBadge({ status }: { status: DeviceStatus }) {
  return <Badge tone={deviceTones[status] ?? 'neutral'}>{status}</Badge>;
}

export function ConnectivityBadge({ connectivity }: { connectivity: Connectivity }) {
  const online = connectivity === 'online';
  return (
    <span className="inline-flex items-center gap-1.5 text-xs font-medium whitespace-nowrap">
      <span
        className={cn(
          'size-2 rounded-full',
          online ? 'bg-emerald-500 ring-2 ring-emerald-500/25' : 'bg-gray-400 dark:bg-gray-500',
        )}
        aria-hidden
      />
      <span className={online ? 'text-emerald-700 dark:text-emerald-400' : 'text-muted'}>
        {online ? 'Online' : 'Offline'}
      </span>
    </span>
  );
}

const commandTones: Record<CommandStatus, BadgeTone> = {
  queued: 'gray',
  sent: 'blue',
  acked: 'blue',
  running: 'purple',
  succeeded: 'green',
  failed: 'red',
  timed_out: 'red',
  canceled: 'gray',
  expired: 'yellow',
  rejected: 'red',
};

export function CommandStatusBadge({ status }: { status: CommandStatus }) {
  return <Badge tone={commandTones[status] ?? 'neutral'}>{status.replace('_', ' ')}</Badge>;
}

const outcomeTones: Record<AuditOutcome, BadgeTone> = {
  success: 'green',
  denied: 'yellow',
  error: 'red',
};

export function OutcomeBadge({ outcome }: { outcome: AuditOutcome }) {
  return <Badge tone={outcomeTones[outcome] ?? 'neutral'}>{outcome}</Badge>;
}
