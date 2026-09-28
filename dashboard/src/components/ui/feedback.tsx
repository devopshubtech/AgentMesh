import type { ReactNode } from 'react';
import { AlertTriangle, Inbox, Loader2 } from 'lucide-react';
import { isApiError } from '@/api/client';
import { cn } from '@/lib/cn';
import { errorMessage } from './toast-store';
import { Button } from './button';

export function Spinner({ className, label }: { className?: string; label?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2 text-sm text-muted', className)} role="status">
      <Loader2 className="size-4 animate-spin" aria-hidden />
      {label ?? <span className="sr-only">Loading</span>}
    </span>
  );
}

export function PageLoader({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="flex min-h-40 items-center justify-center">
      <Spinner label={label} />
    </div>
  );
}

export function EmptyState({
  title,
  description,
  icon,
  action,
}: {
  title: string;
  description?: ReactNode;
  icon?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-6 py-12 text-center">
      <div className="rounded-full bg-subtle p-3 text-muted">
        {icon ?? <Inbox className="size-5" aria-hidden />}
      </div>
      <p className="text-sm font-medium text-fg">{title}</p>
      {description && <p className="max-w-md text-sm text-muted">{description}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

export function ErrorState({
  error,
  onRetry,
  title = 'Something went wrong',
}: {
  error: unknown;
  onRetry?: () => void;
  title?: string;
}) {
  const reqId = isApiError(error) ? error.requestId : null;
  const forbidden = isApiError(error) && error.status === 403;
  return (
    <div
      role="alert"
      className="flex flex-col items-center justify-center gap-2 px-6 py-10 text-center"
    >
      <div className="rounded-full bg-red-50 p-3 text-red-600 dark:bg-red-950 dark:text-red-400">
        <AlertTriangle className="size-5" aria-hidden />
      </div>
      <p className="text-sm font-medium text-fg">{forbidden ? 'Access denied' : title}</p>
      <p className="max-w-md text-sm break-words text-muted">{errorMessage(error)}</p>
      {reqId && <p className="font-mono text-xs text-muted select-all">request_id: {reqId}</p>}
      {onRetry && !forbidden && (
        <Button variant="outline" size="sm" onClick={onRetry} className="mt-2">
          Retry
        </Button>
      )}
    </div>
  );
}

/** Horizontal usage bar. Width is set via CSSOM (style prop), which a strict CSP allows. */
export function UsageBar({ value, className }: { value: number; className?: string }) {
  const v = Math.max(0, Math.min(100, value));
  const tone = v >= 90 ? 'bg-red-500' : v >= 75 ? 'bg-amber-500' : 'bg-primary';
  return (
    <div
      className={cn('h-2 w-full overflow-hidden rounded-full bg-subtle', className)}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(v)}
    >
      <div className={cn('h-full rounded-full transition-[width]', tone)} style={{ width: `${v}%` }} />
    </div>
  );
}

export function LoadMore({
  hasNextPage,
  isFetchingNextPage,
  fetchNextPage,
}: {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => unknown;
}) {
  if (!hasNextPage) return null;
  return (
    <div className="flex justify-center border-t border-border p-3">
      <Button
        variant="outline"
        size="sm"
        loading={isFetchingNextPage}
        onClick={() => void fetchNextPage()}
      >
        Load more
      </Button>
    </div>
  );
}
