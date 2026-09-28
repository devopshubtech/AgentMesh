import { useSyncExternalStore } from 'react';
import { CheckCircle2, Info, X, XCircle } from 'lucide-react';
import { cn } from '@/lib/cn';
import { dismissToast, getToasts, subscribeToasts } from './toast-store';

export function Toaster() {
  const toasts = useSyncExternalStore(subscribeToasts, getToasts);
  return (
    <div
      aria-live="polite"
      className="pointer-events-none fixed right-0 bottom-0 z-[100] flex w-full max-w-sm flex-col gap-2 p-4"
    >
      {toasts.map((t) => {
        const Icon = t.variant === 'success' ? CheckCircle2 : t.variant === 'error' ? XCircle : Info;
        return (
          <div
            key={t.id}
            role={t.variant === 'error' ? 'alert' : 'status'}
            className={cn(
              'pointer-events-auto flex gap-3 rounded-lg border bg-surface p-3 shadow-lg',
              t.variant === 'error' && 'border-red-300 dark:border-red-900',
              t.variant === 'success' && 'border-emerald-300 dark:border-emerald-900',
              t.variant === 'info' && 'border-border',
            )}
          >
            <Icon
              className={cn(
                'mt-0.5 size-5 shrink-0',
                t.variant === 'error' && 'text-red-600 dark:text-red-400',
                t.variant === 'success' && 'text-emerald-600 dark:text-emerald-400',
                t.variant === 'info' && 'text-primary',
              )}
              aria-hidden
            />
            <div className="min-w-0 flex-1 text-sm">
              <p className="font-medium text-fg">{t.title}</p>
              {t.description && <p className="mt-0.5 break-words text-muted">{t.description}</p>}
              {t.requestId && (
                <p className="mt-1 font-mono text-xs text-muted select-all">
                  request_id: {t.requestId}
                </p>
              )}
            </div>
            <button
              type="button"
              onClick={() => dismissToast(t.id)}
              className="h-fit rounded p-0.5 text-muted hover:bg-subtle hover:text-fg"
              aria-label="Dismiss notification"
            >
              <X className="size-4" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
