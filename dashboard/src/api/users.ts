import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { setSessionUser, getSession } from '@/auth/session';
import { api } from './client';
import { qk } from './keys';
import type { CreateUserRequest, ListResponse, Role, UpdateUserRequest, User } from './types';

export function useUsers() {
  return useInfiniteQuery({
    queryKey: qk.users.all,
    queryFn: ({ pageParam, signal }) =>
      api<ListResponse<User>>('/users', { query: { limit: 50, cursor: pageParam }, signal }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useRoles(enabled = true) {
  return useQuery({
    queryKey: qk.users.roles,
    queryFn: ({ signal }) => api<Role[]>('/roles', { signal }),
    staleTime: 5 * 60_000,
    enabled,
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateUserRequest) => api<User>('/users', { method: 'POST', body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.users.all }),
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<void>(`/users/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.users.all }),
  });
}

export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: UpdateUserRequest }) =>
      api<User>(`/users/${encodeURIComponent(id)}`, { method: 'PATCH', body }),
    onSuccess: (user) => {
      if (getSession().user?.id === user.id) setSessionUser(user);
      return qc.invalidateQueries({ queryKey: qk.users.all });
    },
  });
}
