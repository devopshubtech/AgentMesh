import { useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { CheckCircle2, Clock, Monitor, Search, Wifi, WifiOff, X, type LucideIcon } from 'lucide-react';
import { useDeviceAction, useDevices, useDeviceSummary } from '@/api/devices';
import type { Connectivity, Device, DeviceListParams, DeviceStatus } from '@/api/types';
import { useCan } from '@/auth/context';
import { PageHeader } from '@/components/page-header';
import { ConnectivityBadge, DeviceStatusBadge } from '@/components/status-badges';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { EmptyState, ErrorState, LoadMore, Spinner } from '@/components/ui/feedback';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { Table, TBody, TD, TH, THead, TR, TableMessageRow } from '@/components/ui/table';
import { RelativeTime } from '@/components/ui/time';
import { toast } from '@/components/ui/toast-store';
import { cn } from '@/lib/cn';
import { useDebouncedValue } from '@/lib/hooks';
import { CONNECTIVITY_OPTIONS, osLabel, PLATFORM_OPTIONS, platformLabel, STATUS_OPTIONS } from './constants';

function SummaryCard({
  label,
  value,
  icon: Icon,
  tone,
  loading,
  onClick,
  active,
}: {
  label: string;
  value: number | undefined;
  icon: LucideIcon;
  tone: string;
  loading: boolean;
  onClick: () => void;
  active: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'flex items-center gap-3 rounded-lg border bg-surface p-4 text-left shadow-sm transition-colors hover:border-ring',
        active ? 'border-primary ring-1 ring-primary' : 'border-border',
      )}
    >
      <div className={cn('rounded-md p-2', tone)}>
        <Icon className="size-5" aria-hidden />
      </div>
      <div>
        <p className="text-xs font-medium text-muted">{label}</p>
        <p className="text-2xl font-semibold text-fg tabular-nums">
          {loading ? <Spinner /> : (value ?? '—')}
        </p>
      </div>
    </button>
  );
}

export function DevicesPage() {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const canManage = useCan('devices.manage');
  const status = (params.get('status') ?? '') as DeviceStatus | '';
  const connectivity = (params.get('connectivity') ?? '') as Connectivity | '';
  const platform = params.get('platform') ?? '';
  const [search, setSearch] = useState(params.get('q') ?? '');
  const q = useDebouncedValue(search.trim(), 300);

  const setParam = (key: string, value: string) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
  };

  const listParams: DeviceListParams = useMemo(
    () => ({ status, connectivity, platform, q }),
    [status, connectivity, platform, q],
  );

  const summary = useDeviceSummary();
  const devices = useDevices(listParams);
  const action = useDeviceAction();
  const [approvingId, setApprovingId] = useState<string | null>(null);

  const items: Device[] = useMemo(
    () => devices.data?.pages.flatMap((p) => p.items) ?? [],
    [devices.data],
  );
  const filtersActive = !!(status || connectivity || platform || q);

  const approve = (d: Device) => {
    setApprovingId(d.id);
    action.mutate(
      { id: d.id, action: 'approve' },
      {
        onSuccess: () => toast.success('Device approved', d.name),
        onSettled: () => setApprovingId(null),
      },
    );
  };

  const toggleFilter = (key: 'status' | 'connectivity', value: string) => {
    const current = key === 'status' ? status : connectivity;
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('status');
        next.delete('connectivity');
        if (current !== value) next.set(key, value);
        return next;
      },
      { replace: true },
    );
  };

  const colCount = canManage ? 8 : 7;

  return (
    <>
      <PageHeader title="Devices" description="Enrolled agents, their health and inventory." />

      <div className="mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <SummaryCard
          label="Total"
          value={summary.data?.total}
          icon={Monitor}
          tone="bg-primary/10 text-primary"
          loading={summary.isPending}
          active={!status && !connectivity}
          onClick={() =>
            setParams(
              (prev) => {
                const next = new URLSearchParams(prev);
                next.delete('status');
                next.delete('connectivity');
                return next;
              },
              { replace: true },
            )
          }
        />
        <SummaryCard
          label="Online"
          value={summary.data?.online}
          icon={Wifi}
          tone="bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
          loading={summary.isPending}
          active={connectivity === 'online'}
          onClick={() => toggleFilter('connectivity', 'online')}
        />
        <SummaryCard
          label="Offline"
          value={summary.data?.offline}
          icon={WifiOff}
          tone="bg-gray-500/10 text-gray-600 dark:text-gray-400"
          loading={summary.isPending}
          active={connectivity === 'offline'}
          onClick={() => toggleFilter('connectivity', 'offline')}
        />
        <SummaryCard
          label="Pending approval"
          value={summary.data?.pending}
          icon={Clock}
          tone="bg-amber-500/10 text-amber-600 dark:text-amber-400"
          loading={summary.isPending}
          active={status === 'pending'}
          onClick={() => toggleFilter('status', 'pending')}
        />
      </div>

      <Card>
        <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
          <div className="relative min-w-52 flex-1">
            <Search
              className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted"
              aria-hidden
            />
            <Input
              type="search"
              placeholder="Search name or hostname…"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setParam('q', e.target.value.trim());
              }}
              className="pl-8"
              aria-label="Search devices"
            />
          </div>
          <Select
            aria-label="Filter by status"
            className="w-36"
            placeholder="All statuses"
            options={STATUS_OPTIONS}
            value={status}
            onChange={(e) => setParam('status', e.target.value)}
          />
          <Select
            aria-label="Filter by connectivity"
            className="w-40"
            placeholder="All connectivity"
            options={CONNECTIVITY_OPTIONS}
            value={connectivity}
            onChange={(e) => setParam('connectivity', e.target.value)}
          />
          <Select
            aria-label="Filter by platform"
            className="w-36"
            placeholder="All platforms"
            options={PLATFORM_OPTIONS}
            value={platform}
            onChange={(e) => setParam('platform', e.target.value)}
          />
          {filtersActive && (
            <Button
              variant="ghost"
              size="sm"
              icon={<X className="size-3.5" aria-hidden />}
              onClick={() => {
                setSearch('');
                setParams(new URLSearchParams(), { replace: true });
              }}
            >
              Clear
            </Button>
          )}
          {devices.isFetching && !devices.isPending && <Spinner />}
        </div>

        <Table>
          <THead>
            <tr>
              <TH>Name</TH>
              <TH>OS</TH>
              <TH>Platform</TH>
              <TH>Agent</TH>
              <TH>Status</TH>
              <TH>Connectivity</TH>
              <TH>Last seen</TH>
              {canManage && <TH className="text-right">Actions</TH>}
            </tr>
          </THead>
          <TBody>
            {devices.isPending ? (
              <TableMessageRow colSpan={colCount}>
                <Spinner label="Loading devices…" />
              </TableMessageRow>
            ) : devices.isError ? (
              <tr>
                <td colSpan={colCount}>
                  <ErrorState error={devices.error} onRetry={() => void devices.refetch()} />
                </td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={colCount}>
                  <EmptyState
                    title={filtersActive ? 'No devices match your filters' : 'No devices yet'}
                    description={
                      filtersActive
                        ? 'Try a different search or clear the filters.'
                        : 'Create an enrollment token and install the agent to register your first device.'
                    }
                    action={
                      !filtersActive ? (
                        <Link to="/enrollment" className="text-sm font-medium text-primary hover:underline">
                          Go to enrollment
                        </Link>
                      ) : undefined
                    }
                  />
                </td>
              </tr>
            ) : (
              items.map((d) => (
                <TR
                  key={d.id}
                  className={cn(
                    'cursor-pointer hover:bg-subtle/60',
                    d.status === 'pending' && 'bg-amber-50/60 dark:bg-amber-950/30',
                    d.status === 'revoked' && 'opacity-60',
                  )}
                  onClick={() => void navigate(`/devices/${encodeURIComponent(d.id)}`)}
                >
                  <TD>
                    <Link
                      to={`/devices/${encodeURIComponent(d.id)}`}
                      className="font-medium text-fg hover:text-primary hover:underline"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {d.name || d.hostname}
                    </Link>
                    {d.hostname && d.hostname !== d.name && (
                      <div className="text-xs text-muted">{d.hostname}</div>
                    )}
                  </TD>
                  <TD className="text-muted">{osLabel(d)}</TD>
                  <TD className="whitespace-nowrap text-muted">
                    {platformLabel(d.platform)}
                    {d.arch ? ` / ${d.arch}` : ''}
                  </TD>
                  <TD className="font-mono text-xs text-muted">{d.agent_version || '—'}</TD>
                  <TD>
                    <DeviceStatusBadge status={d.status} />
                  </TD>
                  <TD>
                    <ConnectivityBadge connectivity={d.connectivity} />
                  </TD>
                  <TD className="text-muted">
                    <RelativeTime value={d.last_seen_at} />
                  </TD>
                  {canManage && (
                    <TD className="text-right">
                      {d.status === 'pending' && (
                        <Button
                          size="sm"
                          icon={<CheckCircle2 className="size-3.5" aria-hidden />}
                          loading={approvingId === d.id}
                          disabled={approvingId !== null && approvingId !== d.id}
                          onClick={(e) => {
                            e.stopPropagation();
                            approve(d);
                          }}
                        >
                          Approve
                        </Button>
                      )}
                    </TD>
                  )}
                </TR>
              ))
            )}
          </TBody>
        </Table>
        <LoadMore
          hasNextPage={devices.hasNextPage}
          isFetchingNextPage={devices.isFetchingNextPage}
          fetchNextPage={devices.fetchNextPage}
        />
      </Card>
    </>
  );
}
