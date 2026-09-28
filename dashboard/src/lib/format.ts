const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const;

/** Formats a byte count using binary (1024) multiples, e.g. 1536 -> "1.5 KB". */
export function formatBytes(bytes: number | null | undefined, decimals = 1): string {
  if (bytes == null || !Number.isFinite(bytes) || bytes < 0) return '—';
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  const fixed = value.toFixed(value >= 100 ? 0 : decimals);
  const trimmed = fixed.includes('.') ? fixed.replace(/\.0+$/, '') : fixed;
  return `${trimmed} ${BYTE_UNITS[unit]}`;
}

/** Formats seconds as a compact duration, e.g. 93784 -> "1d 2h 3m". */
export function formatDurationSeconds(totalSeconds: number | null | undefined): string {
  if (totalSeconds == null || !Number.isFinite(totalSeconds) || totalSeconds < 0) return '—';
  const s = Math.floor(totalSeconds);
  if (s < 60) return `${s}s`;
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;
  const parts: string[] = [];
  if (days) parts.push(`${days}d`);
  if (hours) parts.push(`${hours}h`);
  if (minutes) parts.push(`${minutes}m`);
  if (!days && !hours && seconds) parts.push(`${seconds}s`);
  return parts.slice(0, 3).join(' ');
}

/** Formats milliseconds, e.g. 450 -> "450 ms", 2500 -> "2.5 s", 125000 -> "2m 5s". */
export function formatDurationMs(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms < 0) return '—';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60_000) {
    const secs = ms / 1000;
    return `${secs < 10 ? secs.toFixed(1).replace(/\.0$/, '') : String(Math.round(secs))} s`;
  }
  return formatDurationSeconds(ms / 1000);
}

function toDate(input: string | Date | null | undefined): Date | null {
  if (input == null || input === '') return null;
  const d = input instanceof Date ? input : new Date(input);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** Relative time such as "just now", "5 min ago", "in 3 h", "2 days ago". */
export function formatRelativeTime(
  input: string | Date | null | undefined,
  now: Date = new Date(),
): string {
  const d = toDate(input);
  if (!d) return 'never';
  const diffSec = Math.round((d.getTime() - now.getTime()) / 1000);
  const abs = Math.abs(diffSec);
  const future = diffSec > 0;
  if (abs < 10) return 'just now';
  const fmt = (n: number, unit: string) => (future ? `in ${n} ${unit}` : `${n} ${unit} ago`);
  if (abs < 60) return fmt(abs, 's');
  if (abs < 3600) return fmt(Math.floor(abs / 60), 'min');
  if (abs < 86400) return fmt(Math.floor(abs / 3600), 'h');
  const days = Math.floor(abs / 86400);
  if (days < 30) return fmt(days, days === 1 ? 'day' : 'days');
  if (days < 365) {
    const months = Math.floor(days / 30);
    return fmt(months, months === 1 ? 'month' : 'months');
  }
  const years = Math.floor(days / 365);
  return fmt(years, years === 1 ? 'year' : 'years');
}

/** Absolute local timestamp, e.g. "2026-09-28 14:03:12". */
export function formatDateTime(input: string | Date | null | undefined): string {
  const d = toDate(input);
  if (!d) return '—';
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  );
}

/** Percentage 0–100 clamped; returns 0 when total is 0. */
export function percent(used: number, total: number): number {
  if (!total || total <= 0 || !Number.isFinite(used)) return 0;
  return Math.min(100, Math.max(0, (used / total) * 100));
}

/** Command duration: prefers result.duration_ms, falls back to started/finished timestamps. */
export function commandDurationMs(cmd: {
  started_at: string | null;
  finished_at: string | null;
  result: { duration_ms: number } | null;
}): number | null {
  if (cmd.result && Number.isFinite(cmd.result.duration_ms)) return cmd.result.duration_ms;
  const s = toDate(cmd.started_at);
  const f = toDate(cmd.finished_at);
  if (s && f) return Math.max(0, f.getTime() - s.getTime());
  return null;
}
