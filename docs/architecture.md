# AgentMesh — Platform Architecture

Status: **Approved. Phase 1 (MVP) implemented.** · Version 0.2 · 2026-09-28. See [Implementation notes](#implementation-notes-mvp) for where the code differs from this plan.

AgentMesh is a cross-platform device management and secure remote-access platform. Devices run a lightweight **agent** that dials **out** to a central **control plane**. Administrators use a **web dashboard** or an **Android control app** to see and manage those devices. The only idea taken from Tailscale is the general one: outbound-connected agents plus a central coordinator. The protocol, identity model, data model and UI here are our own. AgentMesh is not a mesh VPN and does not build peer-to-peer tunnels. It is a brokered control plane.

---

## Table of contents

1. [High-level architecture](#1-high-level-architecture)
2. [Detailed component architecture](#2-detailed-component-architecture)
3. [Device-agent architecture](#3-device-agent-architecture)
4. [Backend architecture](#4-backend-architecture)
5. [Communication protocol design](#5-communication-protocol-design)
6. [Database schema](#6-database-schema)
7. [Authentication and authorization flow](#7-authentication-and-authorization-flow)
8. [Device registration flow](#8-device-registration-flow)
9. [Command execution flow](#9-command-execution-flow)
10. [Remote-session architecture](#10-remote-session-architecture)
11. [Artifact/package distribution architecture](#11-artifactpackage-distribution-architecture)
12. [Agent update architecture](#12-agent-update-architecture)
13. [Web dashboard architecture](#13-web-dashboard-architecture)
14. [Android control-app architecture](#14-android-control-app-architecture)
15. [Platform capability matrix](#15-platform-capability-matrix)
16. [Security model](#16-security-model)
17. [Kubernetes deployment architecture](#17-kubernetes-deployment-architecture)
18. [Monorepo structure](#18-monorepo-structure)
19. [Phase-by-phase implementation plan](#19-phase-by-phase-implementation-plan)
20. [MVP implementation plan](#20-mvp-implementation-plan)
- [Appendix A — Technology recommendations](#appendix-a--technology-recommendations)
- [Appendix B — Local development ports](#appendix-b--local-development-ports)
- [Appendix C — Open decisions for review](#appendix-c--open-decisions-for-review)

---

## 1. High-level architecture

```text
   Operators                                              Managed devices
 ┌───────────────┐  ┌──────────────────┐        ┌────────┐ ┌─────────┐ ┌───────┐
 │ Web dashboard │  │ Android control  │        │ Linux  │ │ Windows │ │ macOS │ ...
 │ (browser)     │  │ app              │        │ agent  │ │ agent   │ │ agent │
 └──────┬────────┘  └────────┬─────────┘        └───┬────┘ └────┬────┘ └───┬───┘
        │ HTTPS (REST + SSE) │                       │  outbound WSS only   │
        └─────────┬──────────┘                       └──────────┬───────────┘
                  ▼                                              ▼
        ┌───────────────────┐                        ┌──────────────────────┐
        │  L7 ingress / WAF │                        │  L4 load balancer    │
        └─────────┬─────────┘                        └──────────┬───────────┘
                  ▼                                              ▼
        ┌───────────────────┐    NATS (events,       ┌──────────────────────┐
        │  control-api  xN  │◄──  command routing,  ─►│  agent-gateway  xN   │
        │  (stateless)      │    presence)           │  (holds WS conns)    │
        └───┬──────────┬────┘                        └──────────┬───────────┘
            │          │                                        │
            │   ┌──────▼──────┐   ┌──────────────┐              │
            │   │ worker  xN  │   │ session-relay│◄─────────────┘ (Phase 4)
            │   │ (jobs, GC,  │   │ xN (terminal │
            │   │  rollouts)  │   │  / tunnels)  │
            │   └──────┬──────┘   └──────────────┘
            ▼          ▼
     ┌─────────────────────┐        ┌──────────────────────────────┐
     │ PostgreSQL (primary │        │ Object storage (S3 / MinIO)  │
     │ + replicas)         │        │ artifacts, logs, recordings  │
     └─────────────────────┘        └──────────────────────────────┘
```

**Key properties**

| Property | How it is achieved |
|---|---|
| No inbound ports on devices | The agent only ever makes outbound TLS connections to port 443. SSH, RDP and ADB are never exposed. |
| Horizontal scale | Agent connections are spread across N stateless `agent-gateway` pods. NATS routes messages to whichever pod holds a given device's connection. |
| No single connection owner | Any gateway can serve any device. If a pod dies, its agents reconnect elsewhere within seconds. |
| Source of truth | PostgreSQL holds all durable state. NATS is used only for routing and notification and can be lost without losing data. |
| Defense in depth | TLS on the transport, device key proof-of-possession, commands signed by the backend and checked by the agent, RBAC, and a tamper-evident audit log. |

---

## 2. Detailed component architecture

| Component | Responsibility | State | Scales by |
|---|---|---|---|
| **control-api** | REST API for the dashboard and mobile app. Handles user auth, RBAC, devices, commands, artifacts, audit and users. Pushes live updates to clients over SSE. | Stateless | Adding pods (HPA on CPU/RPS) |
| **agent-gateway** | Terminates agent WebSockets. Authenticates the device token, runs the per-connection read/write loops, handles heartbeats, and delivers commands and collects results. | Connections in memory | Adding pods (HPA on connection count) |
| **worker** | Background jobs: offline reaper, command expiry, rollout orchestration, retention/GC, metric rollups, notification fan-out. | Stateless; jobs run under Postgres advisory locks | Adding pods |
| **session-relay** *(Phase 4)* | Brokers interactive byte streams (PTY, TCP forward) between an operator client and an agent data channel. | Streams in memory | Adding pods |
| **NATS** | Subjects `dev.<id>.cmd`, `dev.<id>.ctl`, `evt.*`. JetStream is added later for durable event streams. | Ephemeral routing | 3-node cluster |
| **PostgreSQL** | Durable state and audit. Heartbeats and metrics go in partitioned tables. | Durable | Vertical, read replicas, partitioning, TimescaleDB later |
| **Object storage** | Agent artifacts, large command output, log archives, session recordings. | Durable | Unbounded |

The "services" named in the brief (Auth, Device, Command, Artifact, Audit, Connection) are **Go modules (packages) with explicit interfaces inside one codebase**. They ship as **three binaries** from one repo: `control-api`, `agent-gateway` and `worker`.

Why not microservices on day one?
- Every service would need its own database, network hops, versioning and deployment. That is a heavy tax while the domain is still changing.
- The piece that genuinely has a different scaling profile, long-lived agent connections, is already split out as its own binary.
- Package boundaries (`internal/devices` never imports `internal/commands` internals; they talk through interfaces) mean a module can be extracted into its own service later without a rewrite.

---

## 3. Device-agent architecture

### 3.1 Principles

- There is **one protocol and several implementations**. The desktop agents (Linux, Windows, macOS) share a Go core. The Android and iOS agents are native (Kotlin and Swift), because their OS models are fundamentally different (see §15).
- The agent is **least-privilege and capability-declared**. On connect it advertises what it can do (`exec`, `pty`, `inventory.packages`, `update.self`...). The backend never sends a command type the agent did not declare.
- There is a **local policy override**. A root/admin-owned config file on the device can disable capabilities, for example `exec: false`. The backend cannot override it. This gives device owners a trust anchor.

### 3.2 Desktop agent (Go) internal structure

```text
agentmesh-agent
├── cmd/agent (main)          CLI: run | enroll | status | uninstall | version
├── core/
│   ├── identity      keypair generation, secure storage adapter, device_id
│   ├── enroll        one-time enrollment with an enrollment token
│   ├── auth          challenge/response → short-lived device access token
│   ├── conn          WSS client: dial, backoff+jitter, ping/pong, read/write pumps
│   ├── protocol      protobuf envelope encode/decode, version negotiation
│   ├── heartbeat     periodic heartbeat + change-driven inventory
│   ├── inventory     OS, CPU, RAM, disks, NICs, uptime (gopsutil) + platform hooks
│   ├── commands      verifier (signature, expiry, replay) → dispatcher → executors
│   ├── executors     exec (argv, no shell by default), builtin actions, cancel, timeout
│   ├── updater       (P2) download → verify → stage → swap → health-confirm/rollback
│   ├── policy        local policy file, capability gating
│   └── logging       structured logs, rotation, optional ship-to-backend (P2)
└── platform/ (build tags)
    ├── linux     systemd unit, /etc/machine-id, /var/lib/agentmesh, dpkg/rpm inventory
    ├── windows   Windows Service (x/sys/windows/svc), DPAPI key store, MachineGuid, registry inventory
    └── darwin    launchd daemon, Keychain, IOPlatformUUID, pkgutil inventory
```

### 3.3 Agent lifecycle state machine

```text
 ┌───────────┐ enroll ok ┌──────────┐ approved ┌────────────┐ token ok ┌───────────┐
 │UNENROLLED ├──────────►│ PENDING  ├─────────►│AUTHENTICATE├─────────►│ CONNECTED │
 └───────────┘           │(polls w/ │          └─────▲──────┘          └─────┬─────┘
                         │ backoff) │                │ reconnect             │ drop / error
                         └──────────┘                │ (backoff+jitter)      ▼
                                                     └───────────────── ┌──────────┐
                            revoked / disabled ────────────────────────►│ HALTED   │
                            (server sends Disconnect{REVOKED})          │ (no retry│
                                                                        │  storm)  │
                                                                        └──────────┘
```

- **Reconnect** uses exponential backoff with *full jitter*: base 1 s, cap 5 min. If the server returns `retry_after` (for example when a gateway is overloaded), the agent honours it. This prevents a thundering herd after a gateway restart with 100k agents.
- **Offline behaviour.** Command results that could not be delivered are queued in a small bounded on-disk outbox (MVP: in memory) and flushed on reconnect. The agent never executes commands from the queue after reconnecting; only *results* are replayed.
- **Clock handling.** The server sends `server_time` in `Welcome`. The agent keeps an offset and uses it when checking command expiry, so a skewed local clock does not break authorization.

### 3.4 Secure key storage per platform

| Platform | Device private key storage | Hardware backing (when available) |
|---|---|---|
| Linux | `/var/lib/agentmesh/identity.key`, mode 0600, owned by root | TPM 2.0 via `go-tpm` (Phase 4) |
| Windows | DPAPI (machine scope) encrypted blob under `%ProgramData%\AgentMesh`, ACL SYSTEM+Administrators | CNG Platform Crypto Provider (TPM) (Phase 4) |
| macOS | System Keychain item, accessible only to the daemon | Secure Enclave (P-256 only) |
| Android | Android Keystore, non-exportable key | StrongBox / TEE |
| iOS | Keychain `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` | Secure Enclave |

Because Secure Enclave and many TPMs only support P-256, **device keys use ECDSA P-256**. Ed25519 is used for backend-side signing keys, which live in our KMS.

---

## 4. Backend architecture

### 4.1 Layering (inside each module)

```text
transport (HTTP handlers / WS handlers)   – decode, validate, authz check, encode
      │
service (use cases)                       – business rules, transactions, emits events
      │
repository (sqlc-generated queries)       – SQL only, no business logic
      │
PostgreSQL
```

Cross-cutting concerns are provided through middleware and interfaces: `authn`, `authz.Require(perm)`, `audit.Record(ctx, event)`, `ratelimit`, `requestid`, `slog` logging, OpenTelemetry tracing and Prometheus metrics.

### 4.2 Modules

| Module | Owns tables | Key interfaces |
|---|---|---|
| `auth` | users, roles, permissions, user_roles, user_sessions | `Authenticator`, `TokenIssuer`, `Authorizer` |
| `orgs` | organizations | multi-tenant scoping (`org_id` on every row from day one) |
| `enrollment` | enrollment_tokens | issue/redeem tokens |
| `devices` | devices, device_credentials, device_groups, device_group_members | register, approve, revoke, disable, inventory |
| `presence` | device_sessions, device_heartbeats | online/offline, last_seen |
| `commands` | device_commands, command_results | create, sign, dispatch, collect, expire |
| `artifacts` | artifacts, artifact_versions, agent_versions, device_artifacts | publish, sign, rollout |
| `sessions` | remote_sessions | authorize, broker (Phase 4) |
| `audit` | audit_logs | append-only, hash-chained |
| `bus` | — | `Publish/Subscribe` over NATS |

### 4.3 How a command reaches the right gateway

```text
control-api                     NATS                        agent-gateway #7 (holds device D)
  │ INSERT device_commands        │                                 │
  │ (status=QUEUED)               │                                 │
  │ publish dev.D.cmd {id} ──────►│────── subscribed on connect ───►│ load cmd from DB, send to agent
  │                               │                                 │
```

- When a device connects, its gateway subscribes to `dev.<device_id>.>`. It unsubscribes on disconnect.
- If nobody is subscribed (the device is offline), the command stays `QUEUED` in Postgres. On connect, the gateway **pulls pending, unexpired commands** for that device. The database is the delivery guarantee; NATS only lowers latency.
- Duplicate connections (the same device connecting to two gateways) are resolved by `device_sessions`. The newest session wins, and the older gateway receives `dev.D.ctl {kick}` and closes its connection.

### 4.4 Presence and heartbeat write volume

With 100k devices and a 30 s heartbeat, the fleet sends about 3,300 heartbeats per second. Doing an `UPDATE devices` for each one would bloat the table. Instead:
- The gateway keeps `last_seen` in memory and **flushes in batches** every 10 s with a single `UPDATE ... FROM unnest($ids, $ts)`.
- Online/offline changes are event-driven: the connect/disconnect callbacks write immediately and publish `evt.device.status`.
- The `worker` reaper marks devices OFFLINE when `last_seen_at < now() - 90s` and they have no live session. This covers gateways that crash without running their disconnect hooks.
- `device_heartbeats` stores a **sampled** record (at most one per 5 min per device) for history. It is range-partitioned by day with 30-day retention.

---

## 5. Communication protocol design

### 5.1 Protocol choice: WebSocket over TLS (WSS) carrying Protobuf frames

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **WebSocket (WSS) + Protobuf** | Runs on port 443 through corporate HTTP proxies and TLS-inspecting firewalls. Works behind any L4/L7 load balancer. Native support in every language, including browsers for future web-based terminals. Simple full-duplex framing. | Multiplexing and flow control have to be done ourselves (simple for our needs). | **Chosen** |
| gRPC bidirectional streaming | Strong typing, built-in flow control and multiplexing, code generation. | Needs HTTP/2 end to end, which often breaks through corporate proxies and some L7 load balancers. Browsers cannot use it without grpc-web. Heavier on mobile. | Alternative. The Protobuf schema allows moving to it later. |
| MQTT | Designed for IoT, supports QoS, very small. | Needs a broker; its pub/sub model does not fit request/response or streams; the ACL model is weaker. | Good for constrained IoT devices; possible later add-on. |
| Long-polling HTTP | Works everywhere. | High latency and overhead. | Fallback only. |
| QUIC / HTTP/3 | Faster reconnects, handles network changes. | UDP is often blocked in enterprise networks. | Revisit for roaming devices. |

The **schema is defined in Protobuf** (`protocols/agent/v1/*.proto`, managed with `buf`) and sent as binary WebSocket messages. The agent protocol is transport-independent, so a gRPC transport could be added later without changing message semantics.

### 5.2 Connection setup

```text
Agent                                   agent-gateway
  │ POST /v1/agent/auth/challenge {device_id}      (HTTPS)
  │◄──────── {nonce, expires_in: 60}
  │ POST /v1/agent/auth/token {device_id, nonce, sig=ECDSA(priv, "agentmesh-auth-v1"||device_id||nonce)}
  │◄──────── {access_token (JWT, 15 min, aud=gateway, sub=device_id), gateway_url}
  │ GET wss://gw/v1/agent/connect   Authorization: Bearer <token>   Sec-WebSocket-Protocol: agentmesh.v1
  │◄──────── 101 Switching Protocols
  │ ─── Hello{agent_version, protocol_versions[], platform, arch, capabilities[], boot_id}
  │◄─── Welcome{session_id, server_time, heartbeat_interval_s, negotiated_version, device_id}
  │ ─── Inventory{...full...}
  │ ─── Heartbeat{seq,...} every 30s        ◄─── WS ping every 20s (dead-peer detection)
```

- The token is short-lived. Before it expires the agent fetches a fresh token and sends `TokenRefresh` in-band, so the socket does not need to be torn down.
- A 15-minute token lifetime means a revoked device is guaranteed to be cut off within 15 minutes. In practice it happens immediately: revocation publishes `dev.D.ctl {revoke}`, and the gateway sends `Disconnect{REVOKED}` and closes.

### 5.3 Envelope

```protobuf
message Envelope {
  uint32 v            = 1;  // protocol major version
  string id           = 2;  // ULID, unique per message (dedup)
  string correlation  = 3;  // id of the message this replies to
  google.protobuf.Timestamp ts = 4;
  oneof body {
    // agent → server
    Hello hello = 10;  Heartbeat heartbeat = 11;  Inventory inventory = 12;
    CommandAck command_ack = 13;  CommandOutput command_output = 14;
    CommandResult command_result = 15;  TokenRefresh token_refresh = 16;
    UpdateStatus update_status = 17;  LogBatch log_batch = 18;  MetricBatch metric_batch = 19;
    // server → agent
    Welcome welcome = 30;  CommandRequest command_request = 31;  CommandCancel command_cancel = 32;
    UpdateAvailable update_available = 33;  ConfigUpdate config_update = 34;
    SessionOpen session_open = 35;  Disconnect disconnect = 36;  Error error = 37;
  }
}
```

### 5.4 Protocol rules

| Rule | Value |
|---|---|
| Max frame size | 1 MiB (the gateway closes the connection with 1009 if exceeded) |
| Command output chunk | ≤ 32 KiB, sent as `CommandOutput{stream: STDOUT/STDERR, seq, data}` |
| Output cap per command | 10 MiB inline. Anything beyond is truncated and flagged; Phase 2 streams it to object storage. |
| Heartbeat | 30 s (the server can tune it via `Welcome`/`ConfigUpdate`) |
| Offline threshold | 3 missed heartbeats (90 s), or immediate on clean close plus a 15 s grace period to avoid flapping during a reconnect |
| Versioning | `v` is the major version. New fields are additive (Protobuf rules). The agent advertises supported versions and the server picks the highest common one. |
| Unknown message types | Ignored and logged (forward compatibility) |
| Per-connection rate limit | Token bucket (for example 50 msg/s burst 200). If exceeded: `Error{RATE_LIMITED}`, then close. |

---

## 6. Database schema

Every tenant-owned table carries `org_id`. MVP runs a single org, but adding tenancy later would require a painful migration. Primary keys are **UUIDv7** (time-ordered, which keeps indexes friendly). Timestamps are `timestamptz`.

### 6.1 Entity relationships

```text
organizations 1─┬─* users *─* roles *─* permissions         (user_roles, role_permissions)
                │      └─1─* user_sessions (refresh tokens)
                ├─* enrollment_tokens 1─* devices
                ├─* device_groups *─* devices                (device_group_members)
                ├─* devices 1─┬─* device_credentials         (public keys; rotation history)
                │             ├─* device_sessions            (one live WS connection each)
                │             ├─* device_heartbeats          (sampled, partitioned)
                │             ├─* device_metrics             (partitioned, P2)
                │             ├─* device_commands 1─1 command_results
                │             ├─* device_artifacts *─1 artifact_versions
                │             └─* remote_sessions *─1 users
                ├─* artifacts 1─* artifact_versions *─1 agent_versions
                └─* audit_logs (actor = user | device | system)
```

### 6.2 Tables (abridged DDL, MVP tables marked ★)

```sql
-- ★ tenancy & users
organizations(id uuid pk, name text, slug text unique, created_at)
users(id uuid pk, org_id fk, email citext, password_hash text /*argon2id*/, display_name,
      status text check (status in ('active','disabled')), mfa_secret_enc bytea null,
      last_login_at, created_at, updated_at, unique(org_id,email))
roles(id uuid pk, org_id fk null /*null = built-in*/, name text, description, is_builtin bool)
permissions(key text pk /* e.g. 'devices.read' */, description)
role_permissions(role_id fk, permission_key fk, pk(role_id, permission_key))
user_roles(user_id fk, role_id fk, scope_group_id fk null /*P2: scoped to device group*/,
           pk(user_id, role_id, coalesce(scope_group_id)))
user_sessions(id uuid pk, user_id fk, refresh_token_hash bytea unique, family_id uuid,
              user_agent, ip inet, expires_at, revoked_at null, replaced_by uuid null, created_at)

-- ★ enrollment & devices
enrollment_tokens(id uuid pk, org_id fk, token_hash bytea unique /*sha256*/, description,
                  group_id fk null, auto_approve bool, max_uses int null, uses int default 0,
                  expires_at, revoked_at null, created_by fk users, created_at)
devices(id uuid pk, org_id fk, name text, hostname text,
        status text check (status in ('pending','active','disabled','revoked')),
        connectivity text check (connectivity in ('online','offline')) default 'offline',
        platform text /*linux|windows|darwin|android|ios*/, arch text,
        os_name, os_version, os_build, kernel_version,
        agent_version text, hardware_fingerprint text /*hint only, not auth*/,
        inventory jsonb /*cpu, ram, disks, nics, ...*/, capabilities text[],
        enrolled_via fk enrollment_tokens null, approved_by fk users null, approved_at,
        last_seen_at, last_ip inet, created_at, updated_at)
  index (org_id, status), index (org_id, connectivity), index (last_seen_at)
device_credentials(id uuid pk, device_id fk, public_key bytea /*SPKI DER*/, algorithm text,
                   fingerprint text unique, created_at, revoked_at null)
device_groups(id uuid pk, org_id fk, name, description, created_at)          -- P2
device_group_members(group_id fk, device_id fk, pk(group_id, device_id))     -- P2

-- ★ presence
device_sessions(id uuid pk, device_id fk, gateway_id text, remote_ip inet, agent_version,
                protocol_version int, connected_at, last_seen_at, disconnected_at null,
                disconnect_reason text null)
  unique index on (device_id) where disconnected_at is null   -- at most one live session
device_heartbeats(device_id, ts, cpu_pct, mem_used_bytes, uptime_s, load1, ...)
  PARTITION BY RANGE (ts)  -- daily; sampled; 30-day retention
device_metrics(device_id, ts, name text, value double, labels jsonb) PARTITION BY RANGE (ts) -- P2

-- ★ commands
device_commands(id uuid pk, org_id fk, device_id fk, requested_by fk users, request_id text,
                kind text /*exec|action*/, action text null, argv text[] null, env jsonb null,
                timeout_s int, status text check (status in
                ('queued','sent','acked','running','succeeded','failed','timed_out','canceled','expired','rejected')),
                signature bytea, signing_key_id text, issued_at, expires_at,
                sent_at, started_at, finished_at, client_ip inet, created_at)
  index (device_id, status) where status in ('queued','sent','acked','running')
command_results(command_id pk fk, exit_code int null, stdout text, stderr text,
                truncated bool, error text null, duration_ms int, output_object_key text null)

-- artifacts & updates (P2)
artifacts(id uuid pk, org_id fk, name text /*agent-linux-amd64*/, kind text /*agent|package*/,
          platform, arch, created_at, unique(org_id,name))
agent_versions(id uuid pk, version text unique /*semver*/, min_protocol int, release_notes,
               channel text /*stable|beta|canary*/, status text /*draft|released|deprecated|revoked*/, created_at)
artifact_versions(id uuid pk, artifact_id fk, agent_version_id fk null, version text,
                  object_key text, size_bytes bigint, sha256 bytea, signature bytea, signing_key_id,
                  min_os_version text null, status text, published_by fk users, published_at)
device_artifacts(device_id fk, artifact_version_id fk, state text
                 /*offered|downloading|verified|installed|failed|rolled_back*/, error, updated_at,
                 pk(device_id, artifact_version_id))
rollouts(id uuid pk, agent_version_id fk, target jsonb /*groups/platforms*/, percent int,
         status, paused_reason, created_by, created_at)                               -- P2

-- remote sessions (P4)
remote_sessions(id uuid pk, org_id fk, device_id fk, user_id fk, kind text /*pty|tcp_forward|screen*/,
                target_port int null, status, relay_id text, reason text, client_ip inet,
                started_at, ended_at, bytes_in bigint, bytes_out bigint, recording_object_key text null)

-- ★ audit (append-only)
audit_logs(id bigserial pk, org_id, ts timestamptz default now(), request_id text,
           actor_type text /*user|device|system*/, actor_id uuid, actor_ip inet, session_id uuid null,
           action text /*device.revoke, command.create, ...*/, target_type, target_id,
           outcome text /*success|denied|error*/, details jsonb,
           prev_hash bytea, hash bytea /* sha256(prev_hash || canonical(row)) */)
  -- app DB role has INSERT + SELECT only; UPDATE/DELETE revoked; partitioned monthly
```

---

## 7. Authentication and authorization flow

There are three kinds of principal, and each has its own credentials:

| Principal | Credential | Token | Lifetime |
|---|---|---|---|
| **User** (dashboard / mobile) | Email + password (argon2id). TOTP in Phase 2, OIDC/SAML SSO in Phase 4. | Access JWT (EdDSA), `aud=api` | 10 min |
| | | Refresh token (opaque, 256-bit, hashed in DB, rotating, reuse detection) | 12 h web / 30 d mobile (sliding) |
| **Device** | ECDSA P-256 private key (proof of possession) | Access JWT `aud=gateway`, `sub=device_id` | 15 min |
| **Service** (internal) | Kubernetes service account plus mTLS via mesh, or a shared NATS NKey | — | — |

### 7.1 User login and refresh

```text
Browser                     control-api                        Postgres
  │ POST /v1/auth/login {email,pw}   │                               │
  │─────────────────────────────────►│ rate-limit (IP + email)       │
  │                                  │ verify argon2id (constant-time path for unknown users)
  │                                  │ INSERT user_sessions(refresh_hash, family) ──►
  │◄──── 200 {access_token} + Set-Cookie: am_refresh=<opaque>; HttpOnly; Secure; SameSite=Strict; Path=/v1/auth
  │ ... API calls with Authorization: Bearer <access_token> (kept in memory only)
  │ POST /v1/auth/refresh (cookie) + X-CSRF header
  │─────────────────────────────────►│ lookup hash; if already rotated ⇒ REUSE ⇒ revoke whole family
  │◄──── new access + rotated refresh
```

- The browser keeps the access token **in memory only**, never in localStorage, so XSS cannot steal a long-lived credential.
- The mobile app stores its refresh token encrypted with an Android Keystore key.

### 7.2 Authorization (RBAC)

Permissions are fine-grained strings. Roles are bundles of permissions. Handlers check **permissions, never role names**.

| Permission | Viewer | Operator | Admin | Super Admin |
|---|:-:|:-:|:-:|:-:|
| `devices.read`, `metrics.read`, `logs.read`, `commands.read` | ✅ | ✅ | ✅ | ✅ |
| `commands.execute.action` (predefined safe actions) | | ✅ | ✅ | ✅ |
| `commands.execute.exec` (arbitrary argv) | | | ✅ | ✅ |
| `sessions.terminal` / `sessions.forward` (P4) | | ✅* | ✅ | ✅ |
| `devices.manage` (approve, rename, disable, revoke, groups) | | | ✅ | ✅ |
| `enrollment.manage` | | | ✅ | ✅ |
| `users.manage` (except Super Admins) | | | ✅ | ✅ |
| `artifacts.publish`, `rollouts.manage` | | | | ✅ |
| `audit.read` | | | ✅ | ✅ |
| `platform.admin` (roles, org settings, signing keys) | | | | ✅ |

\* Can be enabled per group in Phase 2 (scoped role assignments).

- **Evaluation.** `allowed = ∃ role ∈ user.roles : perm ∈ role.perms ∧ (role.scope == nil ∨ device ∈ role.scope)`. Permission sets are cached per access token and embedded as a compact claim, with a DB re-check for sensitive actions.
- **Sensitive actions** (exec, revoke, user management, publishing) require a fresh authentication (`auth_time` < 15 min) once MFA exists. They are always audited, and that includes *denied* attempts.
- **Privilege escalation guard.** A user can never grant a role containing permissions they do not hold themselves.

---

## 8. Device registration flow

```text
Admin (dashboard)                 control-api                  Agent (on device)
  │ Create enrollment token        │                              │
  │ {group, auto_approve, max_uses, expires_in} ─►│ store sha256(token) │
  │◄── "am_enr_<base32 32B>" (shown once)         │                     │
  │                                               │                     │
  │   operator installs agent:  agentmesh-agent enroll --server https://mesh.example.com --token am_enr_...
  │                                               │                     │
  │                                               │     generate P-256 keypair → secure store
  │                                               │◄── POST /v1/agent/enroll
  │                                               │    {token, public_key, facts{hostname, os, arch,
  │                                               │     machine_id_hash, agent_version},
  │                                               │     pop_sig = sign(priv, "agentmesh-enroll-v1"||sha256(token)||pubkey)}
  │                                               │ rate-limit per IP; validate token (not expired/revoked/over-uses)
  │                                               │ verify pop_sig with public_key
  │                                               │ TX: INSERT devices(status = auto_approve ? active : pending)
  │                                               │     INSERT device_credentials; UPDATE token.uses += 1
  │                                               │     INSERT audit_logs(device.enroll)
  │                                               │──► {device_id, status, command_pubkeys[], gateway_url}
  │                                               │                     │ persist device_id + server pubkeys
  │ (if pending) sees "1 device awaiting approval"│                     │ poll/auth with backoff → 403 PENDING
  │ Approve ─────────────────────────────────────►│ status=active, audit│
  │                                               │                     │ next auth succeeds → connect → ONLINE
```

- **Secrets never go on the command line in production.** The installer also accepts `--token-file` or the environment variable `AGENTMESH_ENROLL_TOKEN`. Installer scripts delete the token file after enrollment.
- **Re-enrollment.** If `machine_id_hash` matches an existing *revoked* device, a new device record is created and linked (`replaces_device_id`) for history. Revocation is permanent and never resurrected.
- **Key rotation (Phase 2).** The agent generates a new key and registers it through `CredentialRotate` signed by the old key. The old credential gets a `revoked_at` after a grace period.
- **Device naming.** The default name is the hostname, and it is editable in the dashboard. `id` is the stable identity; the name is only a label.

---

## 9. Command execution flow

### 9.1 Command types

| Kind | Example | Permission | Agent capability |
|---|---|---|---|
| `action` | `system.info`, `service.restart{name}`, `process.list`, `agent.restart` | `commands.execute.action` | per-action allowlist in agent |
| `exec` | argv `["systemctl","status","nginx"]`, no shell unless `shell: true` is explicit | `commands.execute.exec` | `exec` (can be disabled by local policy) |

### 9.2 Flow

```text
Operator   control-api                    NATS      gateway(D)           Agent D
  │ POST /v1/devices/D/commands  {kind, argv, timeout_s}  Idempotency-Key: <uuid>
  │──────►│ authn → authz(commands.execute.*) → device active? capability declared?
  │       │ rate-limit per user & per device
  │       │ build CommandSpec{cmd_id, org_id, device_id, kind, argv_hash, issued_by,
  │       │                   issued_at, expires_at = now+5m (configurable ≤ 1h), nonce}
  │       │ sig = Ed25519(command_signing_key, canonical(CommandSpec))   (key in KMS/Secret)
  │       │ TX: INSERT device_commands(status=queued) + audit(command.create)
  │       │ publish dev.D.cmd ─────────►│─────────►│ load + send CommandRequest
  │◄──202 {command_id}                               │────────────────────►│ VERIFY:
  │                                                  │                     │  sig with pinned pubkey
  │                                                  │                     │  device_id == mine
  │                                                  │                     │  now(adj) < expires_at
  │                                                  │                     │  cmd_id not in seen-set (replay)
  │                                                  │                     │  capability allowed by local policy
  │                                                  │◄── CommandAck ──────│  (or CommandResult{rejected, reason})
  │  SSE: command.status=running                     │◄── CommandOutput* ──│ exec with timeout, own process group
  │  SSE: output chunks (live)                       │◄── CommandResult ───│ {exit_code, duration_ms, truncated}
  │       │◄──────── evt.command.result ──────────────│ UPDATE status + INSERT command_results + audit(command.result)
```

- **Expiry.** The worker marks `queued`/`sent` commands as `expired` after `expires_at`. The agent independently refuses expired ones.
- **Replay protection.** The agent persists seen `cmd_id`s until their `expires_at` has passed, so the seen-set stays bounded. A captured `CommandRequest` cannot be replayed to the same device, and the `device_id` binding stops it being replayed to a different one.
- **Idempotency.** `Idempotency-Key` on POST prevents double-submits from the UI or mobile retries.
- **Cancel.** `POST /commands/{id}/cancel` → `CommandCancel`. The agent kills the process group (SIGTERM, then SIGKILL after 5 s; Windows uses a Job Object).
- **Why signing matters even with TLS.** If a gateway pod or NATS is compromised, it still cannot forge commands. Only a holder of the signing key, which only control-api has, can create one.

Every command's audit record includes: user, device, command (argv, with any secrets redacted by pattern), timestamp, request ID, result, duration, client IP and user-session ID.

---

## 10. Remote-session architecture

*(Designed now, built in Phase 4. The Phase 1 protocol already reserves `SessionOpen`.)*

### 10.1 Model

Interactive traffic **does not use the control connection**. That keeps heartbeats and commands responsive and lets the relay tier scale separately.

```text
Operator client                       control-api            session-relay R         Agent D
(browser xterm.js / CLI)
  │ POST /v1/devices/D/sessions {kind: pty|tcp_forward, port?, reason}
  │────────────────────────────────────►│ authz(sessions.*), device capability,
  │                                     │ policy (e.g. require reason, business hours, approval)
  │                                     │ INSERT remote_sessions(pending) + audit
  │                                     │ mint two single-use tickets (60 s TTL, bound to session_id+side)
  │                                     │ SessionOpen{session_id, relay_url, agent_ticket} ─► via gateway ─►│
  │◄── {session_id, relay_url, client_ticket}                                                       │
  │ WSS relay_url?ticket=client ───────────────────────────────►│◄──── WSS relay_url (agent_ticket)──│
  │                                                              │ pair both sides by session_id     │
  │◄═══════════════ bytes (E2E: TLS to relay; relay pipes) ════►│◄═════════════════════════════════►│
  │                                                              │ recording (P4+), idle timeout,    │ PTY / localhost:port
  │                                                              │ max duration, byte counters       │
```

- The agent dials the relay **outbound**. The relay never dials devices.
- `tcp_forward` only allows **loopback targets on ports the local policy lists** (for example `22` on Linux, `3389` on Windows). This lets a native SSH or RDP client work through `agentmesh connect D --port 22` (a CLI that opens a local listener) with nothing exposed publicly.
- Session tickets are single-use and redeemed atomically in Redis/Postgres. Sessions can be terminated from the dashboard (`sessions.manage`) and are killed on device revocation.
- **Optional end-to-end encryption (future).** Client and agent can run a Noise handshake inside the relay stream, keyed with the device's key, so the relay only sees ciphertext. It is deferred because it complicates session recording; this is a policy choice per org.

### 10.2 Per-platform mechanism

| Platform | Session kinds |
|---|---|
| Linux | PTY (`creack/pty`) running the user's login shell; `tcp_forward` to sshd, VNC or web UIs |
| Windows | ConPTY with PowerShell/cmd; `tcp_forward` to RDP 3389 (native client through the CLI); WinRM stays local |
| macOS | PTY with zsh; `tcp_forward` to Screen Sharing (VNC 5900) if the user enabled it (TCC restricts programmatic enablement) |
| Android | No shell. Device-owner APIs only; screen view through `MediaProjection` with **user consent on every session** (Phase 4+) |
| iOS | No shell, no remote control. The user can start a screen broadcast via ReplayKit (view only). Management happens through MDM. |

---

## 11. Artifact/package distribution architecture

```text
CI (GitHub Actions)                         AgentMesh                          Device
  build matrix: linux/amd64, linux/arm64,
  windows/amd64, darwin/arm64, darwin/amd64
  │ reproducible build (-trimpath, SOURCE_DATE_EPOCH)
  │ platform signing: Authenticode (Win), Developer ID + notarization (mac), GPG for .deb/.rpm
  │ generate manifest.json {artifact, version, sha256, size, min_os}
  │ sign manifest with RELEASE KEY (Ed25519, offline/HSM, NOT on the server)
  │ POST /v1/artifacts/{name}/versions (artifacts.publish) + upload via presigned PUT ─►  S3/MinIO
  │                                         │ verify sha256 + signature against trusted release keys
  │                                         │ status=draft → admin marks released → rollout
  │                                                                                │
  │                                         GET /v1/agent/updates/manifest ◄───────│ (device token)
  │                                         302 → presigned GET (5 min)  ─────────►│ download over TLS
```

- **Two independent layers of signature.**
  1. OS-native code signing, so Windows SmartScreen, macOS Gatekeeper and package managers trust the binary.
  2. The **AgentMesh release signature** over the manifest, verified by the agent against release public keys **compiled into the agent**.
- **Server compromise does not equal fleet compromise.** The release signing key never lives on the backend. An attacker controlling the backend can offer an old or held-back version (mitigated by a monotonic version rule: no downgrade unless a *signed* rollback manifest allows it) but cannot push a malicious binary.
- **Key rotation.** The agent trusts a small set of root keys. A root-signed `keys.json` lists the current release keys with expiry. Phase 4 option: adopt **TUF** (`go-tuf`) for full rollback, freeze and mix-and-match protection.
- **Storage layout.** `artifacts/{name}/{version}/{file}` holds immutable objects, served by presigned URL or optionally a CDN.
- **Public install endpoint.** `GET /install.sh` or `/install.ps1` serves a small, versioned, signed bootstrap script that downloads the correct artifact, verifies its checksum and signature, installs the service and runs `enroll`.
- **Formats.** `.deb`/`.rpm` plus a tarball (Linux), `.msi` (WiX) (Windows), `.pkg` (macOS, notarized), `.apk` or managed Google Play (Android), App Store / Apple Business Manager (iOS; side-loading `.ipa` is not viable for a fleet).

---

## 12. Agent update architecture

```text
worker (rollout)           gateway                 Agent
  │ rollout: v1.2.0 → stable, 5% → 25% → 100%; pause on failure rate > 2%
  │ pick devices (hash(device_id) mod 100 < percent), platform/arch match, min_os ok
  │ UpdateAvailable{version, manifest_url} ───►│──────►│ 1. fetch manifest; verify release signature
  │                                            │       │ 2. check version > current (anti-downgrade)
  │                                            │       │ 3. download to staging dir; verify sha256 + size
  │                                            │◄──────│ UpdateStatus{downloading|verified}
  │                                            │       │ 4. hand off to platform installer
  │                                            │       │    Linux:   atomic rename bin → systemd restart
  │                                            │       │    Windows: spawn detached updater.exe → msiexec /qn → service restart
  │                                            │       │    macOS:   installer -pkg (launchd restart)
  │                                            │       │ 5. new agent starts in "probation": must connect +
  │                                            │       │    send Hello within 5 min, else watchdog restores previous binary
  │                                            │◄──────│ UpdateStatus{installed|rolled_back, error}
  │ update device_artifacts; rollout stats → auto-pause
```

- The agent keeps **N-1 binary** for rollback.
- Maintenance windows and "do not update" group policies come in Phase 4.
- Agents only receive `UpdateAvailable` for versions that support the protocol version they currently negotiate, and the server supports protocol N and N-1. This allows fleet-wide protocol upgrades without a flag day.
- Package-manager installs (apt/yum repositories) can opt out of self-update and follow the repository instead (`update.mode = managed | self`).

---

## 13. Web dashboard architecture

**Stack:** React 19 + TypeScript + Vite, TanStack Router + TanStack Query, Tailwind CSS + shadcn/ui components, xterm.js (Phase 4), Recharts (metrics). Served as static files by nginx (or from a CDN). It talks only to `control-api`.

```text
dashboard/src
├── app/            router, providers (QueryClient, Auth), layout shell
├── api/            typed client generated from OpenAPI (openapi-typescript) + fetch wrapper
│                   (attaches in-memory access token, transparent refresh on 401, request-id)
├── auth/           login, session context, permission hooks: useCan('devices.manage')
├── features/
│   ├── devices/    list (server-side paging/filter/sort), details tabs, approve/disable/revoke
│   ├── commands/   run command dialog, live output (SSE), history
│   ├── enrollment/ tokens, install-snippet generator
│   ├── audit/      searchable audit log
│   ├── users/      users & roles (P2 full RBAC editor)
│   ├── artifacts/  (P2) versions, rollouts
│   └── sessions/   (P4) terminal
├── components/     design-system wrappers (Table, StatusBadge, ConfirmDialog...)
└── lib/            formatting (bytes, relative time), sse client
```

- **Live updates.** A single `GET /v1/events` SSE stream per tab, filtered server-side by org and permission, carries `device.status`, `device.updated` and `command.*` events. TanStack Query caches are patched or invalidated from those events. SSE is chosen over WebSocket here because it works over plain HTTP, reconnects automatically and needs nothing extra from proxies.
- **Pages (MVP):** Login · Devices (table: Name, OS, Version, Agent, Status, Last seen) · Device details (Overview: CPU/RAM/storage/network/agent version/last heartbeat; tabs Commands · Activity · Settings; placeholders for Metrics · Logs · Packages · Artifacts · Access) · Enrollment tokens · Audit log · Users.
- **UI permission gating is for UX only.** The API always enforces authorization.
- **Security.** Strict CSP (no inline scripts), `frame-ancestors 'none'`, and no third-party scripts. All device-reported strings are escaped (device inventory is attacker-controlled input).

---

## 14. Android control-app architecture

The **Android Control App** (an operator tool) is a separate product from the **Android Device Agent** (runs on managed devices, Phase 3). They are separate apps with separate package IDs, and they never share code beyond the generated API client.

**Stack:** Kotlin, Jetpack Compose (Material 3), MVVM + unidirectional data flow, Hilt (DI), Retrofit + OkHttp (certificate pinning optional), Kotlinx Serialization, Room (offline cache), DataStore, WorkManager, Firebase Cloud Messaging, BiometricPrompt.

```text
mobile/android-control/
├── app/                    navigation graph, theme, Hilt application
├── core/network            OkHttp: auth interceptor + authenticator (refresh), SSE client
├── core/data               repositories (network + Room cache, Flow-based)
├── core/security           Keystore-backed encrypted storage, biometric gate
├── feature/login
├── feature/devices         list, detail, metrics charts
├── feature/commands        run (only actions the user's perms allow), live output
├── feature/sessions        (P4) terminal view
├── feature/notifications   FCM token registration, deep links
└── feature/access          approve devices, disable/revoke (with confirm + biometric)
```

- **Authentication.** Same user auth as the web (§7). The refresh token is stored encrypted with an Android Keystore key. The app optionally requires biometric unlock on launch and **always** requires it before a destructive action (revoke, exec).
- **Notifications.** The backend `worker` sends through FCM (device offline, pending approval, command failed). A payload contains **only IDs**, never sensitive data; the app fetches details after the user authenticates.
- **Offline.** Cached device list is marked "as of <time>". Commands are never queued offline.

---

## 15. Platform capability matrix

Legend: ✅ supported · ⚠️ supported with conditions · ❌ not possible under platform rules

| Capability | Linux | Windows | macOS | Android (agent) | iOS |
|---|:-:|:-:|:-:|:-:|:-:|
| Runs as background service | ✅ systemd | ✅ Windows Service | ✅ launchd daemon | ⚠️ foreground service with persistent notification; Doze limits | ❌ no persistent background process |
| Persistent outbound connection | ✅ | ✅ | ✅ | ⚠️ drops in Doze; **FCM high-priority push wakes agent** | ❌ use **APNs + MDM** check-in instead |
| Heartbeat interval | 30 s | 30 s | 30 s | ~15 min (WorkManager) + on-push | MDM check-in on demand |
| Hardware/OS inventory | ✅ | ✅ | ✅ | ✅ (more with Device Owner) | ⚠️ MDM `DeviceInformation` query |
| Installed software list | ✅ dpkg/rpm/snap | ✅ registry/MSI | ✅ pkgutil/apps | ⚠️ needs `QUERY_ALL_PACKAGES` or Device Owner | ⚠️ MDM `InstalledApplicationList` (managed apps fully) |
| Execute commands / shell | ✅ | ✅ PowerShell/cmd | ✅ zsh | ❌ no shell (no root); only DPM APIs | ❌ |
| Remote terminal (PTY) | ✅ | ✅ ConPTY | ✅ | ❌ | ❌ |
| TCP forward (SSH/RDP/VNC via relay) | ✅ | ✅ | ✅ | ❌ | ❌ |
| Remote screen view | ⚠️ via VNC forward | ⚠️ via RDP forward | ⚠️ Screen Sharing if enabled by user | ⚠️ `MediaProjection`, user consent each session | ⚠️ ReplayKit broadcast, user-initiated, view only |
| Remote input control | ⚠️ via VNC | ⚠️ via RDP | ⚠️ via VNC | ⚠️ AccessibilityService only; Play policy restricts this heavily | ❌ |
| Lock / wipe device | ✅ (commands) | ✅ | ✅ | ✅ Device Owner / Admin API | ✅ via MDM |
| Install/remove apps silently | ✅ | ✅ | ✅ | ✅ Device Owner (`PackageInstaller`) | ✅ MDM (supervised/managed apps) |
| Enforce policies (passcode, Wi-Fi, VPN) | ⚠️ config mgmt | ✅ registry/GPO-like | ⚠️ config profiles | ✅ Device Owner | ✅ MDM profiles |
| Self-update agent | ✅ | ✅ | ✅ | ⚠️ Device Owner silent; otherwise Play/user prompt | ❌ App Store only |
| Key storage | file 0600 / TPM | DPAPI / TPM | Keychain / Secure Enclave | Keystore / StrongBox | Keychain / Secure Enclave |
| Enrollment method | token CLI | token CLI / MSI property | token CLI / pkg | QR code provisioning (Android Enterprise) | Apple Business Manager / user enrollment profile |

**Design consequences**
- **Android agent (Phase 3)** is a Kotlin **Device Policy Controller**. Full-management mode is set up through QR provisioning, and a reduced work-profile mode is also available. It keeps the WSS connection while in the foreground, falls back to FCM wake-ups, and speaks the same Protobuf protocol. Its capability list (`dpm.lock`, `dpm.wipe`, `app.install`...) replaces `exec`.
- **iOS (Phase 4)** is **not an "agent" in the desktop sense**. AgentMesh implements an **Apple MDM server** component (check-in and command endpoints plus APNs push with an MDM push certificate), alongside an optional companion app for user-facing features. Commands map to MDM commands. This requires an Apple Developer Enterprise/MDM vendor relationship. It is the only sanctioned way.
- The backend models this generally: `devices.capabilities[]` drives what the UI offers and what the API accepts.

---

## 16. Security model

### 16.1 Threat model (summary)

| Threat | Mitigation |
|---|---|
| Stolen enrollment token | Short expiry, max uses, optional manual approval, hash-only storage, audit of each use, revocable |
| Device impersonation | Per-device non-exportable key plus proof of possession; short-lived tokens; revocation takes effect immediately |
| Compromised gateway/NATS forging commands | Ed25519 command signatures verified by the agent; key held only by control-api (KMS in production) |
| Replay of commands | `cmd_id` seen-set, device binding, `expires_at`, adjusted clock |
| Malicious update pushed via backend | Offline release signing key, compiled-in trust roots, anti-downgrade |
| Operator abuse / insider | RBAC least privilege, reason fields, tamper-evident audit, 4-eyes approval for sessions (P4) |
| Stolen user session | In-memory access token, rotating refresh token with reuse detection, MFA, re-auth for sensitive actions |
| XSS via device-reported data | Output encoding, strict CSP, inventory schema validation and size limits |
| DoS / reconnect storms | Rate limits (IP, user, device), connection limits per gateway, jittered backoff, `retry_after`, frame and output caps |
| Lateral movement from agent | Agent exposes no listening ports; `tcp_forward` restricted to loopback plus allowlisted ports; local policy file |
| Data at rest | Postgres/S3 encryption; secrets (MFA seeds, etc.) envelope-encrypted with a KMS key |

### 16.2 Controls checklist

- **TLS 1.3 only** for agents (TLS 1.2+ for browsers). HSTS. The agent optionally pins the server CA for private deployments.
- **Secrets:** Kubernetes Secrets sourced from Vault / cloud secret manager via External Secrets Operator. Nothing secret is stored in the repo or in images. JWT and command-signing keys are rotated with `kid` headers.
- **Rate limiting:** per IP on auth/enroll endpoints (Redis token bucket at scale; in-memory per pod for MVP), per user on commands, per device on the socket.
- **Connection limits:** max connections per gateway pod (for example 20k, then 503 + `retry_after`), max one live connection per device, per-IP connection caps on the handshake.
- **Audit:** every state-changing API call and every denied authorization is recorded. The hash chain is verified by a worker job, and a daily anchor hash is exported to an external write-once store (S3 Object Lock).
- **Supply chain:** Go module checksum DB, `govulncheck`, `npm audit`, Dependabot, SBOM (Syft) per artifact, signed container images (cosign).
- **Agent hardening:** runs as root/SYSTEM only because inventory and exec need it. The PTY session drops to a specified user where possible. `exec` runs without a shell by default. The environment is scrubbed.

---

## 17. Kubernetes deployment architecture

```text
                     ┌────────────── Kubernetes cluster ─────────────────────────────┐
 Internet ─► CDN ──► │ Ingress (nginx/Envoy Gateway) — TLS via cert-manager          │
      │              │   /           → dashboard (Deployment, nginx, 2+)             │
      │              │   /v1/*       → control-api (Deployment, HPA 2..N)            │
      │              │                                                               │
      └──► L4 NLB ──►│ agent-gateway (Deployment, HPA on active_connections, 2..N)   │
         (443/tcp,   │   PodDisruptionBudget, preStop: drain → send Disconnect{      │
          TLS at pod │   RECONNECT, retry_after=rand(0..60s)} → graceful close       │
          or NLB)    │ session-relay (Deployment, HPA, P4)                           │
                     │ worker (Deployment, 2, leader tasks via pg advisory locks)    │
                     │ NATS (StatefulSet ×3, Helm chart)                             │
                     │ PostgreSQL: managed (RDS/Cloud SQL) or CloudNativePG operator │
                     │ Redis (P2+, rate limits/tickets), MinIO or cloud S3           │
                     │ Observability: Prometheus/VictoriaMetrics, Grafana, Loki,     │
                     │   Tempo/Jaeger via OpenTelemetry Collector                    │
                     └───────────────────────────────────────────────────────────────┘
```

- **Graceful gateway rollouts.** On SIGTERM, a gateway stops accepting new connections, tells its agents to reconnect with a randomized delay, then exits. This spreads 20k reconnects over 60 s instead of 1 s.
- **Helm chart** (`infrastructure/helm/agentmesh`) with values per environment. Kustomize overlays can be used optionally.
- **Network policies:** only the ingress and NLB can reach pods; Postgres is reachable only from api, gateway and worker.

### 17.1 Scaling plan

| Fleet size | Topology | Notes |
|---|---|---|
| **100** | Docker Compose on one VM: 1× each service, Postgres, NATS | MVP target; all features |
| **1,000** | Same, or small k8s: 2× api, 2× gateway | Heartbeat batching already in place |
| **10,000** | k8s: 3× api, 3–4× gateway (≈3k conns each), NATS cluster, managed Postgres + 1 replica | Partitioned heartbeats; reads for lists from replica |
| **100,000+** | 10+ gateways (≈10–20k conns each, ~ 2–4 GB RAM), multi-AZ, PgBouncer, TimescaleDB/VictoriaMetrics for metrics, JetStream for events, CDN for artifacts | Regional gateway clusters with a global API later; consider org-based sharding |

A gateway's memory per connection is about 20–40 KB (two goroutines plus buffers), so 20k connections need roughly 0.5–1 GB. The real limits are file descriptors (set `ulimit` / `LimitNOFILE`) and NLB idle timeouts (kept below the WS ping interval: 20 s ping vs the typical 350 s NLB idle timeout).

---

## 18. Monorepo structure

One Go module at the root covers the backend and desktop agents, so the protocol and shared libraries have one version. `depguard` lint rules enforce that `agents/**` never imports `backend/**`, and the reverse.

```text
AgentMesh/
├── go.mod                         # module github.com/enfec/agentmesh
├── Makefile                       # make dev | test | lint | proto | build-agents
├── buf.yaml / buf.gen.yaml
├── protocols/
│   ├── agent/v1/*.proto           # agent ⇄ gateway protocol (source of truth)
│   ├── api/openapi.yaml           # control-api REST contract (dashboard/mobile clients generated)
│   └── gen/go/...                 # generated (committed)
├── backend/
│   ├── cmd/
│   │   ├── control-api/main.go
│   │   ├── agent-gateway/main.go
│   │   └── worker/main.go
│   ├── internal/
│   │   ├── auth/  orgs/  enrollment/  devices/  presence/  commands/
│   │   ├── artifacts/  sessions/  audit/
│   │   ├── platform/              # config, db (pgx pool), bus (nats), httpx, logging, telemetry
│   │   └── store/                 # sqlc output + queries/*.sql
│   └── migrations/                # goose SQL migrations
├── agents/
│   ├── core/                      # shared Go agent core (identity, conn, commands, ...)
│   ├── linux/                     # main + systemd unit + nfpm (.deb/.rpm) config
│   ├── windows/                   # main + service + WiX (.msi) config
│   ├── macos/                     # main + launchd plist + pkgbuild scripts (P2)
│   ├── android/                   # Kotlin DPC agent (P3) — own Gradle project
│   └── ios/                       # MDM notes + companion app (P4) — own Xcode project
├── dashboard/                     # React + Vite (pnpm)
├── mobile/
│   └── android-control/           # Kotlin + Compose (P3) — own Gradle project
├── infrastructure/
│   ├── docker/                    # Dockerfiles (distroless), docker-compose.yml, .env.example
│   ├── kubernetes/                # raw manifests / kustomize (reference)
│   └── helm/agentmesh/
├── scripts/                       # install.sh, install.ps1, dev helpers
├── docs/
│   ├── architecture.md            # this document
│   ├── adr/                       # architecture decision records (0001-websocket-protobuf.md, ...)
│   └── runbooks/
└── .github/workflows/             # ci.yml (lint/test), release.yml (build/sign/publish agents)
```

---

## 19. Phase-by-phase implementation plan

| Phase | Scope | Exit criteria |
|---|---|---|
| **1 — MVP** | Go backend (api, gateway, worker), PostgreSQL + migrations, NATS, enrollment tokens, device registration with approval, device auth (P-256 PoP → JWT), WSS protocol v1, heartbeat, inventory, online/offline, Linux + Windows agents (service install), signed command execution (`exec` + a few `action`s) with live output, audit log with hash chain, fixed built-in roles (Super Admin/Admin/Operator/Viewer), web dashboard (login, devices, details, commands, enrollment, audit, users), Docker Compose dev env | Enroll a Linux VM and a Windows laptop, see both ONLINE, run a command on each and see the output, revoke one and watch it disconnect within 1 s, find every action in the audit log. Unit + integration tests green in CI. |
| **2** | macOS agent (launchd, notarized .pkg), artifact repository (MinIO/S3, publish API, release-key verification), agent version management, self-update with rollback + staged rollouts, metrics (time series + charts), agent log shipping, full RBAC editor + group-scoped roles, device groups, TOTP MFA, Redis rate limits, key rotation, Helm chart | Roll v1.0.1 to 10%, then 100%, across three OSes; a corrupted artifact is rejected by agents; a Viewer cannot run commands. |
| **3** | Android device agent (DPC, QR provisioning, FCM wake), Android control app, push notifications, Android capabilities (lock, wipe, app install, policy) | Provision a test phone via QR, view it in the control app, lock it remotely, receive an "offline" push. |
| **4** | iOS via MDM server + APNs, macOS advanced management (profiles), session-relay with PTY + TCP forward + recording, `agentmesh` CLI, device policies, fleet management (bulk actions, saved filters, maintenance windows), SSO (OIDC/SAML), TPM/Secure Enclave keys, optional TUF | Open a browser terminal to Linux, RDP to Windows via the CLI tunnel, send an MDM lock to a supervised iPhone. |

Each phase ships behind stable APIs, so earlier clients keep working.

---

## 20. MVP implementation plan

### 20.1 In scope / out of scope

**In:** everything listed for Phase 1 above.
**Out (deferred, but designed for):** metrics history charts, log shipping, artifacts/updates, groups, custom roles, MFA/SSO, macOS/mobile, remote sessions, Redis, Helm (Compose only for MVP; a basic k8s manifest set is a stretch goal).

### 20.2 MVP API surface (control-api)

```text
POST   /v1/auth/login                 POST /v1/auth/refresh          POST /v1/auth/logout
GET    /v1/me
GET    /v1/devices?status=&connectivity=&platform=&q=&cursor=&limit=
GET    /v1/devices/{id}               PATCH /v1/devices/{id} {name}
POST   /v1/devices/{id}/approve       POST /v1/devices/{id}/disable   POST /v1/devices/{id}/enable
POST   /v1/devices/{id}/revoke
GET    /v1/devices/{id}/commands      POST /v1/devices/{id}/commands
GET    /v1/commands/{id}              POST /v1/commands/{id}/cancel
GET    /v1/devices/{id}/activity      (audit filtered by target)
GET    /v1/enrollment-tokens          POST /v1/enrollment-tokens      DELETE /v1/enrollment-tokens/{id}
GET    /v1/audit?actor=&action=&target=&from=&to=&cursor=
GET    /v1/users  POST /v1/users  PATCH /v1/users/{id}  (roles assignment from built-ins)
GET    /v1/events                     (SSE)
GET    /healthz  /readyz  /metrics
```

Agent endpoints (served by agent-gateway):

```text
POST /v1/agent/enroll   POST /v1/agent/auth/challenge   POST /v1/agent/auth/token   GET /v1/agent/connect (WSS)
```

### 20.3 Key libraries

| Concern | Library |
|---|---|
| HTTP router | `go-chi/chi/v5` |
| WebSocket | `github.com/coder/websocket` (context-aware, maintained successor of nhooyr) |
| Postgres | `jackc/pgx/v5` with hand-written queries in each domain package + `pressly/goose` (embedded migrations) |
| NATS | `nats-io/nats.go` |
| JWT | `golang-jwt/jwt/v5` (EdDSA) |
| Password hashing | `golang.org/x/crypto/argon2` (argon2id) |
| Protobuf | `buf` + `google.golang.org/protobuf` |
| System info (agent) | `shirou/gopsutil/v4` |
| Windows service | `golang.org/x/sys/windows/svc` |
| IDs | `google/uuid` (v7) |
| Config | environment variables with a typed loader (`caarlos0/env`) |
| Logging / telemetry | `log/slog` (JSON), `prometheus/client_golang`, OpenTelemetry (stub in MVP) |
| Testing | stdlib `testing`, `testcontainers-go` (Postgres, NATS) for integration tests |

### 20.4 Build order (each step is a reviewable slice with tests)

1. **Foundation:** repo skeleton, `go.mod`, Makefile, lint config, Docker Compose (Postgres, NATS) on non-conflicting ports, config loader, logging, health endpoints.
2. **Schema:** migrations for MVP tables, sqlc queries, seed built-in roles/permissions, bootstrap Super Admin from env on first start.
3. **User auth:** login, refresh rotation, logout, authz middleware, audit writer with hash chain.
4. **Protocol:** `.proto` v1 + generated code + envelope codec tests.
5. **Enrollment + device auth:** tokens API, `/agent/enroll`, challenge/token, approval/disable/revoke.
6. **Agent gateway:** WSS server, connection registry, NATS subscription, heartbeat batching, presence, reaper in worker.
7. **Agent core + Linux agent:** identity, enroll CLI, connect/reconnect, heartbeat, inventory, systemd unit.
8. **Commands end to end:** signing, dispatch, agent verification/execution, streaming output, results, expiry, cancel.
9. **Windows agent:** service wrapper, DPAPI key store, inventory, Job-Object process control; build `.exe` (MSI in stretch).
10. **Dashboard:** login, devices list/details, SSE live status, command runner, enrollment tokens with install snippet, audit, users.
11. **Hardening pass:** rate limits, connection limits, security headers, end-to-end test (Compose + real agent container), docs/runbook.

I'll stop after each 1–3 steps for review rather than generating everything at once.

---

## Implementation notes (MVP)

This section records where the Phase 1 code differs from the plan above, and why.

| Topic | Plan | Implemented | Reason |
|---|---|---|---|
| Go version | 1.24+ | **Go 1.26** | Current pgx, goose, NATS and x/crypto releases require Go ≥ 1.25/1.26. |
| SQL layer | sqlc | **Hand-written pgx queries**, kept next to each domain service | sqlc's parser needs cgo, which is unavailable on the Windows dev machine. The queries are small and reviewed. sqlc can be adopted later without schema changes. |
| Dashboard routing / UI kit | TanStack Router, shadcn/ui | **React Router v7**, hand-written shadcn-style components | Fewer dependencies and no CLI code generation. |
| Command-key distribution | `Welcome` carries `command_pubkeys` | **Keys are pinned once, at enrollment** (`EnrollResponse.command_keys`). `Welcome` carries none. | If a gateway could hand out keys on every connection, a compromised gateway could inject its own. Enrollment is trust-on-first-use over TLS. Signed key rotation is planned for Phase 2. |
| Shell execution | part of `exec` | **Separate capability `exec.shell`**, which local policy can disable independently of argv-only `exec` | This gives finer control to device owners. |
| Stored output | 10 MiB inline | **256 KiB per stream** stored in `command_results`. Up to 1 MiB per stream is streamed live over SSE. | A result must fit in one 1 MiB protocol frame. Larger output goes to object storage in Phase 2. |
| Command states | `acked` then `running` | `CommandAck` moves a command straight to **`running`**. `acked` is kept in the schema for later use. | The MVP acknowledges and starts in the same step. |
| Cancel | — | `queued` commands are canceled immediately. For delivered commands, `CommandCancel` goes to the agent, which kills the process tree and reports `canceled`. | — |
| Result delivery | — | The agent keeps an in-memory outbox (500 results) that it flushes on reconnect. The worker fails commands still running at `timeout + 120 s` with no result. | Delivery is at-most-once after a socket write. A durable on-disk outbox is planned for Phase 2. |
| Offline detection | 90 s / 15 s grace | Offline is marked 15 s after a clean disconnect (gateway timer). The worker reaper closes sessions not refreshed for 90 s and marks devices without a live session offline, which covers crashed gateways. | — |
| Rate limits | Redis at scale | **In-memory per replica** (token bucket) | The effective limit scales with the replica count. A shared Redis limiter is planned for Phase 2. |
| Audit immutability | Revoke UPDATE/DELETE from the app role | A **database trigger** blocks UPDATE, DELETE and TRUNCATE for every role, including the owner. The worker re-verifies the hash chain every 6 h, and `GET /v1/audit/verify` does it on demand. | The trigger does not depend on how the database roles are provisioned. External anchoring (S3 Object Lock) is planned for Phase 2. |
| Audit partitioning | monthly | Not partitioned yet | Planned for Phase 2. |
| Heartbeat history | sampled + partitioned | As planned: at most one row per device per 5 min in daily partitions, created and dropped by the worker, with 30-day retention. | — |
| Deferred tables | — | `device_groups`, `artifacts*`, `agent_versions`, `device_artifacts`, `rollouts`, `remote_sessions` and `device_metrics` are left out of migration 00001. | They will arrive with the features that use them (Phase 2 and later). |
| Email uniqueness | per org | Globally unique | Login is by email only. This will be revisited with SSO and multiple orgs. |
| macOS agent | Phase 2 | The core builds and runs in the foreground on darwin. `install` reports "Phase 2". | Service install and packaging are planned for Phase 2. |

**What was verified end to end on the development machine (2026-09-28).**

- **Linux agent (container), full lifecycle:** enroll → pending → approve → online, inventory, argv exec, shell exec with exit-code propagation, actions, timeout with process-group kill, and cancel.
- **Revocation:** immediate disconnect, and the agent stays halted across restarts.
- **Windows 11 agent (native):** DPAPI-protected identity, registry-based OS info, PowerShell, and tree kill on timeout.
- **Resilience:** gateway graceful drain with jittered reconnect (about 11 s), and a duplicate identity being superseded.
- **Access control:** RBAC matrix, escalation guard, idempotency, and the CSRF guard on refresh.
- **Sessions and audit:** refresh-token reuse revokes the session family; the append-only audit trigger holds; audit hash chain verification passes.
- **Live events:** SSE delivers live output.
- **Dashboard:** 10 checks in headless Edge, with no CSP violations.
- **Automated test:** `tests/e2e` covers the above flows and passes.

---

## Appendix A — Technology recommendations

| Area | Recommendation | Why | Alternatives |
|---|---|---|---|
| Backend | **Go 1.24+**, modular monolith, 3 binaries | Excellent concurrency for many long-lived connections, static binaries, same language as desktop agents | Rust (more safety, slower development), Java/Kotlin (heavier), Node (weaker for CPU and long-lived connection density) |
| Database | **PostgreSQL 16/17** | Transactions, JSONB for inventory, partitioning, mature operators; TimescaleDB extension path for metrics | CockroachDB (global scale, more ops cost), MySQL |
| Agent communication | **WSS + Protobuf** | See §5.1 | gRPC streaming, MQTT, QUIC |
| Message/event system | **NATS** (JetStream later) | Tiny, fast, subject-based routing that fits `dev.<id>.cmd` exactly, simple clustering | Redis Pub/Sub (no durability path), Kafka (overkill until analytics scale), Postgres LISTEN/NOTIFY (fine at 100 devices, poor at 100k) |
| User authentication | Built-in (argon2id + JWT + rotating refresh) → **OIDC** (Keycloak/Entra/Google) Phase 4 | Full control now; enterprise SSO later | Keycloak/Ory/Zitadel from day one (more infrastructure), Auth0 (SaaS dependency) |
| Device authentication | **P-256 keypair + PoP → short JWT** | Works through any LB, hardware-key compatible, no CA ops in MVP | mTLS with an internal CA (step-ca) — can be added for regulated customers |
| Web dashboard | **React + TS + Vite + TanStack + Tailwind/shadcn** | Largest ecosystem, fast development, typed API client | Vue/Nuxt, SvelteKit, Angular |
| Android apps | **Kotlin + Jetpack Compose** | Google's official modern stack; required for DPC APIs | Flutter / React Native (fine for control app, not for DPC agent) |
| Artifact storage | **S3-compatible** (MinIO locally/on-prem, S3/GCS/R2 in cloud) | Immutable objects, presigned URLs, CDN-friendly | OCI registry via ORAS (nice for signing via cosign), GitHub Releases (not private enough) |
| Artifact signing | **Ed25519 release key (offline/HSM)** + OS code signing; TUF later | Simple and strong | Sigstore/cosign keyless (great for CI provenance; can be added alongside) |
| Metrics (platform) | **Prometheus + Grafana** (VictoriaMetrics at scale) | Standard for k8s | Datadog, New Relic |
| Metrics (device) | Postgres partitioned → **TimescaleDB/VictoriaMetrics** at 10k+ | Start simple, same DB | InfluxDB, ClickHouse |
| Logs | **slog JSON → Loki** (platform); agent logs → object storage + Loki (P2) | Cheap, label-based, Grafana-native | ELK/OpenSearch (heavier), ClickHouse |
| Tracing | **OpenTelemetry → Tempo/Jaeger** | Vendor-neutral | Vendor APM |
| Remote sessions | **Custom session-relay over WSS**, xterm.js, PTY/ConPTY | Fits outbound-only model and RBAC/audit | Apache Guacamole (good for RDP/VNC-in-browser; could be embedded behind relay), Teleport (a product, not a component) |
| Deployment | **Docker Compose (dev) → Kubernetes + Helm**, cert-manager, External Secrets, CloudNativePG | Standard, portable | Nomad, managed PaaS (Fly/Render) |
| CI/CD | **GitHub Actions**, goreleaser for agents, cosign for images | Cross-compile matrix, signing built in | GitLab CI, Jenkins (a stopped Jenkins exists locally, if preferred) |

## Appendix B — Local development ports

These were checked on 2026-09-28. Host ports **80, 3000, 5432 (vmcc-postgres), 6000** and the Windows system ports (135, 139, 445, 5040, 7680) are already in use and **will not be reused**. All host ports are configurable in `infrastructure/docker/.env`.

| Service | Host port | Container port |
|---|---|---|
| PostgreSQL | **15432** | 5432 |
| NATS client / monitoring | **14222 / 18222** | 4222 / 8222 |
| control-api | **18080** | 8080 |
| agent-gateway | **18443** | 8443 |
| dashboard (Vite dev / nginx) | **13000** | 3000 / 80 |
| MinIO API / console (P2) | **19000 / 19001** | 9000 / 9001 |

## Appendix C — Open decisions for review

These are the recommended defaults. Confirm or change them before implementation starts.

1. **Device auth:** P-256 proof-of-possession + short JWT (recommended), or full mTLS with an internal CA from day one?
2. **Event bus in MVP:** NATS from the start (recommended; avoids a later migration), or Postgres LISTEN/NOTIFY first?
3. **Arbitrary `exec` in MVP:** allowed for Admin+ only, and disable-able on the device (recommended), or predefined actions only?
4. **Go module path:** `github.com/enfec/agentmesh`, or another org/repo name?
5. **Dashboard stack:** React + Vite + shadcn (recommended), or a different preference?
6. **Multi-tenancy:** `org_id` everywhere from day one (recommended), even though MVP runs a single org?
