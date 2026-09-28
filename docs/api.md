# AgentMesh control-api — REST contract (v1, MVP)

Base path: `/v1`. All request and response bodies are JSON with `snake_case` fields. Timestamps are RFC 3339 UTC strings. IDs are UUIDv7 strings.

## Conventions

**Auth.** Send `Authorization: Bearer <access_token>` on every endpoint except `/v1/auth/login` and `/v1/auth/refresh`.

**Errors.** Every error response has this shape:

```json
{ "error": { "code": "not_found", "message": "device not found", "request_id": "01J..." } }
```

| HTTP | code examples |
|---|---|
| 400 | `invalid_request`, `validation_failed` (the error then also has `"fields": {"email": "required"}`) |
| 401 | `unauthenticated`, `token_expired`, `invalid_credentials` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 409 | `conflict`, `invalid_state` |
| 429 | `rate_limited` (with a `Retry-After` header) |
| 500 | `internal` |

**Lists.** List endpoints return `{ "items": [...], "next_cursor": "opaque" | null }` and accept `?limit=` (1–200, default 50) and `?cursor=`.

**Request ID.** Every response carries an `X-Request-ID` header. Clients may send their own `X-Request-ID`.

---

## Auth

### POST /v1/auth/login
Request: `{ "email": "a@b.c", "password": "...", "client": "web" | "mobile" }` (`client` defaults to `"web"`).

Response `200`:
```json
{ "access_token": "eyJ...", "expires_in": 600, "user": User, "refresh_token": "only when client=mobile" }
```
- For `client=web` the server sets the cookie `am_refresh`: HttpOnly, SameSite=Strict, Path=/v1/auth.
- Rate limited per IP and per email.

### POST /v1/auth/refresh
- **Web:** the `am_refresh` cookie is sent automatically. The request **must** include the header `X-Requested-With: agentmesh`.
- **Mobile:** body `{ "refresh_token": "..." }`.

Response: the same shape as login. The refresh token is rotated. If a refresh token is reused, the whole session family is revoked and the call returns `401`.

### POST /v1/auth/logout
Revokes the current session family and clears the cookie. Returns `204`. Web clients must send `X-Requested-With: agentmesh`.

### GET /v1/me → `User`

```ts
User = {
  id: string, email: string, display_name: string,
  status: "active" | "disabled",
  role: "super_admin" | "admin" | "operator" | "viewer",
  permissions: string[],          // e.g. ["devices.read","commands.execute.action"]
  created_at: string, last_login_at: string | null
}
```

Permission keys: `devices.read`, `devices.manage`, `commands.read`, `commands.execute.action`, `commands.execute.exec`, `enrollment.manage`, `users.manage`, `audit.read`, `platform.admin`.

---

## Devices

```ts
Device = {
  id: string, name: string, hostname: string,
  status: "pending" | "active" | "disabled" | "revoked",
  connectivity: "online" | "offline",
  platform: "linux" | "windows" | "darwin" | "android" | "ios" | string,
  arch: string,                       // amd64, arm64, ...
  os_name: string, os_version: string, os_build: string, kernel_version: string,
  agent_version: string,
  capabilities: string[],             // "exec", "action.ping", "action.process.list", "action.inventory.refresh"
  inventory: Inventory | null,
  last_seen_at: string | null, last_ip: string | null,
  created_at: string, approved_at: string | null
}
Inventory = {
  cpu: { model: string, cores: number, threads: number },
  memory: { total_bytes: number, used_bytes: number },
  disks: { mount: string, fstype: string, total_bytes: number, used_bytes: number }[],
  network: { name: string, mac: string, addrs: string[] }[],
  uptime_s: number, boot_time: string | null,
  collected_at: string
}
```

| Method & path | Permission | Notes |
|---|---|---|
| `GET /v1/devices?status=&connectivity=&platform=&q=&limit=&cursor=` | devices.read | Sorted by name. `q` matches name or hostname (case-insensitive substring). |
| `GET /v1/devices/summary` | devices.read | `{ "total": 5, "online": 3, "offline": 2, "pending": 1, "disabled": 0, "revoked": 0 }` (connectivity counts exclude revoked devices) |
| `GET /v1/devices/{id}` | devices.read | → `Device` |
| `PATCH /v1/devices/{id}` | devices.manage | `{ "name": "Server-01" }` → `Device` |
| `POST /v1/devices/{id}/approve` | devices.manage | pending → active. Anything else → 409 `invalid_state` |
| `POST /v1/devices/{id}/disable` | devices.manage | active → disabled; the agent is disconnected |
| `POST /v1/devices/{id}/enable` | devices.manage | disabled → active |
| `POST /v1/devices/{id}/revoke` | devices.manage | Any state except revoked → revoked (**permanent**). The agent is disconnected and its queued commands are canceled. |
| `GET /v1/devices/{id}/activity?limit=&cursor=` | audit.read **or** devices.read | Audit entries whose target is this device → `{items: AuditEntry[], next_cursor}` |

---

## Commands

```ts
Command = {
  id: string, device_id: string,
  requested_by: { id: string, email: string },
  kind: "exec" | "action",
  action: string | null,              // for kind=action: "ping" | "process.list" | "inventory.refresh"
  argv: string[] | null,              // for kind=exec
  shell: boolean,
  timeout_s: number,
  status: "queued" | "sent" | "acked" | "running" | "succeeded" | "failed"
        | "timed_out" | "canceled" | "expired" | "rejected",
  created_at: string, expires_at: string,
  sent_at: string | null, started_at: string | null, finished_at: string | null,
  result: null | {
    exit_code: number | null, stdout: string, stderr: string,
    truncated: boolean, error: string | null, duration_ms: number
  }
}
```

| Method & path | Permission | Notes |
|---|---|---|
| `GET /v1/devices/{id}/commands?limit=&cursor=` | commands.read | Newest first |
| `POST /v1/devices/{id}/commands` | exec → `commands.execute.exec`; action → `commands.execute.action` | Body `{ "kind": "exec", "argv": ["uname","-a"], "shell": false, "timeout_s": 60 }` or `{ "kind": "action", "action": "ping" }`. Optional header `Idempotency-Key`. Returns `202` with a `Command`. The device must be `active` and must have declared the matching capability: `exec` for argv commands, `exec.shell` for `shell: true`, and `action.<name>` for actions. Otherwise the call returns 409 `invalid_state` or 400. `timeout_s` is 1–3600, default 60. With `shell: true`, `argv` must have exactly one element: the script. |
| `GET /v1/commands/{id}` | commands.read | → `Command` |
| `POST /v1/commands/{id}/cancel` | the same permission that was needed to create it | Only for non-terminal states → `Command` |

Terminal states are `succeeded`, `failed`, `timed_out`, `canceled`, `expired` and `rejected`.

---

## Enrollment tokens

```ts
EnrollmentToken = {
  id: string, description: string, auto_approve: boolean,
  max_uses: number | null, uses: number,
  expires_at: string, revoked_at: string | null,
  created_at: string, created_by: { id: string, email: string }
}
```

| Method & path | Permission | Notes |
|---|---|---|
| `GET /v1/enrollment-tokens?limit=&cursor=` | enrollment.manage | Newest first |
| `POST /v1/enrollment-tokens` | enrollment.manage | Body `{ "description": "...", "auto_approve": false, "max_uses": 10 \| null, "expires_in_s": 86400 }` (`expires_in_s` is 300–2592000). Returns `201` with `EnrollmentToken & { "token": "am_enr_..." }`. **The token is returned only once.** |
| `DELETE /v1/enrollment-tokens/{id}` | enrollment.manage | Revokes the token. Returns `204` |

---

## Audit

```ts
AuditEntry = {
  id: number, ts: string, request_id: string | null,
  actor_type: "user" | "device" | "system", actor_id: string | null, actor_label: string | null,
  actor_ip: string | null,
  action: string,                     // "auth.login", "device.revoke", "command.create", "command.result", ...
  target_type: string | null, target_id: string | null,
  outcome: "success" | "denied" | "error",
  details: object
}
```

| Method & path | Permission | Notes |
|---|---|---|
| `GET /v1/audit?action=&actor_id=&target_id=&outcome=&from=&to=&limit=&cursor=` | audit.read | Newest first. `action` supports a prefix match with a trailing `*`, for example `device.*`. |
| `GET /v1/audit/verify` | platform.admin | `{ "ok": true, "checked": 1234, "first_bad_id": null }` |

---

## Users & roles

| Method & path | Permission | Notes |
|---|---|---|
| `GET /v1/roles` | users.manage | `[{ "name": "operator", "description": "...", "permissions": [...] }]` |
| `GET /v1/users?limit=&cursor=` | users.manage | → list of `User` |
| `POST /v1/users` | users.manage | `{ "email", "display_name", "password" (min 12 chars), "role" }` → `201` with a `User`. You cannot grant a role that holds permissions you lack. |
| `PATCH /v1/users/{id}` | users.manage | `{ "display_name"?, "status"?, "role"?, "password"? }` → `User`. You cannot modify a user who holds permissions you lack, and you cannot disable yourself or change your own role. |

---

## Live events — GET /v1/events (Server-Sent Events)

Authenticate with the `Authorization` header, which means reading the stream with `fetch` because `EventSource` cannot set headers. The server sends a comment line `: ping` every 20 s. Events:

```text
event: device.status     data: {"device_id":"...","status":"active","connectivity":"online","last_seen_at":"..."}
event: device.updated    data: {"device_id":"..."}                       # inventory/name changed → refetch
event: command.updated   data: {"command_id":"...","device_id":"...","status":"running"}
event: command.output    data: {"command_id":"...","device_id":"...","stream":"stdout"|"stderr","seq":3,"data":"<utf8 text>"}
```

The stream closes when the access token expires. The client should reconnect with a fresh token.

---

## Config

`GET /v1/config` (any authenticated user) → `{ "gateway_url": "https://localhost:18443", "version": "0.1.0" }`. The dashboard uses `gateway_url` to build agent install commands.

Agent install commands (the dashboard shows these with the token filled in):

```text
Linux   (root):   sudo ./agentmesh-agent install --server <gateway_url> --token <token> [--ca-file ./ca.pem]
Windows (admin):  .\agentmesh-agent.exe install --server <gateway_url> --token <token> [--ca-file .\ca.pem]
```

---

## Health

`GET /healthz` returns 200 when the process is alive. `GET /readyz` returns 200 when the DB and NATS are reachable. `GET /metrics` serves Prometheus metrics.
