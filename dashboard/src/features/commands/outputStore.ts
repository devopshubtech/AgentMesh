import { useSyncExternalStore } from 'react';
import type { CommandOutputEvent } from '@/api/types';

/**
 * Live command output buffer, fed by `command.output` SSE events and keyed by command_id.
 * Chunks are de-duplicated by (stream, seq) and kept ordered by seq so the dialog can
 * show streaming output before the final result arrives.
 */

export interface OutputChunk {
  stream: 'stdout' | 'stderr';
  seq: number;
  data: string;
}

const MAX_CHARS_PER_COMMAND = 2_000_000;
const MAX_COMMANDS = 50;

const buffers = new Map<string, OutputChunk[]>();
const sizes = new Map<string, number>();
const listeners = new Map<string, Set<() => void>>();
const EMPTY: readonly OutputChunk[] = Object.freeze([]);

function notify(id: string) {
  listeners.get(id)?.forEach((l) => l());
}

export function appendOutput(ev: CommandOutputEvent): void {
  if (typeof ev.data !== 'string' || !ev.command_id) return;
  const stream: OutputChunk['stream'] = ev.stream === 'stderr' ? 'stderr' : 'stdout';
  const prev = buffers.get(ev.command_id) ?? [];
  if (prev.some((c) => c.seq === ev.seq && c.stream === stream)) return;
  const size = (sizes.get(ev.command_id) ?? 0) + ev.data.length;
  if (size > MAX_CHARS_PER_COMMAND) return; // final result will contain the (truncated) output
  const next: OutputChunk[] = [...prev, { stream, seq: Number(ev.seq) || 0, data: ev.data }];
  // Keep ordered by seq (stable for equal seq values).
  if (prev.length && prev[prev.length - 1]!.seq > ev.seq) next.sort((a, b) => a.seq - b.seq);
  if (!buffers.has(ev.command_id) && buffers.size >= MAX_COMMANDS) {
    const oldest = buffers.keys().next().value;
    if (oldest !== undefined) {
      buffers.delete(oldest);
      sizes.delete(oldest);
      notify(oldest);
    }
  }
  buffers.set(ev.command_id, next);
  sizes.set(ev.command_id, size);
  notify(ev.command_id);
}

export function getOutput(commandId: string): readonly OutputChunk[] {
  return buffers.get(commandId) ?? EMPTY;
}

export function clearOutput(commandId: string): void {
  if (buffers.delete(commandId)) {
    sizes.delete(commandId);
    notify(commandId);
  }
}

export function clearAllOutput(): void {
  const ids = [...buffers.keys()];
  buffers.clear();
  sizes.clear();
  ids.forEach(notify);
}

function subscribe(commandId: string, l: () => void): () => void {
  let set = listeners.get(commandId);
  if (!set) {
    set = new Set();
    listeners.set(commandId, set);
  }
  set.add(l);
  return () => {
    set.delete(l);
    if (set.size === 0) listeners.delete(commandId);
  };
}

export function useCommandOutput(commandId: string | null): readonly OutputChunk[] {
  return useSyncExternalStore(
    (l) => (commandId ? subscribe(commandId, l) : () => {}),
    () => (commandId ? getOutput(commandId) : EMPTY),
  );
}
