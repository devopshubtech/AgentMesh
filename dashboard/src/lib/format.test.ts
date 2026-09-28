import { describe, expect, it } from 'vitest';
import {
  commandDurationMs,
  formatBytes,
  formatDateTime,
  formatDurationMs,
  formatDurationSeconds,
  formatRelativeTime,
  percent,
} from './format';

describe('formatBytes', () => {
  it('handles small and invalid values', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(null)).toBe('—');
    expect(formatBytes(-1)).toBe('—');
    expect(formatBytes(Number.NaN)).toBe('—');
  });
  it('uses binary units', () => {
    expect(formatBytes(1024)).toBe('1 KB');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(8 * 1024 ** 3)).toBe('8 GB');
    expect(formatBytes(500 * 1024 ** 3)).toBe('500 GB');
    expect(formatBytes(2.25 * 1024 ** 4, 2)).toBe('2.25 TB');
  });
});

describe('formatDurationSeconds', () => {
  it('formats compact durations', () => {
    expect(formatDurationSeconds(5)).toBe('5s');
    expect(formatDurationSeconds(125)).toBe('2m 5s');
    expect(formatDurationSeconds(3600)).toBe('1h');
    expect(formatDurationSeconds(93784)).toBe('1d 2h 3m');
    expect(formatDurationSeconds(null)).toBe('—');
  });
});

describe('formatDurationMs', () => {
  it('formats milliseconds', () => {
    expect(formatDurationMs(450)).toBe('450 ms');
    expect(formatDurationMs(2500)).toBe('2.5 s');
    expect(formatDurationMs(2000)).toBe('2 s');
    expect(formatDurationMs(42_000)).toBe('42 s');
    expect(formatDurationMs(125_000)).toBe('2m 5s');
    expect(formatDurationMs(undefined)).toBe('—');
  });
});

describe('formatRelativeTime', () => {
  const now = new Date('2026-09-28T12:00:00Z');
  it('formats past and future', () => {
    expect(formatRelativeTime('2026-09-28T11:59:58Z', now)).toBe('just now');
    expect(formatRelativeTime('2026-09-28T11:59:30Z', now)).toBe('30 s ago');
    expect(formatRelativeTime('2026-09-28T11:55:00Z', now)).toBe('5 min ago');
    expect(formatRelativeTime('2026-09-28T09:00:00Z', now)).toBe('3 h ago');
    expect(formatRelativeTime('2026-09-27T12:00:00Z', now)).toBe('1 day ago');
    expect(formatRelativeTime('2026-09-18T12:00:00Z', now)).toBe('10 days ago');
    expect(formatRelativeTime('2026-09-28T15:00:00Z', now)).toBe('in 3 h');
    expect(formatRelativeTime('2024-09-28T12:00:00Z', now)).toBe('2 years ago');
  });
  it('handles null and garbage', () => {
    expect(formatRelativeTime(null, now)).toBe('never');
    expect(formatRelativeTime('not a date', now)).toBe('never');
  });
});

describe('formatDateTime', () => {
  it('formats and handles invalid input', () => {
    expect(formatDateTime(new Date(2026, 0, 2, 3, 4, 5))).toBe('2026-01-02 03:04:05');
    expect(formatDateTime(null)).toBe('—');
  });
});

describe('percent', () => {
  it('clamps', () => {
    expect(percent(50, 200)).toBe(25);
    expect(percent(5, 0)).toBe(0);
    expect(percent(300, 200)).toBe(100);
  });
});

describe('commandDurationMs', () => {
  it('prefers result.duration_ms', () => {
    expect(
      commandDurationMs({ started_at: null, finished_at: null, result: { duration_ms: 42 } }),
    ).toBe(42);
  });
  it('falls back to timestamps', () => {
    expect(
      commandDurationMs({
        started_at: '2026-09-28T12:00:00Z',
        finished_at: '2026-09-28T12:00:03Z',
        result: null,
      }),
    ).toBe(3000);
    expect(commandDurationMs({ started_at: null, finished_at: null, result: null })).toBeNull();
  });
});
