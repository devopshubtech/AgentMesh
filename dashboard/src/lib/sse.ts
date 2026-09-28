/** A single parsed Server-Sent Event. */
export interface SseMessage {
  event: string;
  data: string;
  id?: string;
}

/**
 * Incremental SSE line parser (WHATWG event-stream format).
 * Feed it decoded text chunks; it returns complete events as they are dispatched.
 * Lines starting with ':' are comments (e.g. ": ping") and are ignored.
 */
export class SseParser {
  private buffer = '';
  private eventType = '';
  private dataLines: string[] = [];
  private hasData = false;
  private lastId: string | undefined;

  push(chunk: string): SseMessage[] {
    this.buffer += chunk;
    const out: SseMessage[] = [];
    for (;;) {
      const match = /\r\n|\n|\r/.exec(this.buffer);
      if (!match) break;
      // A trailing lone "\r" may be the first half of "\r\n": wait for more input.
      if (match[0] === '\r' && match.index === this.buffer.length - 1) break;
      const line = this.buffer.slice(0, match.index);
      this.buffer = this.buffer.slice(match.index + match[0].length);
      const msg = this.processLine(line);
      if (msg) out.push(msg);
    }
    return out;
  }

  /** Clears state (e.g. before reusing across connections). */
  reset(): void {
    this.buffer = '';
    this.eventType = '';
    this.dataLines = [];
    this.hasData = false;
  }

  private processLine(line: string): SseMessage | null {
    if (line === '') return this.dispatch();
    if (line.startsWith(':')) return null; // comment / keep-alive
    const colon = line.indexOf(':');
    let field: string;
    let value: string;
    if (colon === -1) {
      field = line;
      value = '';
    } else {
      field = line.slice(0, colon);
      value = line.slice(colon + 1);
      if (value.startsWith(' ')) value = value.slice(1);
    }
    switch (field) {
      case 'event':
        this.eventType = value;
        break;
      case 'data':
        this.dataLines.push(value);
        this.hasData = true;
        break;
      case 'id':
        if (!value.includes('\0')) this.lastId = value;
        break;
      default:
        // "retry" and unknown fields are ignored.
        break;
    }
    return null;
  }

  private dispatch(): SseMessage | null {
    if (!this.hasData) {
      this.eventType = '';
      return null;
    }
    const msg: SseMessage = {
      event: this.eventType || 'message',
      data: this.dataLines.join('\n'),
    };
    if (this.lastId !== undefined) msg.id = this.lastId;
    this.eventType = '';
    this.dataLines = [];
    this.hasData = false;
    return msg;
  }
}

/** Exponential backoff delay with ±20% jitter. */
export function backoffDelay(
  attempt: number,
  minMs = 1000,
  maxMs = 30_000,
  rand: () => number = Math.random,
): number {
  const base = Math.min(maxMs, minMs * 2 ** Math.max(0, attempt));
  const jitter = base * 0.2 * (rand() * 2 - 1);
  return Math.max(0, Math.round(base + jitter));
}

export type SseStatus = 'connecting' | 'open' | 'closed';

export interface SseClientOptions {
  url: string;
  /** Returns a current (fresh) access token. Null => not authenticated right now. */
  getToken: () => Promise<string | null>;
  /** Called when the server answers 401. Should refresh the token; return false to stop for good. */
  onUnauthorized?: () => Promise<boolean>;
  onMessage: (msg: SseMessage) => void;
  onStatusChange?: (status: SseStatus) => void;
  minBackoffMs?: number;
  maxBackoffMs?: number;
}

/**
 * SSE client over fetch() so the Authorization header can be sent (EventSource cannot).
 * Reconnects automatically with backoff, obtaining a fresh token for every attempt.
 * Returns a stop() function.
 */
export function startSse(opts: SseClientOptions): () => void {
  const minB = opts.minBackoffMs ?? 1000;
  const maxB = opts.maxBackoffMs ?? 30_000;
  let stopped = false;
  let controller: AbortController | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let attempt = 0;

  const schedule = (delay: number) => {
    if (stopped) return;
    opts.onStatusChange?.('closed');
    timer = setTimeout(() => {
      timer = null;
      void connect();
    }, delay);
  };

  const connect = async (): Promise<void> => {
    if (stopped) return;
    opts.onStatusChange?.('connecting');
    let token: string | null;
    try {
      token = await opts.getToken();
    } catch {
      token = null;
    }
    if (stopped) return;
    if (!token) {
      schedule(backoffDelay(attempt++, minB, maxB));
      return;
    }
    controller = new AbortController();
    try {
      const res = await fetch(opts.url, {
        method: 'GET',
        headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' },
        cache: 'no-store',
        credentials: 'same-origin',
        signal: controller.signal,
      });
      if (res.status === 401) {
        const cont = opts.onUnauthorized ? await opts.onUnauthorized() : true;
        if (!cont) {
          stopped = true;
          opts.onStatusChange?.('closed');
          return;
        }
        schedule(backoffDelay(attempt++, minB, maxB));
        return;
      }
      if (!res.ok || !res.body) {
        schedule(backoffDelay(attempt++, minB, maxB));
        return;
      }
      opts.onStatusChange?.('open');
      attempt = 0;
      const reader = res.body.getReader();
      const decoder = new TextDecoder('utf-8');
      const parser = new SseParser();
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        for (const msg of parser.push(decoder.decode(value, { stream: true }))) {
          try {
            opts.onMessage(msg);
          } catch (err) {
            console.error('SSE handler error', err);
          }
        }
      }
      // Stream ended (e.g. token expired server-side): reconnect soon with a fresh token.
      schedule(backoffDelay(0, 500, 1000));
    } catch {
      if (stopped) return;
      schedule(backoffDelay(attempt++, minB, maxB));
    } finally {
      controller = null;
    }
  };

  void connect();

  return () => {
    stopped = true;
    if (timer) clearTimeout(timer);
    controller?.abort();
    opts.onStatusChange?.('closed');
  };
}
