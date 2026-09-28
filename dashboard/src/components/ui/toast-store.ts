import { isApiError } from '@/api/client';

export type ToastVariant = 'success' | 'error' | 'info';

export interface ToastItem {
  id: number;
  variant: ToastVariant;
  title: string;
  description?: string;
  requestId?: string | null;
}

let items: ToastItem[] = [];
let nextId = 1;
const listeners = new Set<() => void>();
const timers = new Map<number, ReturnType<typeof setTimeout>>();

function emit() {
  for (const l of listeners) l();
}

export function subscribeToasts(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

export function getToasts(): ToastItem[] {
  return items;
}

export function dismissToast(id: number): void {
  const t = timers.get(id);
  if (t) clearTimeout(t);
  timers.delete(id);
  items = items.filter((i) => i.id !== id);
  emit();
}

function push(t: Omit<ToastItem, 'id'>, durationMs: number): number {
  const id = nextId++;
  items = [...items.slice(-4), { ...t, id }];
  emit();
  timers.set(
    id,
    setTimeout(() => dismissToast(id), durationMs),
  );
  return id;
}

export const toast = {
  success: (title: string, description?: string) =>
    push({ variant: 'success', title, description }, 4000),
  info: (title: string, description?: string) => push({ variant: 'info', title, description }, 5000),
  error: (title: string, description?: string, requestId?: string | null) =>
    push({ variant: 'error', title, description, requestId }, 8000),
};

/** Human-readable message for any thrown value. */
export function errorMessage(err: unknown): string {
  if (isApiError(err)) {
    if (err.code === 'rate_limited' && err.retryAfterSec) {
      return `${err.message} (retry in ${err.retryAfterSec}s)`;
    }
    if (err.fields && Object.keys(err.fields).length) {
      const f = Object.entries(err.fields)
        .map(([k, v]) => `${k}: ${v}`)
        .join(', ');
      return `${err.message} — ${f}`;
    }
    return err.message;
  }
  if (err instanceof Error) return err.message;
  return 'Unexpected error';
}

/** Shows an error toast including the server request_id when available. */
export function toastError(err: unknown, title = 'Request failed'): void {
  toast.error(title, errorMessage(err), isApiError(err) ? err.requestId : null);
}
