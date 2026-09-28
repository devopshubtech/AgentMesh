import { describe, expect, it } from 'vitest';
import { backoffDelay, SseParser } from './sse';

describe('SseParser', () => {
  it('parses a basic event', () => {
    const p = new SseParser();
    const out = p.push('event: device.status\ndata: {"device_id":"d1"}\n\n');
    expect(out).toEqual([{ event: 'device.status', data: '{"device_id":"d1"}' }]);
  });

  it('ignores comments like ": ping"', () => {
    const p = new SseParser();
    expect(p.push(': ping\n\n')).toEqual([]);
    expect(p.push(':\n')).toEqual([]);
  });

  it('handles events split across chunks', () => {
    const p = new SseParser();
    expect(p.push('event: command.out')).toEqual([]);
    expect(p.push('put\ndata: {"a"')).toEqual([]);
    expect(p.push(':1}\n')).toEqual([]);
    expect(p.push('\n')).toEqual([{ event: 'command.output', data: '{"a":1}' }]);
  });

  it('joins multi-line data with newlines', () => {
    const p = new SseParser();
    expect(p.push('data: line1\ndata: line2\n\n')).toEqual([
      { event: 'message', data: 'line1\nline2' },
    ]);
  });

  it('supports CRLF and CR line endings, including CRLF split across chunks', () => {
    const p = new SseParser();
    expect(p.push('event: a\r\ndata: 1\r')).toEqual([]);
    expect(p.push('\n\r\n')).toEqual([{ event: 'a', data: '1' }]);
    expect(p.push('event: b\rdata: 2\r\r')).toEqual([]);
    expect(p.push('x')).toEqual([{ event: 'b', data: '2' }]);
  });

  it('parses multiple events in one chunk and resets the event type', () => {
    const p = new SseParser();
    const out = p.push('event: x\ndata: 1\n\ndata: 2\n\n');
    expect(out).toEqual([
      { event: 'x', data: '1' },
      { event: 'message', data: '2' },
    ]);
  });

  it('does not dispatch events without data', () => {
    const p = new SseParser();
    expect(p.push('event: foo\n\n')).toEqual([]);
    expect(p.push('data: bar\n\n')).toEqual([{ event: 'message', data: 'bar' }]);
  });

  it('only strips a single leading space and supports no-space values', () => {
    const p = new SseParser();
    expect(p.push('event:e\ndata:  two\n\n')).toEqual([{ event: 'e', data: ' two' }]);
  });

  it('tracks id and ignores unknown fields', () => {
    const p = new SseParser();
    expect(p.push('id: 7\nretry: 1000\nfoo: bar\ndata: x\n\n')).toEqual([
      { event: 'message', data: 'x', id: '7' },
    ]);
  });
});

describe('backoffDelay', () => {
  it('grows exponentially and caps', () => {
    const mid = () => 0.5; // zero jitter
    expect(backoffDelay(0, 1000, 30000, mid)).toBe(1000);
    expect(backoffDelay(1, 1000, 30000, mid)).toBe(2000);
    expect(backoffDelay(3, 1000, 30000, mid)).toBe(8000);
    expect(backoffDelay(20, 1000, 30000, mid)).toBe(30000);
  });
  it('applies bounded jitter', () => {
    expect(backoffDelay(0, 1000, 30000, () => 0)).toBe(800);
    expect(backoffDelay(0, 1000, 30000, () => 1)).toBe(1200);
  });
});
