import type { User } from '@/api/types';

/**
 * In-memory session state. The access token lives ONLY in this module variable:
 * it is never written to localStorage/sessionStorage/cookies, so XSS cannot
 * lift a long-lived credential (the refresh token is an HttpOnly cookie).
 */

export interface SessionState {
  accessToken: string | null;
  /** Epoch ms when the access token expires (client clock). */
  expiresAt: number | null;
  user: User | null;
}

let state: SessionState = { accessToken: null, expiresAt: null, user: null };
const listeners = new Set<() => void>();

function emit() {
  for (const l of listeners) l();
}

export function getSession(): SessionState {
  return state;
}

export function getAccessToken(): string | null {
  return state.accessToken;
}

export function setSession(accessToken: string, expiresInSec: number, user: User): void {
  state = {
    accessToken,
    expiresAt: Date.now() + Math.max(0, expiresInSec) * 1000,
    user,
  };
  emit();
}

export function setSessionUser(user: User): void {
  if (!state.accessToken) return;
  state = { ...state, user };
  emit();
}

export function clearSession(): void {
  if (!state.accessToken && !state.user) return;
  state = { accessToken: null, expiresAt: null, user: null };
  emit();
}

export function subscribeSession(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
