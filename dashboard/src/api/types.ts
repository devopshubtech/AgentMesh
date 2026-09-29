/**
 * Types for the AgentMesh control-api v1 REST contract (docs/api.md).
 * Field names are snake_case exactly as sent over the wire.
 */

// ---------- Common ----------

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    request_id: string;
    fields?: Record<string, string>;
  };
}

export interface ListResponse<T> {
  items: T[];
  next_cursor: string | null;
}

export interface Actor {
  id: string;
  email: string;
}

// ---------- Auth & users ----------

export type Permission =
  | 'devices.read'
  | 'devices.manage'
  | 'commands.read'
  | 'commands.execute.action'
  | 'commands.execute.exec'
  | 'enrollment.manage'
  | 'users.manage'
  | 'audit.read'
  | 'platform.admin'
  | 'sessions.exit_node';

export type UserStatus = 'active' | 'disabled';
export type RoleName = 'super_admin' | 'admin' | 'operator' | 'viewer';

export interface User {
  id: string;
  email: string;
  display_name: string;
  status: UserStatus;
  role: RoleName;
  /** Permission keys; typed as string because the server may add new ones. */
  permissions: string[];
  created_at: string;
  last_login_at: string | null;
}

export interface LoginRequest {
  email: string;
  password: string;
  client?: 'web' | 'mobile';
}

export interface LoginResponse {
  access_token: string;
  expires_in: number;
  user: User;
  /** Only present when client=mobile. */
  refresh_token?: string;
}

export interface Role {
  name: RoleName | string;
  description: string;
  permissions: string[];
}

export interface CreateUserRequest {
  email: string;
  display_name: string;
  password: string;
  role: string;
}

export interface UpdateUserRequest {
  display_name?: string;
  status?: UserStatus;
  role?: string;
  password?: string;
}

// ---------- Devices ----------

export type DeviceStatus = 'pending' | 'active' | 'disabled' | 'revoked';
export type Connectivity = 'online' | 'offline';
export type Platform = 'linux' | 'windows' | 'darwin' | 'android' | 'ios' | (string & {});

export interface InventoryCpu {
  model: string;
  cores: number;
  threads: number;
}

export interface InventoryMemory {
  total_bytes: number;
  used_bytes: number;
}

export interface InventoryDisk {
  mount: string;
  fstype: string;
  total_bytes: number;
  used_bytes: number;
}

export interface InventoryNetwork {
  name: string;
  mac: string;
  addrs: string[];
}

export interface Inventory {
  cpu: InventoryCpu;
  memory: InventoryMemory;
  disks: InventoryDisk[];
  network: InventoryNetwork[];
  uptime_s: number;
  boot_time: string | null;
  collected_at: string;
}

export interface Device {
  id: string;
  name: string;
  hostname: string;
  status: DeviceStatus;
  connectivity: Connectivity;
  platform: Platform;
  arch: string;
  os_name: string;
  os_version: string;
  os_build: string;
  kernel_version: string;
  agent_version: string;
  capabilities: string[];
  inventory: Inventory | null;
  last_seen_at: string | null;
  last_ip: string | null;
  created_at: string;
  approved_at: string | null;
}

export interface DeviceSummary {
  total: number;
  online: number;
  offline: number;
  pending: number;
  disabled: number;
  revoked: number;
}

export interface DeviceListParams {
  status?: DeviceStatus | '';
  connectivity?: Connectivity | '';
  platform?: string;
  q?: string;
  limit?: number;
}

export interface UpdateDeviceRequest {
  name: string;
}

// ---------- Commands ----------

export type CommandKind = 'exec' | 'action';

export type CommandStatus =
  | 'queued'
  | 'sent'
  | 'acked'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'timed_out'
  | 'canceled'
  | 'expired'
  | 'rejected';

export const TERMINAL_COMMAND_STATUSES: readonly CommandStatus[] = [
  'succeeded',
  'failed',
  'timed_out',
  'canceled',
  'expired',
  'rejected',
];

export function isTerminalStatus(s: CommandStatus): boolean {
  return TERMINAL_COMMAND_STATUSES.includes(s);
}

export interface CommandResult {
  exit_code: number | null;
  stdout: string;
  stderr: string;
  truncated: boolean;
  error: string | null;
  duration_ms: number;
}

export interface Command {
  id: string;
  device_id: string;
  requested_by: Actor;
  kind: CommandKind;
  action: string | null;
  argv: string[] | null;
  shell: boolean;
  timeout_s: number;
  status: CommandStatus;
  created_at: string;
  expires_at: string;
  sent_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  result: CommandResult | null;
}

export type CreateCommandRequest =
  | { kind: 'exec'; argv: string[]; shell: boolean; timeout_s: number }
  | { kind: 'action'; action: string; timeout_s?: number };

// ---------- Enrollment ----------

export interface EnrollmentToken {
  id: string;
  description: string;
  auto_approve: boolean;
  max_uses: number | null;
  uses: number;
  expires_at: string;
  revoked_at: string | null;
  created_at: string;
  created_by: Actor;
}

export interface CreateEnrollmentTokenRequest {
  description: string;
  auto_approve: boolean;
  max_uses: number | null;
  expires_in_s: number;
}

export type CreatedEnrollmentToken = EnrollmentToken & { token: string };

// ---------- Audit ----------

export type ActorType = 'user' | 'device' | 'system';
export type AuditOutcome = 'success' | 'denied' | 'error';

export interface AuditEntry {
  id: number;
  ts: string;
  request_id: string | null;
  actor_type: ActorType;
  actor_id: string | null;
  actor_label: string | null;
  actor_ip: string | null;
  action: string;
  target_type: string | null;
  target_id: string | null;
  outcome: AuditOutcome;
  details: Record<string, unknown>;
}

export interface AuditListParams {
  action?: string;
  actor_id?: string;
  target_id?: string;
  outcome?: AuditOutcome | '';
  from?: string;
  to?: string;
  limit?: number;
}

export interface AuditVerifyResult {
  ok: boolean;
  checked: number;
  first_bad_id: number | null;
}

// ---------- Config ----------

export interface PublicConfig {
  gateway_url: string;
  version: string;
}

// ---------- Live events (SSE) ----------

export interface DeviceStatusEvent {
  device_id: string;
  status: DeviceStatus;
  connectivity: Connectivity;
  last_seen_at: string | null;
}

export interface DeviceUpdatedEvent {
  device_id: string;
}

export interface CommandUpdatedEvent {
  command_id: string;
  device_id: string;
  status: CommandStatus;
}

export interface CommandOutputEvent {
  command_id: string;
  device_id: string;
  stream: 'stdout' | 'stderr';
  seq: number;
  data: string;
}

// ---------------------------------------------------------------- exit-node sessions

export type ExitSessionStatus = 'pending' | 'active' | 'ended';

/** A phone (or other client) routing its traffic through a device. */
export interface ExitSession {
  id: string;
  device_id: string;
  kind: 'exit_node';
  status: ExitSessionStatus;
  user: { id: string; email: string };
  client_ip: string;
  client_label: string;
  created_at: string;
  max_ends_at: string;
  started_at: string | null;
  ended_at: string | null;
  end_reason: string | null;
  bytes_up: number;
  bytes_down: number;
  /** Set when the phone connected with a connect link (QR code). */
  connect_key_label?: string | null;
}

export interface SessionUpdatedEvent {
  session_id: string;
  device_id: string;
  status: ExitSessionStatus;
}
