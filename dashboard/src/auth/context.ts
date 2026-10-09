import { createContext, useContext } from 'react';
import type { Permission, User } from '@/api/types';

export type AuthStatus = 'loading' | 'authenticated' | 'unauthenticated';

export interface AuthContextValue {
  status: AuthStatus;
  user: User | null;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  /** Error from the initial session restore (non-401), shown on the login page. */
  bootError: unknown;
  /** True while exploring the demo (sample data, no server). */
  demo: boolean;
  /** Opens the dashboard with sample data, without an account. */
  startDemo: () => void;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>');
  return ctx;
}

/** UX-only permission check. The API always enforces authorization server-side. */
export function useCan(perm: Permission): boolean {
  const { user } = useAuth();
  return !!user?.permissions.includes(perm);
}

/** Returns a checker function, handy when several permissions are needed. */
export function usePermissions(): (perm: Permission) => boolean {
  const { user } = useAuth();
  const perms = user?.permissions ?? [];
  return (perm) => perms.includes(perm);
}
