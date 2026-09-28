import { useMemo, useState } from 'react';
import { ShieldAlert, ShieldCheck, X } from 'lucide-react';
import { useAuditLog, useVerifyAudit } from '@/api/audit';
import type { AuditListParams, AuditOutcome } from '@/api/types';
import { useCan } from '@/auth/context';
import { PageHeader } from '@/components/page-header';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { EmptyState, ErrorState, LoadMore, Spinner } from '@/components/ui/feedback';
import { Field, Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { TableMessageRow } from '@/components/ui/table';
import { useDebouncedValue } from '@/lib/hooks';
import { AuditTable } from './AuditTable';

const OUTCOME_OPTIONS = [
  { value: 'success', label: 'Success' },
  { value: 'denied', label: 'Denied' },
  { value: 'error', label: 'Error' },
];

const QUICK_PREFIXES = ['auth.', 'device.', 'command.', 'enrollment.', 'user.'];

/** Converts an <input type="datetime-local"> value (local time) to RFC 3339 UTC. */
function localToIso(v: string): string | undefined {
  if (!v) return undefined;
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
}

/** The API matches a prefix when the filter ends with "*". */
function toActionFilter(v: string): string | undefined {
  const t = v.trim();
  if (!t) return undefined;
  return t.endsWith('*') ? t : `${t}*`;
}

function VerifyChain() {
  const verify = useVerifyAudit();
  const r = verify.data;
  return (
    <div className="flex flex-wrap items-center gap-3">
      {r && (
        <span
          role="status"
          className={
            r.ok
              ? 'inline-flex items-center gap-1.5 text-sm text-emerald-700 dark:text-emerald-400'
              : 'inline-flex items-center gap-1.5 text-sm font-medium text-red-700 dark:text-red-400'
          }
        >
          {r.ok ? <ShieldCheck className="size-4" /> : <ShieldAlert className="size-4" />}
          {r.ok
            ? `Chain intact · ${r.checked.toLocaleString()} entries checked`
            : `Chain broken at entry #${r.first_bad_id ?? '?'} (${r.checked.toLocaleString()} checked)`}
        </span>
      )}
      <Button
        variant="outline"
        loading={verify.isPending}
        icon={<ShieldCheck className="size-4" />}
        onClick={() => verify.mutate()}
      >
        Verify chain
      </Button>
    </div>
  );
}

export function AuditPage() {
  const isAdmin = useCan('platform.admin');
  const [action, setAction] = useState('');
  const [outcome, setOutcome] = useState<AuditOutcome | ''>('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const debouncedAction = useDebouncedValue(action, 350);

  const params: AuditListParams = useMemo(
    () => ({
      action: toActionFilter(debouncedAction),
      outcome: outcome || undefined,
      from: localToIso(from),
      to: localToIso(to),
    }),
    [debouncedAction, outcome, from, to],
  );
  const log = useAuditLog(params);
  const entries = useMemo(() => log.data?.pages.flatMap((p) => p.items) ?? [], [log.data]);
  const filtersActive = !!(action || outcome || from || to);
  const rangeInvalid = !!(from && to && new Date(from) > new Date(to));

  return (
    <>
      <PageHeader
        title="Audit log"
        description="Tamper-evident record of every security-relevant action."
        actions={isAdmin ? <VerifyChain /> : undefined}
      />
      <Card>
        <div className="flex flex-wrap items-end gap-3 border-b border-border p-3">
          <Field label="Action prefix" className="min-w-52 flex-1">
            {(id) => (
              <Input
                id={id}
                value={action}
                onChange={(e) => setAction(e.target.value)}
                placeholder="e.g. device. or command.create"
                list="audit-prefixes"
                spellCheck={false}
                className="font-mono"
              />
            )}
          </Field>
          <datalist id="audit-prefixes">
            {QUICK_PREFIXES.map((p) => (
              <option key={p} value={p} />
            ))}
          </datalist>
          <Field label="Outcome" className="w-36">
            {(id) => (
              <Select
                id={id}
                placeholder="Any"
                options={OUTCOME_OPTIONS}
                value={outcome}
                onChange={(e) => setOutcome(e.target.value as AuditOutcome | '')}
              />
            )}
          </Field>
          <Field label="From" className="w-52">
            {(id) => (
              <Input id={id} type="datetime-local" value={from} onChange={(e) => setFrom(e.target.value)} />
            )}
          </Field>
          <Field label="To" className="w-52" error={rangeInvalid ? '"To" is before "From".' : null}>
            {(id) => (
              <Input
                id={id}
                type="datetime-local"
                value={to}
                aria-invalid={rangeInvalid}
                onChange={(e) => setTo(e.target.value)}
              />
            )}
          </Field>
          {filtersActive && (
            <Button
              variant="ghost"
              size="sm"
              className="mb-0.5"
              icon={<X className="size-3.5" />}
              onClick={() => {
                setAction('');
                setOutcome('');
                setFrom('');
                setTo('');
              }}
            >
              Clear
            </Button>
          )}
          {log.isFetching && !log.isPending && <Spinner className="mb-2" />}
        </div>

        {log.isPending ? (
          <div className="py-10 text-center">
            <Spinner label="Loading audit log…" />
          </div>
        ) : log.isError ? (
          <ErrorState error={log.error} onRetry={() => void log.refetch()} />
        ) : (
          <AuditTable
            entries={entries}
            emptyRow={
              <TableMessageRow colSpan={6}>
                <EmptyState
                  title={filtersActive ? 'No entries match your filters' : 'No audit entries yet'}
                />
              </TableMessageRow>
            }
          />
        )}
        <LoadMore
          hasNextPage={log.hasNextPage}
          isFetchingNextPage={log.isFetchingNextPage}
          fetchNextPage={log.fetchNextPage}
        />
      </Card>
    </>
  );
}
