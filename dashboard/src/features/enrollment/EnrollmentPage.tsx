import { useMemo, useState, type FormEvent } from 'react';
import { KeyRound, Plus, Trash2 } from 'lucide-react';
import { isApiError } from '@/api/client';
import { useConfig } from '@/api/config';
import {
  useCreateEnrollmentToken,
  useEnrollmentTokens,
  useRevokeEnrollmentToken,
} from '@/api/enrollment';
import type { CreatedEnrollmentToken, EnrollmentToken } from '@/api/types';
import { CopyField } from '@/components/copy-button';
import { PageHeader } from '@/components/page-header';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { ConfirmDialog, Dialog } from '@/components/ui/dialog';
import { EmptyState, ErrorState, LoadMore, Spinner } from '@/components/ui/feedback';
import { Checkbox, Field, Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { Table, TBody, TD, TH, THead, TR, TableMessageRow } from '@/components/ui/table';
import { RelativeTime } from '@/components/ui/time';
import { toast } from '@/components/ui/toast-store';
import { EXPIRY_OPTIONS, linuxInstallCommand, windowsInstallCommand } from './install';

function tokenState(t: EnrollmentToken): { label: string; tone: 'green' | 'gray' | 'red' | 'yellow' } {
  if (t.revoked_at) return { label: 'revoked', tone: 'red' };
  if (new Date(t.expires_at).getTime() <= Date.now()) return { label: 'expired', tone: 'gray' };
  if (t.max_uses != null && t.uses >= t.max_uses) return { label: 'used up', tone: 'yellow' };
  return { label: 'active', tone: 'green' };
}

function CreateTokenDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (t: CreatedEnrollmentToken) => void;
}) {
  const [description, setDescription] = useState('');
  const [autoApprove, setAutoApprove] = useState(false);
  const [limitUses, setLimitUses] = useState(false);
  const [maxUses, setMaxUses] = useState('10');
  const [expiry, setExpiry] = useState<string>(String(86400));
  const create = useCreateEnrollmentToken();

  const maxUsesNum = Number(maxUses);
  const maxUsesValid = !limitUses || (Number.isInteger(maxUsesNum) && maxUsesNum >= 1);
  const fieldErrors = isApiError(create.error) ? create.error.fields : undefined;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!maxUsesValid) return;
    create.mutate(
      {
        description: description.trim(),
        auto_approve: autoApprove,
        max_uses: limitUses ? maxUsesNum : null,
        expires_in_s: Number(expiry),
      },
      {
        onSuccess: (t) => {
          setDescription('');
          setAutoApprove(false);
          setLimitUses(false);
          onCreated(t);
        },
      },
    );
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Create enrollment token"
      description="Agents use this token once to register with the platform."
      dismissable={!create.isPending}
    >
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Description" error={fieldErrors?.description} hint="Where or for what this token is used.">
          {(id) => (
            <Input
              id={id}
              autoFocus
              value={description}
              maxLength={200}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="e.g. Lab servers, Q4 rollout"
            />
          )}
        </Field>
        <Field label="Expires in" error={fieldErrors?.expires_in_s}>
          {(id) => (
            <Select
              id={id}
              className="max-w-48"
              value={expiry}
              onChange={(e) => setExpiry(e.target.value)}
              options={EXPIRY_OPTIONS}
            />
          )}
        </Field>
        <div className="flex flex-col gap-2">
          <Checkbox
            label="Limit number of uses"
            checked={limitUses}
            onChange={(e) => setLimitUses(e.target.checked)}
          />
          {limitUses && (
            <Field
              label="Max uses"
              className="max-w-40"
              error={maxUsesValid ? fieldErrors?.max_uses : 'Enter a whole number ≥ 1.'}
            >
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  min={1}
                  step={1}
                  value={maxUses}
                  aria-invalid={!maxUsesValid}
                  onChange={(e) => setMaxUses(e.target.value)}
                />
              )}
            </Field>
          )}
        </div>
        <div>
          <Checkbox
            label="Auto-approve enrolled devices"
            checked={autoApprove}
            onChange={(e) => setAutoApprove(e.target.checked)}
          />
          <p className="mt-1 pl-6 text-xs text-muted">
            Devices become active immediately instead of waiting for manual approval.
          </p>
        </div>
        <div className="flex justify-end gap-2 border-t border-border pt-4">
          <Button variant="outline" onClick={onClose} disabled={create.isPending}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending} disabled={!maxUsesValid}>
            Create token
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function TokenCreatedDialog({ token, onClose }: { token: CreatedEnrollmentToken | null; onClose: () => void }) {
  const config = useConfig();
  const [withCa, setWithCa] = useState(false);
  const gateway = config.data?.gateway_url;

  return (
    <Dialog
      open={!!token}
      onClose={onClose}
      size="lg"
      title="Enrollment token created"
      footer={<Button onClick={onClose}>I have saved the token</Button>}
    >
      {token && (
        <div className="flex flex-col gap-5">
          <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-200">
            Copy this token now. <strong>It will not be shown again.</strong>
          </div>
          <CopyField label="Token" value={token.token} />
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h3 className="text-sm font-semibold">Install commands</h3>
              <Checkbox
                label="Include --ca-file (self-signed gateway CA)"
                checked={withCa}
                onChange={(e) => setWithCa(e.target.checked)}
              />
            </div>
            {config.isPending ? (
              <Spinner label="Loading gateway URL…" />
            ) : !gateway ? (
              <ErrorState
                title="Could not load the gateway URL"
                error={config.error ?? new Error('gateway_url missing from /v1/config')}
                onRetry={() => void config.refetch()}
              />
            ) : (
              <>
                <CopyField label="Linux (run as root)" value={linuxInstallCommand(gateway, token.token, withCa)} />
                <CopyField
                  label="Windows (run in an elevated PowerShell)"
                  value={windowsInstallCommand(gateway, token.token, withCa)}
                />
              </>
            )}
          </div>
        </div>
      )}
    </Dialog>
  );
}

export function EnrollmentPage() {
  const tokens = useEnrollmentTokens();
  const revoke = useRevokeEnrollmentToken();
  const [createOpen, setCreateOpen] = useState(false);
  const [created, setCreated] = useState<CreatedEnrollmentToken | null>(null);
  const [toRevoke, setToRevoke] = useState<EnrollmentToken | null>(null);
  const items = useMemo(() => tokens.data?.pages.flatMap((p) => p.items) ?? [], [tokens.data]);

  return (
    <>
      <PageHeader
        title="Enrollment tokens"
        description="Tokens let new agents register. Each token is shown only once, when it is created."
        actions={
          <Button icon={<Plus className="size-4" />} onClick={() => setCreateOpen(true)}>
            New token
          </Button>
        }
      />
      <Card>
        <Table>
          <THead>
            <tr>
              <TH>Description</TH>
              <TH>State</TH>
              <TH>Uses</TH>
              <TH>Auto-approve</TH>
              <TH>Expires</TH>
              <TH>Created</TH>
              <TH className="text-right">
                <span className="sr-only">Actions</span>
              </TH>
            </tr>
          </THead>
          <TBody>
            {tokens.isPending ? (
              <TableMessageRow colSpan={7}>
                <Spinner label="Loading tokens…" />
              </TableMessageRow>
            ) : tokens.isError ? (
              <tr>
                <td colSpan={7}>
                  <ErrorState error={tokens.error} onRetry={() => void tokens.refetch()} />
                </td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={7}>
                  <EmptyState
                    icon={<KeyRound className="size-5" />}
                    title="No enrollment tokens"
                    description="Create a token to enroll your first device."
                    action={
                      <Button size="sm" icon={<Plus className="size-4" />} onClick={() => setCreateOpen(true)}>
                        New token
                      </Button>
                    }
                  />
                </td>
              </tr>
            ) : (
              items.map((t) => {
                const st = tokenState(t);
                return (
                  <TR key={t.id}>
                    <TD className="max-w-xs">
                      <div className="truncate font-medium" title={t.description}>
                        {t.description || <span className="text-muted italic">No description</span>}
                      </div>
                      <div className="text-xs text-muted">by {t.created_by.email}</div>
                    </TD>
                    <TD>
                      <Badge tone={st.tone}>{st.label}</Badge>
                    </TD>
                    <TD className="tabular-nums">
                      {t.uses}
                      {t.max_uses != null ? ` / ${t.max_uses}` : <span className="text-muted"> / ∞</span>}
                    </TD>
                    <TD>{t.auto_approve ? <Badge tone="blue">yes</Badge> : <span className="text-muted">no</span>}</TD>
                    <TD className="text-muted">
                      <RelativeTime value={t.expires_at} />
                    </TD>
                    <TD className="text-muted">
                      <RelativeTime value={t.created_at} />
                    </TD>
                    <TD className="text-right">
                      {!t.revoked_at && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-red-600 dark:text-red-400"
                          icon={<Trash2 className="size-3.5" />}
                          onClick={() => setToRevoke(t)}
                        >
                          Revoke
                        </Button>
                      )}
                    </TD>
                  </TR>
                );
              })
            )}
          </TBody>
        </Table>
        <LoadMore
          hasNextPage={tokens.hasNextPage}
          isFetchingNextPage={tokens.isFetchingNextPage}
          fetchNextPage={tokens.fetchNextPage}
        />
      </Card>

      <CreateTokenDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(t) => {
          setCreateOpen(false);
          setCreated(t);
        }}
      />
      <TokenCreatedDialog token={created} onClose={() => setCreated(null)} />
      <ConfirmDialog
        open={!!toRevoke}
        onClose={() => setToRevoke(null)}
        onConfirm={() => {
          if (!toRevoke) return;
          revoke.mutate(toRevoke.id, {
            onSuccess: () => {
              toast.success('Token revoked');
              setToRevoke(null);
            },
          });
        }}
        title="Revoke enrollment token"
        description={
          <>
            Agents will no longer be able to enroll with{' '}
            <span className="font-medium text-fg">{toRevoke?.description || 'this token'}</span>. Devices that
            already enrolled are not affected.
          </>
        }
        confirmLabel="Revoke"
        destructive
        loading={revoke.isPending}
      />
    </>
  );
}
