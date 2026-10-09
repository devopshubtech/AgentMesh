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
import { DEMO_USER } from '@/demo/data';
import { enterDemo, exitDemo, isDemo } from '@/demo/mode';
import { clearSession, getSession, setSession, subscribeSession } from './session';
import { AuthContext, type AuthContextValue } from './context';

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const session = useSyncExternalStore(subscribeSession, getSession);
  // A demo tab is signed in before the first render (main.tsx), so it never boots.
  const [booting, setBooting] = useState(() => !isDemo());
  const [bootError, setBootError] = useState<unknown>(null);
  const [demo, setDemo] = useState(isDemo);

  // Restore the session from the HttpOnly refresh cookie on first load.
  // refreshSession() is single-flight, so StrictMode's double effect is safe.
  useEffect(() => {
    let cancelled = false;
    if (isDemo()) return; // demo tab: no server session to restore
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
    if (demo || !session.accessToken || !session.expiresAt) return;
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
  }, [demo, session.accessToken, session.expiresAt, qc]);

  const login = useCallback(
    async (email: string, password: string) => {
      qc.clear();
      exitDemo(); // a real sign-in ends demo mode in this tab
      setDemo(false);
      await apiLogin(email, password);
      setBootError(null);
    },
    [qc],
  );

  const logout = useCallback(async () => {
    if (isDemo()) {
      exitDemo();
      setDemo(false);
      clearSession();
    } else {
      await apiLogout();
    }
    qc.clear();
    clearAllOutput();
  }, [qc]);

  const startDemo = useCallback(() => {
    qc.clear();
    enterDemo();
    setDemo(true);
    setSession('demo', 365 * 86400, DEMO_USER);
    setBooting(false);
  }, [qc]);

  const value = useMemo<AuthContextValue>(
    () => ({
      status: booting ? 'loading' : session.user ? 'authenticated' : 'unauthenticated',
      user: session.user,
      login,
      logout,
      bootError,
      demo,
      startDemo,
    }),
    [booting, session.user, login, logout, bootError, demo, startDemo],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
