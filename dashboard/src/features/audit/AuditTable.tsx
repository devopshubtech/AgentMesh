import { Fragment, useState, type ReactNode } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import type { AuditEntry } from '@/api/types';
import { OutcomeBadge } from '@/components/status-badges';
import { Badge } from '@/components/ui/badge';
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table';
import { formatDateTime } from '@/lib/format';
import { RelativeTime } from '@/components/ui/time';

function safeJson(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

function hasDetails(d: AuditEntry['details']): boolean {
  return !!d && typeof d === 'object' && Object.keys(d).length > 0;
}

function actorText(e: AuditEntry): string {
  return e.actor_label || e.actor_id || e.actor_type;
}

/** Audit entries table with expandable details. All values are rendered as text. */
export function AuditTable({
  entries,
  showTarget = true,
  emptyRow,
}: {
  entries: AuditEntry[];
  showTarget?: boolean;
  emptyRow?: ReactNode;
}) {
  const [expanded, setExpanded] = useState<Set<number>>(() => new Set());
  const toggle = (id: number) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const cols = showTarget ? 6 : 5;

  return (
    <Table>
      <THead>
        <tr>
          <TH className="w-8">
            <span className="sr-only">Expand</span>
          </TH>
          <TH>Time</TH>
          <TH>Actor</TH>
          <TH>Action</TH>
          {showTarget && <TH>Target</TH>}
          <TH>Outcome</TH>
        </tr>
      </THead>
      <TBody>
        {entries.length === 0 && emptyRow}
        {entries.map((e) => {
          const open = expanded.has(e.id);
          const details = hasDetails(e.details);
          return (
            <Fragment key={e.id}>
              <TR className="hover:bg-subtle/50">
                <TD className="pr-0">
                  <button
                    type="button"
                    onClick={() => toggle(e.id)}
                    className="rounded p-0.5 text-muted hover:bg-subtle hover:text-fg"
                    aria-expanded={open}
                    aria-label={open ? 'Collapse details' : 'Expand details'}
                  >
                    {open ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                  </button>
                </TD>
                <TD className="whitespace-nowrap text-muted">
                  <RelativeTime value={e.ts} />
                </TD>
                <TD>
                  <div className="flex items-center gap-1.5">
                    <Badge tone="gray">{e.actor_type}</Badge>
                    <span className="max-w-56 truncate" title={actorText(e)}>
                      {actorText(e)}
                    </span>
                  </div>
                  {e.actor_ip && <div className="font-mono text-xs text-muted">{e.actor_ip}</div>}
                </TD>
                <TD className="font-mono text-xs">{e.action}</TD>
                {showTarget && (
                  <TD className="text-xs text-muted">
                    {e.target_type ? (
                      <>
                        <span>{e.target_type}</span>
                        {e.target_id && (
                          <div className="max-w-56 truncate font-mono" title={e.target_id}>
                            {e.target_id}
                          </div>
                        )}
                      </>
                    ) : (
                      '—'
                    )}
                  </TD>
                )}
                <TD>
                  <OutcomeBadge outcome={e.outcome} />
                </TD>
              </TR>
              {open && (
                <tr className="bg-subtle/40">
                  <td colSpan={cols} className="px-4 py-3">
                    <dl className="mb-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
                      <dt className="text-muted">Timestamp</dt>
                      <dd className="font-mono">{formatDateTime(e.ts)} ({e.ts})</dd>
                      <dt className="text-muted">Entry ID</dt>
                      <dd className="font-mono">{e.id}</dd>
                      {e.request_id && (
                        <>
                          <dt className="text-muted">Request ID</dt>
                          <dd className="font-mono select-all">{e.request_id}</dd>
                        </>
                      )}
                      {e.actor_id && (
                        <>
                          <dt className="text-muted">Actor ID</dt>
                          <dd className="font-mono select-all">{e.actor_id}</dd>
                        </>
                      )}
                    </dl>
                    {details ? (
                      <pre className="max-h-80 overflow-auto rounded-md bg-code-bg p-3 font-mono text-xs text-code-fg">
                        {safeJson(e.details)}
                      </pre>
                    ) : (
                      <p className="text-xs text-muted">No additional details.</p>
                    )}
                  </td>
                </tr>
              )}
            </Fragment>
          );
        })}
      </TBody>
    </Table>
  );
}
