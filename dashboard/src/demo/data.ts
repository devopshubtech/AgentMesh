import type { AuditEntry, Command, Device, EnrollmentToken, ExitSession, Role, User } from '@/api/types';
import type { ConnectKey, PairCode } from '@/api/connect';

/** Sample data for demo mode. Times are relative to when the demo starts. */

const now = Date.now();
export const ago = (minutes: number) => new Date(now - minutes * 60_000).toISOString();
export const inMinutes = (minutes: number) => new Date(now + minutes * 60_000).toISOString();
const GB = 1024 ** 3;

const ALL_PERMISSIONS = [
  'devices.read',
  'devices.manage',
  'commands.read',
  'commands.execute.action',
  'commands.execute.exec',
  'enrollment.manage',
  'users.manage',
  'audit.read',
  'platform.admin',
  'sessions.exit_node',
];

export const DEMO_USER: User = {
  id: 'demo-user-1',
  email: 'demo@agentmesh.app',
  display_name: 'Demo admin',
  status: 'active',
  role: 'super_admin',
  permissions: ALL_PERMISSIONS,
  created_at: ago(60 * 24 * 30),
  last_login_at: ago(1),
};

const EXIT_CAPS = ['exit_node', 'exec', 'action.ping', 'action.process.list', 'action.inventory.refresh'];

function inventory(cpu: string, cores: number, memGB: number, usedGB: number, diskGB: number, diskUsedGB: number, ip: string) {
  return {
    cpu: { model: cpu, cores, threads: cores * 2 },
    memory: { total_bytes: memGB * GB, used_bytes: usedGB * GB },
    disks: [{ mount: '/', fstype: 'apfs', total_bytes: diskGB * GB, used_bytes: diskUsedGB * GB }],
    network: [{ name: 'en0', mac: '3c:22:fb:12:4a:9e', addrs: [ip] }],
    uptime_s: 6 * 86400 + 3600 * 5,
    boot_time: ago(60 * 24 * 6),
    collected_at: ago(2),
  };
}

export function demoDevices(): Device[] {
  return [
    {
      id: 'dev-mac-mini',
      name: 'Office Mac mini',
      hostname: 'mac-mini.local',
      status: 'active',
      connectivity: 'online',
      platform: 'darwin',
      arch: 'arm64',
      os_name: 'macOS',
      os_version: '15.1',
      os_build: '24B83',
      kernel_version: 'Darwin 24.1.0',
      agent_version: '0.6.4',
      capabilities: EXIT_CAPS,
      inventory: inventory('Apple M4', 10, 16, 7, 512, 188, '192.168.1.20'),
      last_seen_at: ago(0.2),
      last_ip: '49.205.112.37',
      created_at: ago(60 * 24 * 3),
      approved_at: ago(60 * 24 * 3),
    },
    {
      id: 'dev-windows',
      name: 'Home Windows PC',
      hostname: 'DESKTOP-HOME',
      status: 'active',
      connectivity: 'online',
      platform: 'windows',
      arch: 'amd64',
      os_name: 'Windows 11 Pro',
      os_version: '24H2',
      os_build: '26100.2314',
      kernel_version: '10.0.26100',
      agent_version: '0.6.4',
      capabilities: EXIT_CAPS,
      inventory: inventory('Intel Core i7-12700', 12, 32, 14, 1024, 610, '192.168.0.14'),
      last_seen_at: ago(0.5),
      last_ip: '103.21.58.140',
      created_at: ago(60 * 24 * 12),
      approved_at: ago(60 * 24 * 12),
    },
    {
      id: 'dev-ubuntu',
      name: 'Cloud server (Ubuntu)',
      hostname: 'ubuntu-vps-1',
      status: 'active',
      connectivity: 'offline',
      platform: 'linux',
      arch: 'amd64',
      os_name: 'Ubuntu',
      os_version: '24.04 LTS',
      os_build: '',
      kernel_version: '6.8.0-45-generic',
      agent_version: '0.6.3',
      capabilities: EXIT_CAPS,
      inventory: inventory('AMD EPYC 7B13', 2, 4, 1, 80, 22, '10.0.0.5'),
      last_seen_at: ago(95),
      last_ip: '34.93.211.6',
      created_at: ago(60 * 24 * 20),
      approved_at: ago(60 * 24 * 20),
    },
    {
      id: 'dev-pending',
      name: 'Raspberry Pi',
      hostname: 'raspberrypi',
      status: 'pending',
      connectivity: 'online',
      platform: 'linux',
      arch: 'arm64',
      os_name: 'Debian GNU/Linux',
      os_version: '12',
      os_build: '',
      kernel_version: '6.6.51-v8+',
      agent_version: '0.6.4',
      capabilities: ['action.ping', 'action.inventory.refresh'],
      inventory: inventory('Cortex-A76', 4, 8, 2, 64, 11, '192.168.1.42'),
      last_seen_at: ago(1),
      last_ip: '49.205.112.37',
      created_at: ago(12),
      approved_at: null,
    },
  ];
}

export function demoSessions(): ExitSession[] {
  const base = { kind: 'exit_node' as const, user: { id: DEMO_USER.id, email: 'phone link' }, end_reason: null, ended_at: null };
  return [
    {
      ...base,
      id: 'sess-1',
      device_id: 'dev-mac-mini',
      status: 'active',
      client_ip: '106.200.14.77',
      client_label: 'Android Samsung SM-S918B',
      created_at: ago(42),
      started_at: ago(42),
      max_ends_at: inMinutes(60 * 12 - 42),
      bytes_up: 48 * 1024 ** 2,
      bytes_down: 1.6 * GB,
      connect_key_label: 'Family',
    },
    {
      ...base,
      id: 'sess-2',
      device_id: 'dev-mac-mini',
      status: 'active',
      client_ip: '157.48.3.201',
      client_label: 'Android Google Pixel 8',
      created_at: ago(7),
      started_at: ago(7),
      max_ends_at: inMinutes(60 * 12 - 7),
      bytes_up: 3 * 1024 ** 2,
      bytes_down: 120 * 1024 ** 2,
      connect_key_label: 'Pairing code',
    },
    {
      ...base,
      id: 'sess-3',
      device_id: 'dev-windows',
      status: 'ended',
      client_ip: '106.200.14.77',
      client_label: 'Android OnePlus 12',
      created_at: ago(300),
      started_at: ago(300),
      ended_at: ago(180),
      end_reason: 'client disconnected',
      max_ends_at: ago(300 - 720),
      bytes_up: 20 * 1024 ** 2,
      bytes_down: 900 * 1024 ** 2,
      connect_key_label: 'Office',
    },
  ];
}

export function demoConnectKeys(): ConnectKey[] {
  return [
    {
      id: 'ck-1',
      device_id: 'dev-mac-mini',
      label: 'Family',
      created_at: ago(60 * 24 * 2),
      expires_at: null,
      revoked_at: null,
      uses: 9,
      last_used_at: ago(42),
      created_by: DEMO_USER.email,
      active_sessions: 1,
    },
    {
      id: 'ck-2',
      device_id: 'dev-windows',
      label: 'Office',
      created_at: ago(60 * 24 * 6),
      expires_at: inMinutes(60 * 24),
      revoked_at: null,
      uses: 3,
      last_used_at: ago(300),
      created_by: DEMO_USER.email,
      active_sessions: 0,
    },
  ];
}

export function demoPairCodes(): PairCode[] {
  return [];
}

export function demoTokens(): EnrollmentToken[] {
  const by = { id: DEMO_USER.id, email: DEMO_USER.email };
  return [
    {
      id: 'tok-1',
      description: 'Office laptops',
      auto_approve: true,
      max_uses: 10,
      uses: 2,
      expires_at: '9999-12-31T00:00:00Z',
      revoked_at: null,
      created_at: ago(60 * 24 * 12),
      created_by: by,
    },
    {
      id: 'tok-2',
      description: 'Raspberry Pi (needs approval)',
      auto_approve: false,
      max_uses: 1,
      uses: 1,
      expires_at: inMinutes(60 * 24),
      revoked_at: null,
      created_at: ago(30),
      created_by: by,
    },
  ];
}

export function demoUsers(): User[] {
  return [
    DEMO_USER,
    {
      id: 'demo-user-2',
      email: 'operator@example.com',
      display_name: 'Support operator',
      status: 'active',
      role: 'operator',
      permissions: ['devices.read', 'commands.read', 'commands.execute.action', 'sessions.exit_node'],
      created_at: ago(60 * 24 * 10),
      last_login_at: ago(60 * 5),
    },
    {
      id: 'demo-user-3',
      email: 'viewer@example.com',
      display_name: 'Read-only viewer',
      status: 'disabled',
      role: 'viewer',
      permissions: ['devices.read', 'audit.read'],
      created_at: ago(60 * 24 * 15),
      last_login_at: null,
    },
  ];
}

export const DEMO_ROLES: Role[] = [
  { name: 'super_admin', description: 'Everything, including platform settings', permissions: ALL_PERMISSIONS },
  {
    name: 'admin',
    description: 'Manage devices, users, enrollment and phone links',
    permissions: ALL_PERMISSIONS.filter((p) => p !== 'platform.admin'),
  },
  {
    name: 'operator',
    description: 'Run actions and connect phones',
    permissions: ['devices.read', 'commands.read', 'commands.execute.action', 'sessions.exit_node'],
  },
  { name: 'viewer', description: 'Read-only access', permissions: ['devices.read', 'audit.read'] },
];

export function demoCommands(): Command[] {
  const by = { id: DEMO_USER.id, email: DEMO_USER.email };
  return [
    {
      id: 'cmd-1',
      device_id: 'dev-mac-mini',
      requested_by: by,
      kind: 'action',
      action: 'ping',
      argv: null,
      shell: false,
      timeout_s: 30,
      status: 'succeeded',
      created_at: ago(30),
      expires_at: ago(25),
      sent_at: ago(30),
      started_at: ago(30),
      finished_at: ago(30),
      result: { exit_code: 0, stdout: 'pong\n', stderr: '', truncated: false, error: null, duration_ms: 12 },
    },
  ];
}

let auditId = 1000;
export function auditEntry(
  action: string,
  minutesAgo: number,
  target: { type: string; id: string } | null,
  details: Record<string, unknown> = {},
  actor: Partial<AuditEntry> = {},
): AuditEntry {
  auditId += 1;
  return {
    id: auditId,
    ts: ago(minutesAgo),
    request_id: null,
    actor_type: 'user',
    actor_id: DEMO_USER.id,
    actor_label: DEMO_USER.email,
    actor_ip: '49.205.112.37',
    action,
    target_type: target?.type ?? null,
    target_id: target?.id ?? null,
    outcome: 'success',
    details,
    ...actor,
  };
}

export function demoAudit(): AuditEntry[] {
  const device = (id: string) => ({ type: 'device', id });
  return [
    auditEntry('exit_session.start', 7, device('dev-mac-mini'), { client: 'Android Google Pixel 8', via: 'pairing code' }),
    auditEntry('pair_code.create', 9, device('dev-mac-mini'), { label: 'Pairing code' }),
    auditEntry('device.enroll', 12, device('dev-pending'), { name: 'Raspberry Pi', auto_approved: false }, {
      actor_type: 'device',
      actor_label: 'raspberrypi',
    }),
    auditEntry('command.create', 30, device('dev-mac-mini'), { kind: 'action', action: 'ping' }),
    auditEntry('exit_session.start', 42, device('dev-mac-mini'), { client: 'Android Samsung SM-S918B', via: 'Family' }),
    auditEntry('exit_session.end', 180, device('dev-windows'), { reason: 'client disconnected' }),
    auditEntry('connect_key.create', 60 * 24 * 2, device('dev-mac-mini'), { label: 'Family' }),
    auditEntry('device.approve', 60 * 24 * 3, device('dev-mac-mini'), { name: 'Office Mac mini' }),
    auditEntry('enrollment_token.create', 60 * 24 * 12, { type: 'enrollment_token', id: 'tok-1' }, { description: 'Office laptops' }),
    auditEntry('user.login', 1, null, { client: 'web' }),
  ].sort((a, b) => b.ts.localeCompare(a.ts));
}
