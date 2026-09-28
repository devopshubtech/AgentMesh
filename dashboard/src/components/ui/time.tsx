import { useEffect, useState } from 'react';
import { formatDateTime, formatRelativeTime } from '@/lib/format';

let now = Date.now();
const subs = new Set<(n: number) => void>();
let interval: ReturnType<typeof setInterval> | null = null;

function subscribeNow(cb: (n: number) => void) {
  subs.add(cb);
  if (!interval) {
    interval = setInterval(() => {
      now = Date.now();
      subs.forEach((s) => s(now));
    }, 15_000);
  }
  return () => {
    subs.delete(cb);
    if (subs.size === 0 && interval) {
      clearInterval(interval);
      interval = null;
    }
  };
}

/** Relative time that re-renders periodically, with the absolute time as tooltip. */
export function RelativeTime({ value, fallback = 'never' }: { value: string | null; fallback?: string }) {
  const [tick, setTick] = useState(() => Date.now());
  useEffect(() => subscribeNow(setTick), []);
  if (!value) return <span className="text-muted">{fallback}</span>;
  return (
    <time dateTime={value} title={formatDateTime(value)} className="whitespace-nowrap">
      {formatRelativeTime(value, new Date(Math.max(tick, now)))}
    </time>
  );
}
