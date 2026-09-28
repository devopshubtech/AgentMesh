import { useEffect, useRef, type ReactNode } from 'react';
import { AlertTriangle, Ban } from 'lucide-react';
import { useCancelCommand, useCommand } from '@/api/commands';
import { isTerminalStatus, type Command } from '@/api/types';
import { useCan } from '@/auth/context';
import { CommandStatusBadge } from '@/components/status-badges';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { ErrorState, Spinner } from '@/components/ui/feedback';
import { toast } from '@/components/ui/toast-store';
import { cn } from '@/lib/cn';
import { commandDurationMs, formatDateTime, formatDurationMs } from '@/lib/format';
import { useCommandOutput, type OutputChunk } from './outputStore';
import { cancelPermission, commandSummary } from './utils';

function Meta({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="truncate text-sm">{children}</dd>
    </div>
  );
}

function LiveOutput({ chunks, running }: { chunks: readonly OutputChunk[]; running: boolean }) {
  const ref = useRef<HTMLPreElement>(null);
  const stick = useRef(true);

  useEffect(() => {
    const el = ref.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [chunks]);

  return (
    <div>
      <div className="mb-1.5 flex items-center gap-2 text-xs font-medium text-muted">
        Live output
        {running && <Spinner />}
      </div>
      <pre
        ref={ref}
        onScroll={(e) => {
          const el = e.currentTarget;
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }}
        className="max-h-96 min-h-24 overflow-auto rounded-md bg-code-bg p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap text-code-fg"
        aria-live="polite"
      >
        {chunks.length === 0 ? (
          <span className="opacity-60">{running ? 'Waiting for output…' : 'No output was streamed.'}</span>
        ) : (
          chunks.map((c) => (
            <span
              key={`${c.stream}-${c.seq}`}
              className={c.stream === 'stderr' ? 'text-red-300' : undefined}
            >
              {c.data}
            </span>
          ))
        )}
      </pre>
    </div>
  );
}

function OutputBlock({ label, text, stderr }: { label: string; text: string; stderr?: boolean }) {
  return (
    <div>
      <div className="mb-1.5 flex items-center gap-2 text-xs font-medium text-muted">
        <span
          className={cn('size-2 rounded-full', stderr ? 'bg-red-500' : 'bg-emerald-500')}
          aria-hidden
        />
        {label}
        <span className="font-normal">({text.length.toLocaleString()} chars)</span>
      </div>
      <pre
        className={cn(
          'max-h-96 overflow-auto rounded-md bg-code-bg p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap text-code-fg',
          stderr && 'border-l-2 border-red-500 text-red-200',
        )}
      >
        {text || <span className="opacity-60">(empty)</span>}
      </pre>
    </div>
  );
}

export function CommandDetailDialog({
  commandId,
  initial,
  onClose,
}: {
  commandId: string | null;
  initial?: Command;
  onClose: () => void;
}) {
  const q = useCommand(commandId);
  const cmd = q.data ?? initial;
  const chunks = useCommandOutput(commandId);
  const cancel = useCancelCommand();
  const canCancel = useCan(cmd ? cancelPermission(cmd) : 'commands.execute.exec');

  const terminal = cmd ? isTerminalStatus(cmd.status) : false;
  const result = cmd?.result ?? null;

  return (
    <Dialog
      open={!!commandId}
      onClose={onClose}
      size="xl"
      title={
        <span className="flex items-center gap-2">
          Command
          {cmd && <CommandStatusBadge status={cmd.status} />}
        </span>
      }
      description={
        cmd ? (
          <code className="block truncate font-mono text-xs" title={commandSummary(cmd)}>
            {commandSummary(cmd)}
          </code>
        ) : undefined
      }
      footer={
        <>
          {cmd && !terminal && canCancel && (
            <Button
              variant="danger"
              icon={<Ban className="size-4" />}
              loading={cancel.isPending}
              onClick={() =>
                cancel.mutate(cmd.id, { onSuccess: () => toast.success('Cancellation requested') })
              }
            >
              Cancel command
            </Button>
          )}
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
        </>
      }
    >
      {!cmd ? (
        q.isError ? (
          <ErrorState error={q.error} onRetry={() => void q.refetch()} />
        ) : (
          <Spinner label="Loading command…" />
        )
      ) : (
        <div className="flex flex-col gap-5">
          <dl className="grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4">
            <Meta label="Kind">
              <Badge tone={cmd.kind === 'exec' ? 'purple' : 'blue'}>{cmd.kind}</Badge>
              {cmd.shell && (
                <Badge tone="gray" className="ml-1">
                  shell
                </Badge>
              )}
            </Meta>
            <Meta label="Requested by">
              <span title={cmd.requested_by.email}>{cmd.requested_by.email}</span>
            </Meta>
            <Meta label="Created">{formatDateTime(cmd.created_at)}</Meta>
            <Meta label="Timeout">{cmd.timeout_s}s</Meta>
            <Meta label="Sent">{formatDateTime(cmd.sent_at)}</Meta>
            <Meta label="Started">{formatDateTime(cmd.started_at)}</Meta>
            <Meta label="Finished">{formatDateTime(cmd.finished_at)}</Meta>
            <Meta label="Duration">{formatDurationMs(commandDurationMs(cmd))}</Meta>
            {!terminal && <Meta label="Expires">{formatDateTime(cmd.expires_at)}</Meta>}
            {result && (
              <Meta label="Exit code">
                <span
                  className={cn(
                    'font-mono font-semibold',
                    result.exit_code === 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400',
                  )}
                >
                  {result.exit_code ?? '—'}
                </span>
              </Meta>
            )}
          </dl>

          {cmd.kind === 'exec' && cmd.argv && (
            <div>
              <div className="mb-1.5 text-xs font-medium text-muted">{cmd.shell ? 'Script' : 'argv'}</div>
              {cmd.shell ? (
                <pre className="max-h-48 overflow-auto rounded-md bg-subtle p-3 font-mono text-xs whitespace-pre-wrap">
                  {cmd.argv[0] ?? ''}
                </pre>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {cmd.argv.map((a, i) => (
                    <code key={i} className="rounded bg-subtle px-1.5 py-0.5 font-mono text-xs whitespace-pre">
                      {a === '' ? '""' : a}
                    </code>
                  ))}
                </div>
              )}
            </div>
          )}

          {result?.error && (
            <div className="flex gap-2 rounded-md border border-red-300 bg-red-50 p-3 text-sm text-red-900 dark:border-red-900 dark:bg-red-950/50 dark:text-red-200">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
              <span className="break-words">{result.error}</span>
            </div>
          )}
          {result?.truncated && (
            <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-200">
              Output was truncated by the agent.
            </div>
          )}

          {result ? (
            <>
              <OutputBlock label="stdout" text={result.stdout} />
              {(result.stderr || cmd.kind === 'exec') && (
                <OutputBlock label="stderr" text={result.stderr} stderr />
              )}
            </>
          ) : terminal ? (
            chunks.length > 0 ? (
              <LiveOutput chunks={chunks} running={false} />
            ) : (
              <p className="text-sm text-muted">No result was reported for this command.</p>
            )
          ) : (
            <LiveOutput chunks={chunks} running />
          )}
        </div>
      )}
    </Dialog>
  );
}
