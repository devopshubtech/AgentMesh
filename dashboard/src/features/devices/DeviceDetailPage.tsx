import { useState, type FormEvent } from 'react';
import { Link, useParams } from 'react-router';
import {
  ArrowLeft,
  Ban,
  Check,
  CheckCircle2,
  Pencil,
  PlayCircle,
  ShieldX,
  X,
} from 'lucide-react';
import { isApiError } from '@/api/client';
import { useDevice, useDeviceAction, useRenameDevice, type DeviceAction } from '@/api/devices';
import type { Device } from '@/api/types';
import { useCan } from '@/auth/context';
import { ConnectivityBadge, DeviceStatusBadge } from '@/components/status-badges';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ui/dialog';
import { EmptyState, ErrorState, PageLoader } from '@/components/ui/feedback';
import { Input } from '@/components/ui/input';
import { Tabs, TabPanel, type TabDef } from '@/components/ui/tabs';
import { toast } from '@/components/ui/toast-store';
import { CommandsTab } from '@/features/commands/CommandsTab';
import { DeviceOverview } from './DeviceOverview';
import { ActivityTab } from './ActivityTab';
import { ConnectedClientsBanner, ConnectedClientsTab } from './ConnectedClients';

type TabValue = 'commands' | 'phones' | 'activity' | 'metrics' | 'logs' | 'packages' | 'artifacts' | 'access';

const PHASE2 = 'Coming in Phase 2+';

const TABS: TabDef<TabValue>[] = [
  { value: 'commands', label: 'Commands' },
  { value: 'phones', label: 'Connected phones' },
  { value: 'activity', label: 'Activity' },
  ...(['metrics', 'logs', 'packages', 'artifacts', 'access'] as const).map((v) => ({
    value: v,
    label: (
      <>
        {v.charAt(0).toUpperCase() + v.slice(1)}
        <span className="rounded bg-subtle px-1 text-[10px] font-normal text-muted">{PHASE2}</span>
      </>
    ),
    disabled: true,
    hint: PHASE2,
  })),
];

function NameEditor({ device, canEdit }: { device: Device; canEdit: boolean }) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(device.name);
  const rename = useRenameDevice();

  if (!editing) {
    return (
      <div className="flex min-w-0 items-center gap-2">
        <h1 className="truncate text-xl font-semibold text-fg" title={device.name}>
          {device.name || device.hostname}
        </h1>
        {canEdit && device.status !== 'revoked' && (
          <Button
            variant="ghost"
            size="icon"
            onClick={() => {
              setValue(device.name);
              setEditing(true);
            }}
            aria-label="Rename device"
            title="Rename device"
          >
            <Pencil className="size-4" />
          </Button>
        )}
      </div>
    );
  }

  const trimmed = value.trim();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!trimmed || trimmed === device.name) {
      setEditing(false);
      return;
    }
    rename.mutate(
      { id: device.id, body: { name: trimmed } },
      {
        onSuccess: () => {
          toast.success('Device renamed');
          setEditing(false);
        },
      },
    );
  };

  return (
    <form onSubmit={submit} className="flex items-center gap-2">
      <Input
        autoFocus
        value={value}
        maxLength={255}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.preventDefault();
            setEditing(false);
          }
        }}
        aria-label="Device name"
        className="h-9 w-72 text-base font-semibold"
      />
      <Button type="submit" size="icon" loading={rename.isPending} disabled={!trimmed} aria-label="Save name">
        {!rename.isPending && <Check className="size-4" />}
      </Button>
      <Button variant="ghost" size="icon" onClick={() => setEditing(false)} aria-label="Cancel rename">
        <X className="size-4" />
      </Button>
    </form>
  );
}

const ACTION_COPY: Record<
  DeviceAction,
  { title: string; label: string; done: string; destructive: boolean; description: string }
> = {
  approve: {
    title: 'Approve device',
    label: 'Approve',
    done: 'Device approved',
    destructive: false,
    description: 'The device will become active and can receive commands.',
  },
  disable: {
    title: 'Disable device',
    label: 'Disable',
    done: 'Device disabled',
    destructive: true,
    description: 'The agent will be disconnected and cannot reconnect until the device is enabled again.',
  },
  enable: {
    title: 'Enable device',
    label: 'Enable',
    done: 'Device enabled',
    destructive: false,
    description: 'The device will become active again and the agent may reconnect.',
  },
  revoke: {
    title: 'Revoke device',
    label: 'Revoke permanently',
    done: 'Device revoked',
    destructive: true,
    description:
      'This is permanent. The agent is disconnected, its credentials are invalidated and all queued commands are canceled. The device must re-enroll to be managed again.',
  },
};

export function DeviceDetailPage() {
  const { id = '' } = useParams();
  const device = useDevice(id);
  const canManage = useCan('devices.manage');
  const action = useDeviceAction();
  const [pending, setPending] = useState<DeviceAction | null>(null);
  const [tab, setTab] = useState<TabValue>('commands');

  if (device.isPending) return <PageLoader label="Loading device…" />;
  if (device.isError) {
    if (isApiError(device.error) && device.error.status === 404) {
      return (
        <EmptyState
          title="Device not found"
          description="It may have been removed, or the link is wrong."
          action={
            <Link to="/devices" className="text-sm font-medium text-primary hover:underline">
              Back to devices
            </Link>
          }
        />
      );
    }
    return <ErrorState error={device.error} onRetry={() => void device.refetch()} />;
  }

  const d = device.data;
  const copy = pending ? ACTION_COPY[pending] : null;

  const run = () => {
    if (!pending) return;
    const which = pending;
    action.mutate(
      { id: d.id, action: which },
      {
        onSuccess: () => {
          toast.success(ACTION_COPY[which].done, d.name);
          setPending(null);
        },
      },
    );
  };

  return (
    <>
      <Link
        to="/devices"
        className="mb-3 inline-flex items-center gap-1 text-sm text-muted hover:text-fg"
      >
        <ArrowLeft className="size-4" aria-hidden /> Devices
      </Link>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 flex-col gap-2">
          <NameEditor key={d.id + d.name} device={d} canEdit={canManage} />
          <div className="flex flex-wrap items-center gap-3 text-sm text-muted">
            <DeviceStatusBadge status={d.status} />
            <ConnectivityBadge connectivity={d.connectivity} />
            <span className="font-mono text-xs" title="Device ID">
              {d.id}
            </span>
          </div>
        </div>
        {canManage && (
          <div className="flex flex-wrap gap-2">
            {d.status === 'pending' && (
              <Button icon={<CheckCircle2 className="size-4" />} onClick={() => setPending('approve')}>
                Approve
              </Button>
            )}
            {d.status === 'active' && (
              <Button variant="outline" icon={<Ban className="size-4" />} onClick={() => setPending('disable')}>
                Disable
              </Button>
            )}
            {d.status === 'disabled' && (
              <Button variant="outline" icon={<PlayCircle className="size-4" />} onClick={() => setPending('enable')}>
                Enable
              </Button>
            )}
            {d.status !== 'revoked' && (
              <Button variant="danger" icon={<ShieldX className="size-4" />} onClick={() => setPending('revoke')}>
                Revoke
              </Button>
            )}
          </div>
        )}
      </div>

      {d.status === 'pending' && (
        <div className="mb-4 rounded-md border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-200">
          This device is waiting for approval. It cannot receive commands until it is approved.
        </div>
      )}
      {d.status === 'revoked' && (
        <div className="mb-4 rounded-md border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-900 dark:border-red-900 dark:bg-red-950/50 dark:text-red-200">
          This device has been revoked permanently.
        </div>
      )}

      <ConnectedClientsBanner deviceId={d.id} />

      <DeviceOverview device={d} />

      <Card className="mt-6 px-4 pb-4">
        <Tabs idPrefix="device" tabs={TABS} value={tab} onChange={setTab} />
        {tab === 'commands' && (
          <TabPanel idPrefix="device" value="commands">
            <CommandsTab device={d} />
          </TabPanel>
        )}
        {tab === 'phones' && (
          <TabPanel idPrefix="device" value="phones">
            <ConnectedClientsTab deviceId={d.id} />
          </TabPanel>
        )}
        {tab === 'activity' && (
          <TabPanel idPrefix="device" value="activity">
            <ActivityTab deviceId={d.id} />
          </TabPanel>
        )}
      </Card>

      {copy && (
        <ConfirmDialog
          open={!!pending}
          onClose={() => !action.isPending && setPending(null)}
          onConfirm={run}
          title={copy.title}
          description={
            <>
              <p>
                <span className="font-medium text-fg">{d.name || d.hostname}</span>
              </p>
              <p className="mt-2">{copy.description}</p>
            </>
          }
          confirmLabel={copy.label}
          destructive={copy.destructive}
          loading={action.isPending}
          confirmText={pending === 'revoke' ? d.name || d.hostname || d.id : undefined}
        />
      )}
    </>
  );
}
