import { useMemo, useRef, useState, type FormEvent } from 'react';
import { Play, Terminal, Zap } from 'lucide-react';
import { useCreateCommand } from '@/api/commands';
import type { Command, CommandKind, CreateCommandRequest, Device } from '@/api/types';
import { useCan } from '@/auth/context';
import { Button } from '@/components/ui/button';
import { Checkbox, Field, Input, Textarea } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { toast } from '@/components/ui/toast-store';
import { ArgvParseError, splitArgv } from '@/lib/argv';
import { cn } from '@/lib/cn';
import { uuid } from '@/lib/uuid';
import { deviceActions, deviceSupportsExec } from './utils';

const MIN_TIMEOUT = 1;
const MAX_TIMEOUT = 3600;

export function RunCommandForm({
  device,
  onCreated,
}: {
  device: Device;
  onCreated: (cmd: Command) => void;
}) {
  const canAction = useCan('commands.execute.action');
  const canExec = useCan('commands.execute.exec');
  const actions = useMemo(() => deviceActions(device), [device]);
  const execSupported = deviceSupportsExec(device);

  const allowedKinds: CommandKind[] = [];
  if (canAction && actions.length > 0) allowedKinds.push('action');
  if (canExec && execSupported) allowedKinds.push('exec');

  const [kindPref, setKind] = useState<CommandKind>('action');
  const kind: CommandKind | undefined = allowedKinds.includes(kindPref) ? kindPref : allowedKinds[0];
  const [actionPref, setAction] = useState('');
  const action = actions.includes(actionPref) ? actionPref : (actions[0] ?? '');
  const [line, setLine] = useState('');
  const [shell, setShell] = useState(false);
  const [script, setScript] = useState('');
  const [timeout, setTimeoutS] = useState('60');

  // One Idempotency-Key per logical submission: it is reused if the user retries the exact
  // same request after an error, and regenerated as soon as any input changes.
  const keyRef = useRef<string | null>(null);
  const resetKey = () => {
    keyRef.current = null;
  };

  const create = useCreateCommand(device.id);

  const parsed = useMemo((): { argv: string[]; error: string | null } => {
    if (kind !== 'exec' || shell) return { argv: [], error: null };
    try {
      return { argv: splitArgv(line), error: null };
    } catch (e) {
      return { argv: [], error: e instanceof ArgvParseError ? e.message : 'Invalid command line' };
    }
  }, [kind, shell, line]);

  const timeoutNum = Number(timeout);
  const timeoutValid =
    Number.isInteger(timeoutNum) && timeoutNum >= MIN_TIMEOUT && timeoutNum <= MAX_TIMEOUT;

  if (allowedKinds.length === 0) {
    const reason = !canAction && !canExec
      ? 'Your role cannot run commands on devices.'
      : 'This device has not declared any capabilities that your role is allowed to use.';
    return <p className="rounded-md bg-subtle px-3 py-2 text-sm text-muted">{reason}</p>;
  }

  const deviceActive = device.status === 'active';

  let body: CreateCommandRequest | null = null;
  if (kind === 'action' && action) {
    body = { kind: 'action', action };
  } else if (kind === 'exec' && timeoutValid) {
    if (shell && script.trim()) body = { kind: 'exec', argv: [script], shell: true, timeout_s: timeoutNum };
    if (!shell && parsed.argv.length > 0 && !parsed.error) {
      body = { kind: 'exec', argv: parsed.argv, shell: false, timeout_s: timeoutNum };
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!body || !deviceActive) return;
    keyRef.current ??= uuid();
    create.mutate(
      { body, idempotencyKey: keyRef.current },
      {
        onSuccess: (cmd) => {
          resetKey();
          toast.success('Command queued', cmd.kind === 'action' ? cmd.action ?? undefined : undefined);
          if (kind === 'exec') {
            setLine('');
            setScript('');
          }
          onCreated(cmd);
        },
      },
    );
  };

  return (
    <form onSubmit={submit} className="flex flex-col gap-4 rounded-lg border border-border bg-subtle/30 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">Run command</h3>
        {allowedKinds.length > 1 && (
          <div role="radiogroup" aria-label="Command kind" className="inline-flex rounded-md border border-border bg-surface p-0.5">
            {allowedKinds.map((k) => (
              <button
                key={k}
                type="button"
                role="radio"
                aria-checked={kind === k}
                onClick={() => {
                  setKind(k);
                  resetKey();
                }}
                className={cn(
                  'inline-flex items-center gap-1.5 rounded px-3 py-1 text-xs font-medium',
                  kind === k ? 'bg-primary text-primary-fg' : 'text-muted hover:text-fg',
                )}
              >
                {k === 'action' ? <Zap className="size-3.5" /> : <Terminal className="size-3.5" />}
                {k === 'action' ? 'Action' : 'Exec'}
              </button>
            ))}
          </div>
        )}
      </div>

      {!deviceActive && (
        <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:bg-amber-950/50 dark:text-amber-200">
          Commands can only be sent to active devices (this device is {device.status}).
        </p>
      )}

      {kind === 'action' && (
        <Field label="Action" hint="Predefined safe actions declared by the agent.">
          {(id) => (
            <Select
              id={id}
              className="max-w-sm"
              value={action}
              onChange={(e) => {
                setAction(e.target.value);
                resetKey();
              }}
              options={actions.map((a) => ({ value: a, label: a }))}
            />
          )}
        </Field>
      )}

      {kind === 'exec' && (
        <>
          <Checkbox
            label="Shell script mode (runs via the device's shell)"
            checked={shell}
            onChange={(e) => {
              setShell(e.target.checked);
              resetKey();
            }}
          />
          {shell ? (
            <Field label="Script" hint="Sent as a single argument with shell: true.">
              {(id) => (
                <Textarea
                  id={id}
                  value={script}
                  spellCheck={false}
                  rows={5}
                  onChange={(e) => {
                    setScript(e.target.value);
                    resetKey();
                  }}
                  placeholder={'set -e\ndf -h\nuptime'}
                />
              )}
            </Field>
          ) : (
            <Field
              label="Command line"
              error={parsed.error}
              hint="Split into argv like a shell (quotes respected). No pipes, globbing or variable expansion."
            >
              {(id) => (
                <Input
                  id={id}
                  value={line}
                  spellCheck={false}
                  autoComplete="off"
                  className="font-mono"
                  aria-invalid={!!parsed.error}
                  onChange={(e) => {
                    setLine(e.target.value);
                    resetKey();
                  }}
                  placeholder='uname -a   ·   ls -la "/var/log"'
                />
              )}
            </Field>
          )}
          {!shell && parsed.argv.length > 0 && (
            <div className="flex flex-wrap items-center gap-1 text-xs">
              <span className="text-muted">argv:</span>
              {parsed.argv.map((a, i) => (
                <code key={i} className="rounded bg-subtle px-1.5 py-0.5 font-mono whitespace-pre text-fg">
                  {a === '' ? '""' : a}
                </code>
              ))}
            </div>
          )}
          <Field
            label="Timeout (seconds)"
            error={timeoutValid ? null : `Enter a whole number between ${MIN_TIMEOUT} and ${MAX_TIMEOUT}.`}
            className="max-w-40"
          >
            {(id) => (
              <Input
                id={id}
                type="number"
                min={MIN_TIMEOUT}
                max={MAX_TIMEOUT}
                step={1}
                value={timeout}
                aria-invalid={!timeoutValid}
                onChange={(e) => {
                  setTimeoutS(e.target.value);
                  resetKey();
                }}
              />
            )}
          </Field>
        </>
      )}

      <div>
        <Button
          type="submit"
          disabled={!body || !deviceActive}
          loading={create.isPending}
          icon={<Play className="size-4" aria-hidden />}
        >
          Run
        </Button>
      </div>
    </form>
  );
}
