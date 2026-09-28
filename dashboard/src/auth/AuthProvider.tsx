import { useCallback, useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  isApiError,
  login as apiLogin,
  logout as apiLogout,
  refreshSession,
  setSessionExpiredHandler,
} from '@/api/client';
import { clearAllOutput } from '@/features/commands/outputStore';
import { toast } from '@/components/ui/toast-store';
import { clearSession, getSession, subscribeSession } from './session';
import { AuthContext, type AuthContextValue } from './context';

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const session = useSyncExternalStore(subscribeSession, getSession);
  const [booting, setBooting] = useState(true);
  const [bootError, setBootError] = useState<unknown>(null);

  // Restore the session from the HttpOnly refresh cookie on first load.
  // refreshSession() is single-flight, so StrictMode's double effect is safe.
  useEffect(() => {
    let cancelled = false;
    refreshSession()
      .catch((e: unknown) => {
        if (cancelled) return;
        if (!(isApiError(e) && e.status === 401)) setBootError(e);
      })
      .finally(() => {
        if (!cancelled) setBooting(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // When the API layer cannot renew the session, drop all cached data.
  useEffect(() => {
    setSessionExpiredHandler(() => {
      qc.clear();
      clearAllOutput();
      toast.info('Session ended', 'Please sign in again.');
    });
    return () => setSessionExpiredHandler(null);
  }, [qc]);

  // Proactively refresh the access token ~60 s before it expires.
  useEffect(() => {
    if (!session.accessToken || !session.expiresAt) return;
    const delay = Math.max(5_000, session.expiresAt - Date.now() - 60_000);
    const t = setTimeout(() => {
      refreshSession().catch((e: unknown) => {
        if (isApiError(e) && e.status === 401) {
          clearSession();
          qc.clear();
          clearAllOutput();
          toast.info('Session ended', 'Please sign in again.');
        }
      });
    }, delay);
    return () => clearTimeout(t);
  }, [session.accessToken, session.expiresAt, qc]);

  const login = useCallback(
    async (email: string, password: string) => {
      qc.clear();
      await apiLogin(email, password);
      setBootError(null);
    },
    [qc],
  );

  const logout = useCallback(async () => {
    await apiLogout();
    qc.clear();
    clearAllOutput();
  }, [qc]);

  const value = useMemo<AuthContextValue>(
    () => ({
      status: booting ? 'loading' : session.user ? 'authenticated' : 'unauthenticated',
      user: session.user,
      login,
      logout,
      bootError,
    }),
    [booting, session.user, login, logout, bootError],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
