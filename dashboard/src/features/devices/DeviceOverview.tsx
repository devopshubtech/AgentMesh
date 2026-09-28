import type { ReactNode } from 'react';
import {
  Activity,
  Clock,
  Cpu,
  HardDrive,
  MemoryStick,
  MonitorSmartphone,
  Network,
  Puzzle,
} from 'lucide-react';
import type { Device } from '@/api/types';
import { Badge } from '@/components/ui/badge';
import { Card, CardBody, CardHeader } from '@/components/ui/card';
import { UsageBar } from '@/components/ui/feedback';
import { RelativeTime } from '@/components/ui/time';
import { formatBytes, formatDateTime, formatDurationSeconds, percent } from '@/lib/format';
import { osLabel, platformLabel } from './constants';

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1 text-sm">
      <dt className="shrink-0 text-muted">{label}</dt>
      <dd className="min-w-0 text-right break-words text-fg">{children}</dd>
    </div>
  );
}

function NoInventory() {
  return <p className="text-sm text-muted">Not reported yet.</p>;
}

export function DeviceOverview({ device }: { device: Device }) {
  const inv = device.inventory;
  const mem = inv?.memory;
  const memPct = mem ? percent(mem.used_bytes, mem.total_bytes) : 0;

  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      <Card>
        <CardHeader title="Operating system" icon={<MonitorSmartphone className="size-4" />} />
        <CardBody>
          <dl>
            <Row label="OS">{osLabel(device)}</Row>
            {device.os_build && <Row label="Build">{device.os_build}</Row>}
            <Row label="Kernel">
              <span className="font-mono text-xs">{device.kernel_version || '—'}</span>
            </Row>
            <Row label="Platform">
              {platformLabel(device.platform)} / {device.arch || '—'}
            </Row>
            <Row label="Hostname">{device.hostname || '—'}</Row>
          </dl>
        </CardBody>
      </Card>

      <Card>
        <CardHeader title="Agent & connectivity" icon={<Activity className="size-4" />} />
        <CardBody>
          <dl>
            <Row label="Agent version">
              <span className="font-mono text-xs">{device.agent_version || '—'}</span>
            </Row>
            <Row label="Last heartbeat">
              <RelativeTime value={device.last_seen_at} />
            </Row>
            <Row label="Last IP">
              <span className="font-mono text-xs">{device.last_ip ?? '—'}</span>
            </Row>
            <Row label="Enrolled">{formatDateTime(device.created_at)}</Row>
            <Row label="Approved">{device.approved_at ? formatDateTime(device.approved_at) : '—'}</Row>
          </dl>
        </CardBody>
      </Card>

      <Card>
        <CardHeader title="Uptime" icon={<Clock className="size-4" />} />
        <CardBody>
          {inv ? (
            <dl>
              <Row label="Uptime">{formatDurationSeconds(inv.uptime_s)}</Row>
              <Row label="Boot time">{inv.boot_time ? formatDateTime(inv.boot_time) : '—'}</Row>
              <Row label="Inventory collected">
                <RelativeTime value={inv.collected_at} />
              </Row>
            </dl>
          ) : (
            <NoInventory />
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader title="CPU" icon={<Cpu className="size-4" />} />
        <CardBody>
          {inv?.cpu ? (
            <dl>
              <Row label="Model">{inv.cpu.model || '—'}</Row>
              <Row label="Cores / threads">
                {inv.cpu.cores} / {inv.cpu.threads}
              </Row>
            </dl>
          ) : (
            <NoInventory />
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader title="Memory" icon={<MemoryStick className="size-4" />} />
        <CardBody>
          {mem ? (
            <div className="flex flex-col gap-2">
              <div className="flex items-baseline justify-between text-sm">
                <span className="text-fg">
                  {formatBytes(mem.used_bytes)} <span className="text-muted">of</span>{' '}
                  {formatBytes(mem.total_bytes)}
                </span>
                <span className="text-muted tabular-nums">{memPct.toFixed(0)}%</span>
              </div>
              <UsageBar value={memPct} />
            </div>
          ) : (
            <NoInventory />
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader title="Capabilities" icon={<Puzzle className="size-4" />} />
        <CardBody>
          {device.capabilities.length ? (
            <div className="flex flex-wrap gap-1.5">
              {device.capabilities.map((c) => (
                <Badge key={c} tone={c === 'exec' ? 'purple' : 'neutral'} className="font-mono">
                  {c}
                </Badge>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted">No capabilities declared.</p>
          )}
        </CardBody>
      </Card>

      <Card className="md:col-span-2 xl:col-span-1">
        <CardHeader title="Storage" icon={<HardDrive className="size-4" />} />
        <CardBody>
          {inv?.disks?.length ? (
            <ul className="flex flex-col gap-3">
              {inv.disks.map((d, i) => {
                const p = percent(d.used_bytes, d.total_bytes);
                return (
                  <li key={`${d.mount}-${i}`} className="flex flex-col gap-1.5">
                    <div className="flex items-baseline justify-between gap-2 text-sm">
                      <span className="min-w-0 truncate font-mono text-xs" title={d.mount}>
                        {d.mount}{' '}
                        <span className="text-muted">({d.fstype || 'unknown'})</span>
                      </span>
                      <span className="shrink-0 text-xs text-muted tabular-nums">
                        {formatBytes(d.used_bytes)} / {formatBytes(d.total_bytes)} · {p.toFixed(0)}%
                      </span>
                    </div>
                    <UsageBar value={p} />
                  </li>
                );
              })}
            </ul>
          ) : (
            <NoInventory />
          )}
        </CardBody>
      </Card>

      <Card className="md:col-span-2">
        <CardHeader title="Network" icon={<Network className="size-4" />} />
        <CardBody>
          {inv?.network?.length ? (
            <ul className="divide-y divide-border">
              {inv.network.map((n, i) => (
                <li key={`${n.name}-${i}`} className="flex flex-wrap items-start gap-x-4 gap-y-1 py-2 first:pt-0 last:pb-0">
                  <div className="w-40 min-w-0">
                    <p className="truncate text-sm font-medium" title={n.name}>
                      {n.name}
                    </p>
                    <p className="font-mono text-xs text-muted">{n.mac || '—'}</p>
                  </div>
                  <div className="flex min-w-0 flex-1 flex-wrap gap-1.5">
                    {n.addrs.length ? (
                      n.addrs.map((a) => (
                        <Badge key={a} tone="neutral" className="font-mono">
                          {a}
                        </Badge>
                      ))
                    ) : (
                      <span className="text-xs text-muted">No addresses</span>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <NoInventory />
          )}
        </CardBody>
      </Card>
    </div>
  );
}
