import { ApiError, type RequestOptions } from '@/api/client';
import type { Command, Device, EnrollmentToken, User } from '@/api/types';
import type { ConnectKey, PairCode } from '@/api/connect';
import {
  DEMO_ROLES,
  DEMO_USER,
  ago,
  auditEntry,
  demoAudit,
  demoCommands,
  demoConnectKeys,
  demoDevices,
  demoPairCodes,
  demoSessions,
  demoTokens,
  demoUsers,
  inMinutes,
} from './data';

/**
 * The control-api, simulated in the browser for demo mode. Changes (new QR
 * links, pairing codes, tokens, renames...) live in memory for this tab only.
 */
const db = {
  devices: demoDevices(),
  sessions: demoSessions(),
  keys: demoConnectKeys(),
  pairCodes: demoPairCodes(),
  tokens: demoTokens(),
  users: demoUsers(),
  commands: demoCommands(),
  audit: demoAudit(),
};

const DEMO_SERVER = 'https://demo.agentmesh.app';
let seq = 0;
const newId = (p: string) => `${p}-demo-${++seq}`;
const randomKey = (len: number) => {
  const abc = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789';
  return Array.from({ length: len }, () => abc[Math.floor(Math.random() * abc.length)]).join('');
};

function notFound(): never {
  throw new ApiError({ status: 404, code: 'not_found', message: 'Not found (demo)', requestId: null });
}

function list<T>(items: T[]) {
  return { items, next_cursor: null };
}

function device(id: string): Device {
  return db.devices.find((d) => d.id === id) ?? notFound();
}

function log(action: string, target: { type: string; id: string } | null, details: Record<string, unknown> = {}) {
  db.audit.unshift(auditEntry(action, 0, target, details));
}

function body<T>(opts: RequestOptions): T {
  return (opts.body ?? {}) as T;
}

/** Output for the demo devices' actions and commands. */
function runCommand(d: Device, cmd: Command): Command {
  const out: Record<string, string> = {
    ping: 'pong\n',
    'inventory.refresh': 'inventory collected\n',
    'process.list':
      'PID   NAME                 CPU%  MEM\n1     launchd               0.1   12 MB\n412   agentmesh-agent       0.4   24 MB\n518   cloudflared           0.2   38 MB\n733   Safari                3.1  410 MB\n',
  };
  const stdout =
    cmd.kind === 'action'
      ? (out[cmd.action ?? ''] ?? 'done\n')
      : `$ ${(cmd.argv ?? []).join(' ')}\n${d.hostname}: command finished (demo output)\n`;
  return {
    ...cmd,
    status: 'succeeded',
    sent_at: cmd.created_at,
    started_at: cmd.created_at,
    finished_at: new Date().toISOString(),
    result: { exit_code: 0, stdout, stderr: '', truncated: false, error: null, duration_ms: 40 + Math.floor(Math.random() * 200) },
  };
}

export async function demoApi<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  await new Promise((r) => setTimeout(r, 120 + Math.random() * 180)); // feels like a network
  return route(path, opts) as T;
}

function route(path: string, opts: RequestOptions): unknown {
  const method = opts.method ?? 'GET';
  const q = opts.query ?? {};
  const seg = (path.split('?')[0] ?? '').split('/').filter(Boolean).map(decodeURIComponent);
  const [a, b, c] = seg;

  if (a === 'config') return { gateway_url: DEMO_SERVER, version: '0.6.4 (demo)' };
  if (a === 'roles') return DEMO_ROLES;

  // ---------------------------------------------------------------- devices
  if (a === 'devices' && !b) {
    let items = db.devices;
    if (q.status) items = items.filter((d) => d.status === q.status);
    if (q.connectivity) items = items.filter((d) => d.connectivity === q.connectivity);
    if (q.platform) items = items.filter((d) => d.platform === q.platform);
    if (q.q) {
      const s = String(q.q).toLowerCase();
      items = items.filter((d) => `${d.name} ${d.hostname} ${d.last_ip}`.toLowerCase().includes(s));
    }
    return list(items);
  }
  if (a === 'devices' && b === 'summary') {
    const d = db.devices;
    return {
      total: d.length,
      online: d.filter((x) => x.connectivity === 'online').length,
      offline: d.filter((x) => x.connectivity === 'offline').length,
      pending: d.filter((x) => x.status === 'pending').length,
      disabled: d.filter((x) => x.status === 'disabled').length,
      revoked: d.filter((x) => x.status === 'revoked').length,
    };
  }
  if (a === 'devices' && b && !c) {
    const d = device(b);
    if (method === 'PATCH') {
      d.name = body<{ name: string }>(opts).name || d.name;
      log('device.update', { type: 'device', id: d.id }, { name: d.name });
    }
    return d;
  }
  if (a === 'devices' && b && c && ['approve', 'disable', 'enable', 'revoke'].includes(c)) {
    const d = device(b);
    d.status = c === 'approve' || c === 'enable' ? 'active' : c === 'disable' ? 'disabled' : 'revoked';
    if (c === 'approve') {
      d.approved_at = new Date().toISOString();
      d.capabilities = [...new Set([...d.capabilities, 'exit_node', 'exec', 'action.process.list'])];
    }
    log(`device.${c}`, { type: 'device', id: d.id }, { name: d.name });
    return d;
  }
  if (a === 'devices' && b && c === 'activity') return list(db.audit.filter((e) => e.target_id === b));
  if (a === 'devices' && b && c === 'commands') {
    const d = device(b);
    if (method === 'POST') {
      const req = body<{ kind: 'exec' | 'action'; action?: string; argv?: string[]; shell?: boolean; timeout_s?: number }>(opts);
      const cmd = runCommand(d, {
        id: newId('cmd'),
        device_id: d.id,
        requested_by: { id: DEMO_USER.id, email: DEMO_USER.email },
        kind: req.kind,
        action: req.action ?? null,
        argv: req.argv ?? null,
        shell: !!req.shell,
        timeout_s: req.timeout_s ?? 60,
        status: 'queued',
        created_at: new Date().toISOString(),
        expires_at: inMinutes(5),
        sent_at: null,
        started_at: null,
        finished_at: null,
        result: null,
      });
      db.commands.unshift(cmd);
      log('command.create', { type: 'device', id: d.id }, { kind: cmd.kind, action: cmd.action });
      return cmd;
    }
    return list(db.commands.filter((x) => x.device_id === b));
  }
  if (a === 'commands' && b) {
    const cmd = db.commands.find((x) => x.id === b) ?? notFound();
    if (c === 'cancel') cmd.status = 'canceled';
    return cmd;
  }

  // ---------------------------------------------------------------- phone connections
  if (a === 'devices' && b && c === 'connect-keys') {
    device(b);
    if (method === 'POST') {
      const req = body<{ label?: string; expires_in_s?: number }>(opts);
      const key = `amk_demo_${randomKey(32)}`;
      const k: ConnectKey = {
        id: newId('ck'),
        device_id: b,
        label: req.label || 'Phone link',
        created_at: new Date().toISOString(),
        expires_at: req.expires_in_s ? new Date(Date.now() + req.expires_in_s * 1000).toISOString() : null,
        revoked_at: null,
        uses: 0,
        last_used_at: null,
        created_by: DEMO_USER.email,
        active_sessions: 0,
      };
      db.keys.unshift(k);
      log('connect_key.create', { type: 'device', id: b }, { label: k.label });
      return { ...k, key, link: `${DEMO_SERVER}/join.html#k=${key}` };
    }
    return list(db.keys.filter((k) => k.device_id === b));
  }
  if (a === 'connect-keys' && b && method === 'DELETE') {
    const k = db.keys.find((x) => x.id === b) ?? notFound();
    k.revoked_at = new Date().toISOString();
    db.sessions.forEach((s) => {
      if (s.device_id === k.device_id && s.connect_key_label === k.label && s.status === 'active') {
        s.status = 'ended';
        s.ended_at = new Date().toISOString();
        s.end_reason = 'link revoked';
      }
    });
    k.active_sessions = 0;
    return undefined;
  }
  if (a === 'devices' && b && c === 'pair-codes') {
    device(b);
    if (method === 'POST') {
      const code = String(Math.floor(100000 + Math.random() * 900000));
      const p: PairCode = {
        id: newId('pc'),
        device_id: b,
        label: body<{ label?: string }>(opts).label || 'Pairing code',
        created_at: new Date().toISOString(),
        expires_at: inMinutes(15),
        revoked_at: null,
        uses: 0,
      };
      db.pairCodes.unshift(p);
      log('pair_code.create', { type: 'device', id: b }, { label: p.label });
      return { ...p, code };
    }
    return list(db.pairCodes.filter((p) => p.device_id === b));
  }
  if (a === 'pair-codes' && b && method === 'DELETE') {
    const p = db.pairCodes.find((x) => x.id === b) ?? notFound();
    p.revoked_at = new Date().toISOString();
    return undefined;
  }
  if (a === 'exit-sessions') {
    if (b && method === 'DELETE') {
      const s = db.sessions.find((x) => x.id === b) ?? notFound();
      s.status = 'ended';
      s.ended_at = new Date().toISOString();
      s.end_reason = 'disconnected by admin';
      log('exit_session.end', { type: 'device', id: s.device_id }, { reason: s.end_reason });
      return s;
    }
    return list(db.sessions.filter((s) => !q.device_id || s.device_id === q.device_id));
  }

  // ---------------------------------------------------------------- enrollment
  if (a === 'enrollment-tokens') {
    if (b && method === 'DELETE') {
      const t = db.tokens.find((x) => x.id === b) ?? notFound();
      t.revoked_at = new Date().toISOString();
      return undefined;
    }
    if (method === 'POST') {
      const req = body<{ description: string; auto_approve: boolean; max_uses: number | null; expires_in_s: number }>(opts);
      const t: EnrollmentToken = {
        id: newId('tok'),
        description: req.description,
        auto_approve: req.auto_approve,
        max_uses: req.max_uses,
        uses: 0,
        expires_at: req.expires_in_s === -1 ? '9999-12-31T00:00:00Z' : new Date(Date.now() + req.expires_in_s * 1000).toISOString(),
        revoked_at: null,
        created_at: new Date().toISOString(),
        created_by: { id: DEMO_USER.id, email: DEMO_USER.email },
      };
      db.tokens.unshift(t);
      log('enrollment_token.create', { type: 'enrollment_token', id: t.id }, { description: t.description });
      return { ...t, token: `am_enr_demo_${randomKey(40)}` };
    }
    return list(db.tokens);
  }

  // ---------------------------------------------------------------- audit
  if (a === 'audit' && b === 'verify') return { ok: true, checked: db.audit.length, first_bad_id: null };
  if (a === 'audit') {
    let items = db.audit;
    if (q.action) items = items.filter((e) => e.action.includes(String(q.action)));
    if (q.outcome) items = items.filter((e) => e.outcome === q.outcome);
    if (q.target_id) items = items.filter((e) => e.target_id === q.target_id);
    return list(items);
  }

  // ---------------------------------------------------------------- users
  if (a === 'users') {
    if (b && method === 'DELETE') {
      db.users = db.users.filter((u) => u.id !== b);
      return undefined;
    }
    if (b && method === 'PATCH') {
      const u = db.users.find((x) => x.id === b) ?? notFound();
      const req = body<Partial<User>>(opts);
      Object.assign(u, {
        display_name: req.display_name ?? u.display_name,
        status: req.status ?? u.status,
        role: req.role ?? u.role,
      });
      u.permissions = DEMO_ROLES.find((r) => r.name === u.role)?.permissions ?? u.permissions;
      return u;
    }
    if (method === 'POST') {
      const req = body<{ email: string; display_name: string; role: string }>(opts);
      const u: User = {
        id: newId('user'),
        email: req.email,
        display_name: req.display_name,
        status: 'active',
        role: req.role as User['role'],
        permissions: DEMO_ROLES.find((r) => r.name === req.role)?.permissions ?? [],
        created_at: new Date().toISOString(),
        last_login_at: null,
      };
      db.users.push(u);
      return u;
    }
    return list(db.users);
  }

  // Anything else (e.g. a feature added later) answers with an empty result.
  if (method === 'GET') return list([]);
  return { ok: true, at: ago(0) };
}
