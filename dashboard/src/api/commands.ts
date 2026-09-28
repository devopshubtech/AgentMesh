import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { api } from './client';
import { qk } from './keys';
import { isTerminalStatus, type Command, type CreateCommandRequest, type ListResponse } from './types';

export type CommandListData = InfiniteData<ListResponse<Command>, string | null>;

export function useDeviceCommands(deviceId: string, enabled = true) {
  return useInfiniteQuery({
    queryKey: qk.commands.byDevice(deviceId),
    queryFn: ({ pageParam, signal }) =>
      api<ListResponse<Command>>(`/devices/${encodeURIComponent(deviceId)}/commands`, {
        query: { limit: 25, cursor: pageParam },
        signal,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled,
  });
}

export function useCommand(id: string | null) {
  return useQuery({
    queryKey: qk.commands.detail(id ?? ''),
    queryFn: ({ signal }) => api<Command>(`/commands/${encodeURIComponent(id ?? '')}`, { signal }),
    enabled: !!id,
    // Safety net in case an SSE event is missed while the command is still running.
    refetchInterval: (q) => {
      const data = q.state.data;
      return data && !isTerminalStatus(data.status) ? 5000 : false;
    },
  });
}

function upsertCommand(qc: ReturnType<typeof useQueryClient>, cmd: Command) {
  qc.setQueryData(qk.commands.detail(cmd.id), cmd);
  qc.setQueryData<CommandListData>(qk.commands.byDevice(cmd.device_id), (old) => {
    if (!old) return old;
    const exists = old.pages.some((p) => p.items.some((c) => c.id === cmd.id));
    if (exists) {
      return {
        ...old,
        pages: old.pages.map((p) => ({
          ...p,
          items: p.items.map((c) => (c.id === cmd.id ? cmd : c)),
        })),
      };
    }
    const [first, ...rest] = old.pages;
    if (!first) return old;
    return { ...old, pages: [{ ...first, items: [cmd, ...first.items] }, ...rest] };
  });
}

export function useCreateCommand(deviceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ body, idempotencyKey }: { body: CreateCommandRequest; idempotencyKey: string }) =>
      api<Command>(`/devices/${encodeURIComponent(deviceId)}/commands`, {
        method: 'POST',
        body,
        headers: { 'Idempotency-Key': idempotencyKey },
      }),
    onSuccess: (cmd) => upsertCommand(qc, cmd),
  });
}

export function useCancelCommand() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      api<Command>(`/commands/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),
    onSuccess: (cmd) => upsertCommand(qc, cmd),
  });
}
