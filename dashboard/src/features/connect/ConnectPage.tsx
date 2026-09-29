import { useEffect, useMemo, useState, type FormEvent } from 'react';
import QRCode from 'qrcode';
import { Link2, QrCode, Smartphone, Trash2 } from 'lucide-react';
import { useConnectKeys, useCreateConnectKey, useRevokeConnectKey, type CreatedConnectKey } from '@/api/connect';
import { useDevices } from '@/api/devices';
import { useDeviceSessions, useTerminateSession } from '@/api/sessions';
import { useCan } from '@/auth/context';
import { CopyButton } from '@/components/copy-button';
import { PageHeader } from '@/components/page-header';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ui/dialog';
import { EmptyState, ErrorState, Spinner } from '@/components/ui/feedback';
import { Field, Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table';
import { RelativeTime } from '@/components/ui/time';
import { toast } from '@/components/ui/toast-store';

const APK_URL = 'https://github.com/devopshubtech/AgentMesh/releases/latest/download/agentmesh-control.apk';

const EXPIRY = [
  { value: '0', label: 'Never expires' },
  { value: String(86400), label: '1 day' },
  { value: String(7 * 86400), label: '7 days' },
  { value: String(30 * 86400), label: '30 days' },
];

function QrImage({ text }: { text: string }) {
  const [src, setSrc] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    void QRCode.toDataURL(text, { width: 320, margin: 1, errorCorrectionLevel: 'M' }).then((u) => alive && setSrc(u));
    return () => {
      alive = false;
    };
  }, [text]);
  return src ? (
    <img src={src} alt="Connect QR code" className="size-72 rounded-md border border-border bg-white p-2" />
  ) : (
    <Spinner label="Generating QR…" />
  );
}

/** Phones using the device right now (updates live via SSE + polling). */
function ConnectedPhones({ deviceId, deviceName }: { deviceId: string; deviceName: string }) {
  const sessions = useDeviceSessions(deviceId);
  const terminate = useTerminateSession();
  const live = (sessions.data?.items ?? []).filter((s) => s.status !== 'ended');
  return (
    <Card className="mt-6">
      <div className="flex items-center gap-2 px-4 pt-4 font-semibold">
        <Smartphone className="size-4" aria-hidden /> Phones connected to {deviceName} now
        <Badge tone={live.length ? 'green' : 'gray'}>{live.length}</Badge>
      </div>
      {live.length === 0 ? (
        <div className="p-4 text-sm text-muted">No phone is connected right now.</div>
      ) : (
        <Table>
          <THead>
            <tr>
              <TH>Phone</TH>
              <TH>Phone's IP</TH>
              <TH>Via link</TH>
              <TH>Status</TH>
              <TH>Connected</TH>
              <TH />
            </tr>
          </THead>
          <TBody>
            {live.map((s) => (
              <TR key={s.id}>
                <TD className="font-medium">{s.client_label || 'Unknown phone'}</TD>
                <TD className="font-mono text-xs">{s.client_ip || '—'}</TD>
                <TD>{s.connect_key_label ?? (s.user.email ? `signed in: ${s.user.email}` : '—')}</TD>
                <TD>
                  <Badge tone={s.status === 'active' ? 'green' : 'yellow'}>{s.status === 'active' ? 'connected' : 'connecting'}</Badge>
                </TD>
                <TD>
                  <RelativeTime value={s.started_at ?? s.created_at} />
                </TD>
                <TD className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    loading={terminate.isPending && terminate.variables === s.id}
                    onClick={() => terminate.mutate(s.id, { onSuccess: () => toast.success('Phone disconnected', s.client_label) })}
                  >
                    Disconnect
                  </Button>
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
      )}
    </Card>
  );
}

/**
 * The one thing most people need: pick the device whose internet the phones
 * should use, create a link, and let phones scan the QR code. No accounts on
 * the phones; the same link works for many people until it is revoked.
 */
export function ConnectPage() {
  const devices = useDevices({ status: 'active', limit: 200 });
  const exitDevices = useMemo(
    () => (devices.data?.pages.flatMap((p) => p.items) ?? []).filter((d) => d.capabilities.includes('exit_node')),
    [devices.data],
  );
  const [deviceId, setDeviceId] = useState('');
  const selected = exitDevices.find((d) => d.id === deviceId) ?? exitDevices.find((d) => d.connectivity === 'online') ?? exitDevices[0];
  const keys = useConnectKeys(selected?.id);
  const create = useCreateConnectKey();
  const revoke = useRevokeConnectKey(selected?.id ?? '');
  const canManage = useCan('devices.manage');
  const [label, setLabel] = useState('');
  const [expiry, setExpiry] = useState('0');
  const [created, setCreated] = useState<CreatedConnectKey | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!selected) return;
    create.mutate(
      { deviceId: selected.id, label: label.trim() || 'Phone link', expiresInS: Number(expiry) },
      { onSuccess: (k) => setCreated(k) },
    );
  };

  if (devices.isPending) return <Spinner label="Loading devices…" />;
  if (devices.isError) return <ErrorState error={devices.error} onRetry={() => void devices.refetch()} />;

  return (
    <>
      <PageHeader
        title="Connect a phone"
        description="Phones that scan this QR code use this device's internet connection (and its IP address), from any network. No login needed on the phone."
      />

      {exitDevices.length === 0 ? (
        <EmptyState
          icon={<Smartphone className="size-5" />}
          title="No device allows phone connections yet"
          description="Install the agent with --enable-exit-node (or set allow_exit_node in its policy), then come back."
        />
      ) : (
        <div className="grid gap-6 lg:grid-cols-2">
          <Card className="p-5">
            <form onSubmit={submit} className="flex flex-col gap-4">
              <Field label="Use the internet of">
                {(id) => (
                  <Select
                    id={id}
                    value={selected?.id ?? ''}
                    onChange={(e) => {
                      setDeviceId(e.target.value);
                      setCreated(null);
                    }}
                    options={exitDevices.map((d) => ({
                      value: d.id,
                      label: `${d.name} (${d.connectivity}${d.last_ip ? ', ' + d.last_ip : ''})`,
                    }))}
                  />
                )}
              </Field>
              {selected && selected.connectivity !== 'online' && (
                <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:bg-amber-950/50 dark:text-amber-200">
                  {selected.name} is offline. Phones can connect once it is back online.
                </p>
              )}
              <Field label="Name for this link" hint="For your reference, e.g. Family, Office, Ramesh's phone.">
                {(id) => <Input id={id} value={label} maxLength={100} onChange={(e) => setLabel(e.target.value)} placeholder="Phone link" />}
              </Field>
              <Field label="Valid for">
                {(id) => <Select id={id} value={expiry} onChange={(e) => setExpiry(e.target.value)} options={EXPIRY} />}
              </Field>
              <div>
                <Button type="submit" icon={<QrCode className="size-4" />} loading={create.isPending} disabled={!selected || !canManage}>
                  Create connect QR code
                </Button>
              </div>
            </form>
          </Card>

          <Card className="flex flex-col items-center gap-4 p-5 text-center">
            {created ? (
              <>
                <QrImage text={created.link} />
                <div className="text-sm">
                  <div className="font-semibold">Scan with the phone camera</div>
                  <div className="text-muted">
                    or share the link below. It opens the AgentMesh app and connects to{' '}
                    <span className="font-medium text-fg">{selected?.name}</span>.
                  </div>
                </div>
                <div className="flex w-full items-center gap-2">
                  <Input readOnly value={created.link} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
                  <CopyButton text={created.link} label="Copy link" />
                </div>
                <p className="text-xs text-muted">
                  Shown once — create a new one if you lose it. Anyone with this link can use {selected?.name}'s internet
                  until you revoke it. No app yet?{' '}
                  <a className="text-primary hover:underline" href={APK_URL}>
                    Download the Android app
                  </a>
                  .
                </p>
              </>
            ) : (
              <div className="flex flex-1 flex-col items-center justify-center gap-2 py-10 text-muted">
                <QrCode className="size-10" aria-hidden />
                <div className="text-sm">Create a link to show its QR code here.</div>
              </div>
            )}
          </Card>
        </div>
      )}

      {selected && <ConnectedPhones deviceId={selected.id} deviceName={selected.name} />}

      {selected && (
        <Card className="mt-6">
          <div className="flex items-center gap-2 px-4 pt-4 font-semibold">
            <Link2 className="size-4" aria-hidden /> Links for {selected.name}
          </div>
          {keys.isPending ? (
            <div className="p-4">
              <Spinner label="Loading links…" />
            </div>
          ) : keys.isError ? (
            <ErrorState error={keys.error} onRetry={() => void keys.refetch()} />
          ) : keys.data.items.length === 0 ? (
            <div className="p-4 text-sm text-muted">No links yet.</div>
          ) : (
            <Table>
              <THead>
                <tr>
                  <TH>Name</TH>
                  <TH>Status</TH>
                  <TH>Phones now</TH>
                  <TH>Times used</TH>
                  <TH>Last used</TH>
                  <TH>Created</TH>
                  <TH />
                </tr>
              </THead>
              <TBody>
                {keys.data.items.map((k) => {
                  const expired = !!k.expires_at && new Date(k.expires_at) < new Date();
                  const status = k.revoked_at ? 'revoked' : expired ? 'expired' : 'active';
                  return (
                    <TR key={k.id} className={status !== 'active' ? 'opacity-60' : undefined}>
                      <TD className="font-medium">{k.label}</TD>
                      <TD>
                        <Badge tone={status === 'active' ? 'green' : 'gray'}>{status}</Badge>
                      </TD>
                      <TD>{k.active_sessions}</TD>
                      <TD>{k.uses}</TD>
                      <TD>
                        <RelativeTime value={k.last_used_at} />
                      </TD>
                      <TD>
                        <RelativeTime value={k.created_at} />
                      </TD>
                      <TD className="text-right">
                        {status === 'active' && canManage && (
                          <Button variant="ghost" size="sm" icon={<Trash2 className="size-3.5" />} onClick={() => setRevoking(k.id)}>
                            Revoke
                          </Button>
                        )}
                      </TD>
                    </TR>
                  );
                })}
              </TBody>
            </Table>
          )}
        </Card>
      )}

      {revoking && (
        <ConfirmDialog
          open
          destructive
          title="Revoke this link?"
          description="Phones using it are disconnected immediately and cannot connect with it again."
          confirmLabel="Revoke link"
          loading={revoke.isPending}
          onClose={() => !revoke.isPending && setRevoking(null)}
          onConfirm={() =>
            revoke.mutate(revoking, {
              onSuccess: () => {
                toast.success('Link revoked');
                setRevoking(null);
              },
            })
          }
        />
      )}
    </>
  );
}
