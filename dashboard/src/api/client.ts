import { clearSession, getAccessToken, getSession, setSession } from '@/auth/session';
import { demoApi } from '@/demo/api';
import { isDemo } from '@/demo/mode';
import type { ApiErrorBody, LoginResponse } from './types';

export const API_BASE = '/v1';
const CSRF_HEADER = { 'X-Requested-With': 'agentmesh' } as const;

/** Typed error for every non-2xx response (parsed from the error envelope). */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId: string | null;
  readonly fields: Record<string, string> | undefined;
  readonly retryAfterSec: number | null;

  constructor(opts: {
    status: number;
    code: string;
    message: string;
    requestId: string | null;
    fields?: Record<string, string>;
    retryAfterSec?: number | null;
  }) {
    super(opts.message);
    this.name = 'ApiError';
    this.status = opts.status;
    this.code = opts.code;
    this.requestId = opts.requestId;
    this.fields = opts.fields;
    this.retryAfterSec = opts.retryAfterSec ?? null;
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError;
}

function isErrorBody(v: unknown): v is ApiErrorBody {
  if (typeof v !== 'object' || v === null || !('error' in v)) return false;
  const err = (v as { error: unknown }).error;
  return typeof err === 'object' && err !== null && 'code' in err && 'message' in err;
}

async function toApiError(res: Response): Promise<ApiError> {
  const headerReqId = res.headers.get('X-Request-ID');
  const retryAfterRaw = res.headers.get('Retry-After');
  const retryAfter = retryAfterRaw && /^\d+$/.test(retryAfterRaw) ? Number(retryAfterRaw) : null;
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    body = null;
  }
  if (isErrorBody(body)) {
    return new ApiError({
      status: res.status,
      code: String(body.error.code),
      message: String(body.error.message),
      requestId: body.error.request_id ?? headerReqId,
      fields: body.error.fields,
      retryAfterSec: retryAfter,
    });
  }
  return new ApiError({
    status: res.status,
    code: res.status >= 500 ? 'internal' : 'http_error',
    message: res.statusText || `Request failed with status ${res.status}`,
    requestId: headerReqId,
    retryAfterSec: retryAfter,
  });
}

function networkError(e: unknown): ApiError {
  return new ApiError({
    status: 0,
    code: 'network_error',
    message: e instanceof Error && e.message ? `Network error: ${e.message}` : 'Network error',
    requestId: null,
  });
}

// ---------------------------------------------------------------------------
// Session refresh (single-flight)
// ---------------------------------------------------------------------------

let refreshInFlight: Promise<LoginResponse> | null = null;
let onSessionExpired: (() => void) | null = null;

/** Registered by the AuthProvider: called when the session cannot be renewed. */
export function setSessionExpiredHandler(fn: (() => void) | null): void {
  onSessionExpired = fn;
}

async function doRefresh(): Promise<LoginResponse> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}/auth/refresh`, {
      method: 'POST',
      credentials: 'include',
      headers: { ...CSRF_HEADER, Accept: 'application/json' },
      cache: 'no-store',
    });
  } catch (e) {
    throw networkError(e);
  }
  if (!res.ok) throw await toApiError(res);
  const data = (await res.json()) as LoginResponse;
  setSession(data.access_token, data.expires_in, data.user);
  return data;
}

/**
 * Refreshes the access token using the HttpOnly refresh cookie.
 * Concurrent callers share one in-flight request (refresh tokens rotate, so
 * parallel refreshes would trigger reuse detection and revoke the session).
 */
export function refreshSession(): Promise<LoginResponse> {
  if (!refreshInFlight) {
    refreshInFlight = doRefresh().finally(() => {
      refreshInFlight = null;
    });
  }
  return refreshInFlight;
}

/** Returns a usable access token, refreshing first if it is (nearly) expired. */
export async function getFreshAccessToken(): Promise<string | null> {
  const s = getSession();
  if (s.accessToken && s.expiresAt && s.expiresAt - Date.now() > 15_000) return s.accessToken;
  if (!s.user && !s.accessToken) return null;
  try {
    const r = await refreshSession();
    return r.access_token;
  } catch (e) {
    if (isApiError(e) && e.status === 401) {
      expireSession();
    }
    return null;
  }
}

function expireSession() {
  // Only notify when there actually was a session (not for stray requests after logout).
  const hadSession = !!getSession().user;
  clearSession();
  if (hadSession) onSessionExpired?.();
}

// ---------------------------------------------------------------------------
// Fetch wrapper
// ---------------------------------------------------------------------------

export type QueryValue = string | number | boolean | null | undefined;

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE';
  query?: Record<string, QueryValue>;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  /** Skip auth header + 401 refresh logic (login endpoint). */
  anonymous?: boolean;
}

export function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  const url = `${API_BASE}${path}`;
  if (!query) return url;
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === '') continue;
    params.set(k, String(v));
  }
  const qs = params.toString();
  return qs ? `${url}?${qs}` : url;
}

async function send(path: string, opts: RequestOptions, token: string | null): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers };
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  if (token && !opts.anonymous) headers['Authorization'] = `Bearer ${token}`;
  try {
    return await fetch(buildUrl(path, opts.query), {
      method: opts.method ?? 'GET',
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      credentials: 'same-origin',
      signal: opts.signal,
      cache: 'no-store',
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e;
    throw networkError(e);
  }
}

/**
 * Performs an API request. Attaches the in-memory Bearer token; on 401 it
 * refreshes once (single-flight) and retries; if that fails the session ends.
 */
export async function api<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  // Demo mode answers from sample data in the browser; no server is involved.
  if (isDemo()) return demoApi<T>(path, opts);
  let res = await send(path, opts, opts.anonymous ? null : getAccessToken());

  if (res.status === 401 && !opts.anonymous) {
    try {
      await refreshSession();
    } catch (refreshErr) {
      const err = await toApiError(res);
      // Only end the session when the server rejected the refresh; transient
      // network/5xx failures surface as errors without logging the user out.
      if (isApiError(refreshErr) && (refreshErr.status === 401 || refreshErr.status === 403)) {
        expireSession();
        throw err;
      }
      throw isApiError(refreshErr) ? refreshErr : err;
    }
    res = await send(path, opts, getAccessToken());
    if (res.status === 401) {
      const err = await toApiError(res);
      expireSession();
      throw err;
    }
  }

  if (!res.ok) throw await toApiError(res);
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

// ---------------------------------------------------------------------------
// Auth endpoints
// ---------------------------------------------------------------------------

export async function login(email: string, password: string): Promise<LoginResponse> {
  const data = await api<LoginResponse>('/auth/login', {
    method: 'POST',
    body: { email, password, client: 'web' },
    anonymous: true,
  });
  setSession(data.access_token, data.expires_in, data.user);
  return data;
}

export async function logout(): Promise<void> {
  const token = getAccessToken();
  try {
    await fetch(`${API_BASE}/auth/logout`, {
      method: 'POST',
      credentials: 'include',
      headers: {
        ...CSRF_HEADER,
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      cache: 'no-store',
    });
  } catch {
    // Ignore network errors: we clear local state regardless.
  } finally {
    clearSession();
  }
}
