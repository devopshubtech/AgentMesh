import { Smartphone, Unplug } from 'lucide-react';
import { useDeviceSessions, useTerminateSession } from '@/api/sessions';
import type { ExitSession } from '@/api/types';
import { useAuth, useCan } from '@/auth/context';
import { Badge, type BadgeTone } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { EmptyState, ErrorState, PageLoader } from '@/components/ui/feedback';
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table';
import { RelativeTime } from '@/components/ui/time';
import { toast } from '@/components/ui/toast-store';
import { formatBytes, formatDateTime, formatDurationSeconds } from '@/lib/format';

const STATUS_TONE: Record<ExitSession['status'], BadgeTone> = { pending: 'yellow', active: 'green', ended: 'gray' };

const REASONS: Record<string, string> = {
  client_closed: 'Phone disconnected',
  agent_closed: 'Agent disconnected',
  terminated: 'Ended by operator',
  max_duration: 'Reached 12 h limit',
  join_timeout: 'Never connected',
  expired: 'Expired',
  device_revoke: 'Device revoked',
  device_disable: 'Device disabled',
};

function durationOf(s: ExitSession): string {
  if (!s.started_at) return '—';
  const end = s.ended_at ? new Date(s.ended_at).getTime() : Date.now();
  return formatDurationSeconds(Math.max(0, Math.round((end - new Date(s.started_at).getTime()) / 1000)));
}

/** Who is using a device as an exit node. */
function useSessions(deviceId: string) {
  const canUse = useCan('sessions.exit_node');
  const canAudit = useCan('audit.read');
  const canSee = canUse || canAudit;
  return { canSee, q: useDeviceSessions(deviceId, canSee) };
}

/** Prominent banner on the device page while phones are routed through it. */
export function ConnectedClientsBanner({ deviceId }: { deviceId: string }) {
  const { canSee, q } = useSessions(deviceId);
  const live = q.data?.items.filter((s) => s.status === 'active') ?? [];
  if (!canSee || live.length === 0) return null;
  return (
    <div className="mb-4 rounded-md border border-emerald-300 bg-emerald-50 px-4 py-3 text-sm text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-200">
      <div className="mb-1 flex items-center gap-2 font-semibold">
        <Smartphone className="size-4" aria-hidden />
        {live.length === 1 ? '1 phone is' : `${live.length} phones are`} using this device as exit node
      </div>
      <ul className="flex flex-col gap-0.5">
        {live.map((s) => (
          <li key={s.id}>
            <span className="font-medium">{s.client_label || 'Unknown client'}</span> · {s.user.email}
            {s.client_ip && <> · from {s.client_ip}</>} · connected <RelativeTime value={s.started_at} />
          </li>
        ))}
      </ul>
    </div>
  );
}

export function ConnectedClientsTab({ deviceId }: { deviceId: string }) {
  const { canSee, q } = useSessions(deviceId);
  const terminate = useTerminateSession();
  const canManage = useCan('devices.manage');
  const { user } = useAuth();

  if (!canSee) {
    return <EmptyState title="Not permitted" description="You need the sessions.exit_node or audit.read permission." />;
  }
  if (q.isPending) return <PageLoader label="Loading connected clients…" />;
  if (q.isError) return <ErrorState error={q.error} onRetry={() => void q.refetch()} />;
  const items = q.data.items;
  if (items.length === 0) {
    return (
      <EmptyState
        title="No phones have connected yet"
        description="Phones that use this device as exit node (AgentMesh Android app → Use as exit node) appear here."
      />
    );
  }
  return (
    <div className="-mx-4 overflow-x-auto">
      <Table>
        <THead>
          <TR>
            <TH>Client</TH>
            <TH>User</TH>
            <TH>Client IP</TH>
            <TH>Status</TH>
            <TH>Connected</TH>
            <TH>Duration</TH>
            <TH>Data (up / down)</TH>
            <TH />
          </TR>
        </THead>
        <TBody>
          {items.map((s) => (
            <TR key={s.id}>
              <TD>
                <div className="flex items-center gap-2">
                  <Smartphone className="size-4 text-muted" aria-hidden />
                  <span className="font-medium">{s.client_label || 'Unknown client'}</span>
                </div>
              </TD>
              <TD>{s.user.email}</TD>
              <TD className="font-mono text-xs">{s.client_ip || '—'}</TD>
              <TD>
                <div className="flex flex-col gap-0.5">
                  <Badge tone={STATUS_TONE[s.status]}>{s.status === 'active' ? 'connected' : s.status}</Badge>
                  {s.end_reason && <span className="text-xs text-muted">{REASONS[s.end_reason] ?? s.end_reason}</span>}
                </div>
              </TD>
              <TD title={formatDateTime(s.started_at ?? s.created_at)}>
                <RelativeTime value={s.started_at ?? s.created_at} />
              </TD>
              <TD>{durationOf(s)}</TD>
              <TD className="whitespace-nowrap text-xs">
                {s.status === 'ended' ? `${formatBytes(s.bytes_up)} / ${formatBytes(s.bytes_down)}` : 'counted at end'}
              </TD>
              <TD>
                {s.status !== 'ended' && (canManage || s.user.id === user?.id) && (
                  <Button
                    size="sm"
                    variant="outline"
                    icon={<Unplug className="size-4" />}
                    loading={terminate.isPending && terminate.variables === s.id}
                    onClick={() =>
                      terminate.mutate(s.id, { onSuccess: () => toast.success('Session ended', s.client_label) })
                    }
                  >
                    Disconnect
                  </Button>
                )}
              </TD>
            </TR>
          ))}
        </TBody>
      </Table>
    </div>
  );
}
