import { useMemo, useState } from 'react';
import { Ban, Eye } from 'lucide-react';
import { useCancelCommand, useDeviceCommands } from '@/api/commands';
import { isTerminalStatus, type Command, type Device } from '@/api/types';
import { useCan, usePermissions } from '@/auth/context';
import { CommandStatusBadge } from '@/components/status-badges';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { EmptyState, ErrorState, LoadMore, Spinner } from '@/components/ui/feedback';
import { Table, TBody, TD, TH, THead, TR, TableMessageRow } from '@/components/ui/table';
import { RelativeTime } from '@/components/ui/time';
import { toast } from '@/components/ui/toast-store';
import { commandDurationMs, formatDurationMs } from '@/lib/format';
import { CommandDetailDialog } from './CommandDetailDialog';
import { RunCommandForm } from './RunCommandForm';
import { cancelPermission, commandSummary } from './utils';

export function CommandsTab({ device }: { device: Device }) {
  const canRead = useCan('commands.read');
  const can = usePermissions();
  const canRun = can('commands.execute.action') || can('commands.execute.exec');
  const commands = useDeviceCommands(device.id, canRead);
  const cancel = useCancelCommand();
  const [selected, setSelected] = useState<Command | null>(null);
  const [cancelingId, setCancelingId] = useState<string | null>(null);

  const items = useMemo(() => commands.data?.pages.flatMap((p) => p.items) ?? [], [commands.data]);

  const doCancel = (cmd: Command) => {
    setCancelingId(cmd.id);
    cancel.mutate(cmd.id, {
      onSuccess: () => toast.success('Cancellation requested'),
      onSettled: () => setCancelingId(null),
    });
  };

  return (
    <div className="flex flex-col gap-6">
      {canRun && device.status !== 'revoked' && (
        <RunCommandForm device={device} onCreated={(cmd) => setSelected(cmd)} />
      )}

      <div>
        <div className="mb-2 flex items-center gap-2">
          <h3 className="text-sm font-semibold">History</h3>
          {commands.isFetching && !commands.isPending && <Spinner />}
        </div>
        {!canRead ? (
          <p className="text-sm text-muted">You do not have permission to view command history.</p>
        ) : (
          <div className="-mx-4 border-t border-border">
            <Table>
              <THead>
                <tr>
                  <TH>Command</TH>
                  <TH>Status</TH>
                  <TH>Requested by</TH>
                  <TH>Created</TH>
                  <TH>Duration</TH>
                  <TH className="text-right">
                    <span className="sr-only">Actions</span>
                  </TH>
                </tr>
              </THead>
              <TBody>
                {commands.isPending ? (
                  <TableMessageRow colSpan={6}>
                    <Spinner label="Loading commands…" />
                  </TableMessageRow>
                ) : commands.isError ? (
                  <tr>
                    <td colSpan={6}>
                      <ErrorState error={commands.error} onRetry={() => void commands.refetch()} />
                    </td>
                  </tr>
                ) : items.length === 0 ? (
                  <tr>
                    <td colSpan={6}>
                      <EmptyState title="No commands yet" description="Commands sent to this device will show up here." />
                    </td>
                  </tr>
                ) : (
                  items.map((c) => {
                    const terminal = isTerminalStatus(c.status);
                    return (
                      <TR key={c.id} className="cursor-pointer hover:bg-subtle/50" onClick={() => setSelected(c)}>
                        <TD className="max-w-md">
                          <div className="flex items-center gap-2">
                            <Badge tone={c.kind === 'exec' ? 'purple' : 'blue'}>{c.kind}</Badge>
                            <code className="truncate font-mono text-xs" title={commandSummary(c)}>
                              {commandSummary(c)}
                            </code>
                          </div>
                        </TD>
                        <TD>
                          <CommandStatusBadge status={c.status} />
                        </TD>
                        <TD className="max-w-48 truncate text-muted" title={c.requested_by.email}>
                          {c.requested_by.email}
                        </TD>
                        <TD className="text-muted">
                          <RelativeTime value={c.created_at} />
                        </TD>
                        <TD className="text-muted tabular-nums">
                          {formatDurationMs(commandDurationMs(c))}
                        </TD>
                        <TD className="text-right whitespace-nowrap">
                          <Button
                            variant="ghost"
                            size="sm"
                            icon={<Eye className="size-3.5" />}
                            onClick={(e) => {
                              e.stopPropagation();
                              setSelected(c);
                            }}
                          >
                            View
                          </Button>
                          {!terminal && can(cancelPermission(c)) && (
                            <Button
                              variant="ghost"
                              size="sm"
                              className="text-red-600 dark:text-red-400"
                              icon={<Ban className="size-3.5" />}
                              loading={cancelingId === c.id}
                              onClick={(e) => {
                                e.stopPropagation();
                                doCancel(c);
                              }}
                            >
                              Cancel
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
              hasNextPage={commands.hasNextPage}
              isFetchingNextPage={commands.isFetchingNextPage}
              fetchNextPage={commands.fetchNextPage}
            />
          </div>
        )}
      </div>

      <CommandDetailDialog
        commandId={selected?.id ?? null}
        initial={selected ?? undefined}
        onClose={() => setSelected(null)}
      />
    </div>
  );
}
