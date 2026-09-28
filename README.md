# AgentMesh

A cross-platform device management and secure remote-access platform. You install a lightweight **agent** on each device. The agent dials **out** to a Go **control plane**, so the device never needs an inbound port. You manage the fleet from a **web dashboard**; an Android control app comes later.

- **Design:** [docs/architecture.md](docs/architecture.md) covers all 20 architecture sections. It also has an **Implementation notes** section recording where the MVP differs from the original plan.
- **REST contract:** [docs/api.md](docs/api.md)
- **Agent protocol:** [protocols/agent/v1/agent.proto](protocols/agent/v1/agent.proto)

**Status: Phase 1 (MVP) is complete.**

| Area | What it covers |
|---|---|
| Backend | Go backend (control-api, agent-gateway, worker), PostgreSQL, NATS |
| Devices | Enrollment tokens with approval; device authentication with a P-256 key proof → short-lived token |
| Agent connection | WSS + Protobuf protocol; heartbeat and inventory; online/offline status |
| Agents | Linux agent (systemd) and Windows agent (Windows Service, DPAPI) |
| Commands | Signed command execution with live output, cancel, timeouts and replay protection |
| Security | RBAC (Super Admin, Admin, Operator, Viewer); append-only, hash-chained audit log |
| Dashboard | React web dashboard |
| Deployment | Docker Compose; reference Kubernetes manifests |

Phases 2 to 4 (macOS, artifacts and updates, metrics, mobile, remote sessions) are planned in the architecture doc.

---

## Quick start (local)

Requirements: Docker Desktop, Go 1.26+, and Node 24 (only needed for dashboard development).

```powershell
# 1. Generate .env (ports, DB password, admin login), signing keys and dev TLS certs
powershell -ExecutionPolicy Bypass -File scripts\dev-setup.ps1      # Linux/macOS: ./scripts/dev-setup.sh

# 2. Build and start the stack
docker compose -f infrastructure/docker/docker-compose.yml up -d --build
```

Open **http://localhost:13000**. Sign in with the admin email and password printed by `dev-setup`; they are also stored in `infrastructure/docker/.env`.

### Local ports

The host ports are deliberately non-default, because 80, 3000, 5432 and 6000 are already used on the development machine. You can change them in `infrastructure/docker/.env`.

| Service | Host port | Notes |
|---|---|---|
| Dashboard (nginx → API proxy) | 13000 | bound to 127.0.0.1 |
| control-api | 18080 | bound to 127.0.0.1 |
| agent-gateway (TLS) | 18443 | bound to all interfaces so VMs and other machines can enroll |
| PostgreSQL | 15432 | bound to 127.0.0.1 |
| NATS / NATS monitoring | 14222 / 18222 | bound to 127.0.0.1 |

---

## Connecting devices

1. In the dashboard, open **Enrollment → New token**. The token is shown only once.
2. On the device, run the install command the dashboard shows, which looks like the examples below. `--ca-file` is only needed with the self-signed development CA in `infrastructure/docker/certs/ca.pem`.

**Linux (as root)**

```bash
sudo ./agentmesh-agent install --server https://<gateway-host>:18443 --token am_enr_... --ca-file ./ca.pem
```

This installs `/usr/local/bin/agentmesh-agent` and the systemd unit `agentmesh-agent.service`, and stores state in `/var/lib/agentmesh`.

**Windows (as Administrator)**

```powershell
.\agentmesh-agent.exe install --server https://<gateway-host>:18443 --token am_enr_... --ca-file .\ca.pem
```

This installs `C:\Program Files\AgentMesh` and the `AgentMesh` service, and stores state in `C:\ProgramData\AgentMesh` (ACL: SYSTEM and Administrators only; key encrypted with DPAPI).

3. If the token did not auto-approve, approve the device in **Devices**. The agent connects within about 15 seconds.

Other agent commands: `status`, `run` (foreground), `enroll --force` (new identity), `uninstall [--purge]`, `version`.

**Local policy.** Device owners can restrict the agent in `<state-dir>/agent.json`, which is owned by root or Administrator. The control plane cannot override these settings:

```json
{ "policy": { "allow_exec": false, "allow_shell": false, "disabled_actions": ["process.list"], "max_concurrent_commands": 4 } }
```

**Try it without a VM.** A containerized Linux device is included:

```powershell
$env:AM_DEMO_ENROLL_TOKEN = "am_enr_..."
docker compose -f infrastructure/docker/docker-compose.yml --profile demo up -d --build demo-agent
```

**Build agents yourself:**

```powershell
$env:CGO_ENABLED=0; $env:GOOS="linux";   go build -trimpath -o dist/agentmesh-agent-linux-amd64 ./agents/linux
$env:GOOS="windows"; go build -trimpath -o dist/agentmesh-agent-windows-amd64.exe ./agents/windows; $env:GOOS=""
```

On Linux or macOS you can use `make agents` instead.

---

## Development

| Task | Command |
|---|---|
| Unit tests (Go) | `go test ./...` |
| End-to-end test (needs the stack running) | `go test -tags e2e -v ./tests/e2e` |
| Lint / vet | `gofmt -l backend agents protocols/agentapi tests` and `go vet ./...` |
| Regenerate protobuf | `buf lint; buf generate` (install `buf` and `protoc-gen-go` via `go install`) |
| Dashboard dev server | `cd dashboard; npm install; npm run dev` → http://localhost:13000, proxying `/v1` to :18080. Stop the `dashboard` container first, because both use port 13000. |
| Dashboard checks | `npm run typecheck; npm run lint; npm test; npm run build` |
| Service logs | `docker compose -f infrastructure/docker/docker-compose.yml logs -f control-api agent-gateway worker` |

> **Windows note:** if Smart App Control / Application Control blocks the Go test binaries it builds in `%TEMP%`, run the e2e test inside a container on the compose network. The CI workflow runs it natively on Linux.
>
> ```powershell
> docker run --rm --network agentmesh_default -v "${PWD}:/src" -w /src -e AGENTMESH_E2E_API=http://control-api:8080 `
>   -e AGENTMESH_E2E_GATEWAY=https://agent-gateway:8443 -e AGENTMESH_E2E_CA=/src/infrastructure/docker/certs/ca.pem `
>   golang:1.26-alpine go test -tags e2e -v ./tests/e2e
> ```

**Operator CLI (`amctl`):**

- `keygen`: prints fresh signing keys.
- `dev-certs -out DIR`: creates a local CA and gateway certificate.
- `migrate up|status`: applies or shows schema migrations.
- `health URL`: container health probe.

---

## Repository layout

```text
backend/            Go control plane
  cmd/              control-api, agent-gateway, worker, amctl
  internal/         api (HTTP), auth, users, devices, enrollment, commands, audit, events (SSE),
                    gateway (agent WS), worker (jobs), platform/* (config, db, bus, httpx, keys, ...)
  migrations/       SQL schema (goose, embedded)
agents/
  core/             shared Go agent: identity, enrollment, WSS session, verifier, executor, inventory,
                    platform_{linux,windows,darwin}.go
  linux/ windows/   thin entry points (packaging lands here)
protocols/
  agent/v1/         agent ⇄ gateway Protobuf schema
  agentapi/         shared HTTP types + exact signed byte strings (imported by agent and gateway)
  gen/go/           generated code (committed)
dashboard/          React 19 + TypeScript + Vite + TanStack Query + Tailwind
infrastructure/
  docker/           Dockerfiles, docker-compose.yml, .env.example
  kubernetes/       reference Kustomize base (+ secrets.example.yaml)
tests/e2e/          end-to-end test against a running stack
scripts/            dev-setup.ps1 / dev-setup.sh
docs/               architecture, API contract
```

---

## Security at a glance

- **Devices.** Each device has a non-exportable P-256 key and gets a key at enrollment. It proves possession of that key through a single-use nonce to obtain a 15-minute token. Revocation disconnects the device immediately.
- **Commands.** Every command is Ed25519-signed by control-api, and the agent verifies the signature against a key pinned at enrollment. Each command is bound to its device and organization, has an expiry checked against server-adjusted time, and a replay set persisted on disk blocks reuse. A compromised gateway or NATS therefore cannot forge commands.
- **Users.** Passwords use argon2id. Access tokens (EdDSA JWT) live for 10 minutes and are held only in memory in the browser. Refresh tokens are HttpOnly and SameSite=Strict, rotate on every use, and reusing an old one revokes the whole session family. The refresh endpoint also requires a CSRF header. Each request re-checks that the session and user are still active.
- **Authorization.** RBAC checks permissions, not role names. A user cannot grant permissions they do not hold. Denied attempts are audited.
- **Audit.** Every audit row is hash-chained per organization, UPDATE, DELETE and TRUNCATE are blocked by a database trigger, and a worker re-verifies the chain every 6 hours.
- **Transport.** TLS is required outside dev mode. Rate limits apply to login, refresh, enrollment, auth and command endpoints, and to each connection. The gateway enforces connection caps and frame and output size limits.

**Production checklist:** real TLS certificates; managed Postgres with TLS (`sslmode=verify-full`); secrets from a secret manager; the API reachable only through the ingress; and `AGENTMESH_TRUST_PROXY_HEADERS=true` only behind a trusted proxy.
