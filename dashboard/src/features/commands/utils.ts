import type { Command, Device } from '@/api/types';
import { joinArgv } from '@/lib/argv';

export const ACTION_PREFIX = 'action.';

/** Action names the device declared, e.g. "action.ping" -> "ping". */
export function deviceActions(device: Pick<Device, 'capabilities'>): string[] {
  return device.capabilities
    .filter((c) => c.startsWith(ACTION_PREFIX) && c.length > ACTION_PREFIX.length)
    .map((c) => c.slice(ACTION_PREFIX.length));
}

export function deviceSupportsExec(device: Pick<Device, 'capabilities'>): boolean {
  return device.capabilities.includes('exec');
}

/** One-line human summary of what a command runs. */
export function commandSummary(cmd: Pick<Command, 'kind' | 'action' | 'argv' | 'shell'>): string {
  if (cmd.kind === 'action') return cmd.action ?? '(action)';
  if (!cmd.argv || cmd.argv.length === 0) return '(empty)';
  if (cmd.shell) {
    const script = cmd.argv[0] ?? '';
    const firstLine = script.split(/\r?\n/, 1)[0] ?? '';
    return script.includes('\n') ? `${firstLine} …` : firstLine;
  }
  return joinArgv(cmd.argv);
}

export function cancelPermission(cmd: Pick<Command, 'kind'>) {
  return cmd.kind === 'exec' ? ('commands.execute.exec' as const) : ('commands.execute.action' as const);
}
