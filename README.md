# AgentMesh

> ## ⬇️ Direct downloads
> | App | Direct link |
> |---|---|
> | 📱 **Android app v0.6.3 (APK)** | **[Download agentmesh-control-v0.6.3.apk](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-control-v0.6.3.apk)** · always-latest: [agentmesh-control.apk](https://github.com/devopshubtech/AgentMesh/releases/latest/download/agentmesh-control.apk) |
> | 🍎 **macOS agent, Apple Silicon (M1–M4)** | **[agentmesh-agent_0.6.3_darwin_arm64.tar.gz](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_darwin_arm64.tar.gz)** |
> | 🍎 **macOS agent, Intel** | **[agentmesh-agent_0.6.3_darwin_amd64.tar.gz](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_darwin_amd64.tar.gz)** |
> | 🪟 **Windows agent (x64)** | **[agentmesh-agent_0.6.3_windows_amd64.zip](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_windows_amd64.zip)** |
> | 🐧 **Linux agent (.deb x64)** | **[agentmesh-agent_0.6.3_amd64.deb](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_amd64.deb)** |
> | 🖥️ **Server for Mac mini (native, Apple Silicon)** | **[agentmesh-server_0.6.3_macos_arm64.tar.gz](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-server_0.6.3_macos_arm64.tar.gz)** · Intel: [agentmesh-server_0.6.3_macos_amd64.tar.gz](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-server_0.6.3_macos_amd64.tar.gz) · see [Run the server on a Mac mini](#run-the-server-on-a-mac-mini) |
>
> See [all downloads](#downloads) for ARM, `.rpm`, Raspberry Pi and one-line installers. Every release: [Releases page](https://github.com/devopshubtech/AgentMesh/releases/latest).
>
> **macOS install:** `tar -xzf agentmesh-agent_*_darwin_*.tar.gz && sudo ./install.sh --server https://<gateway>:18443 --token am_enr_...`
> **Android install:** open the APK link on the phone and allow "Install unknown apps" when asked.
> **Need help?** Report a problem or ask a question on the [Issues page](https://github.com/devopshubtech/AgentMesh/issues).

## 🖥️ Run the server on a Mac mini

Native install, no Docker: the AgentMesh server, NATS, a Caddy web front and cloudflared run as launchd services that start at boot. The database is an external Postgres, for example Neon from Vercel.

```bash
# Apple Silicon (M1–M4); use _macos_amd64 on an Intel Mac
curl -fLO https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-server_0.6.3_macos_arm64.tar.gz
tar -xzf agentmesh-server_0.6.3_macos_arm64.tar.gz && cd agentmesh-server
./install.sh
```

- **First run** asks for the dashboard admin email and password (12+ characters; press Enter to generate one) and the database URL. It installs to `/usr/local/agentmesh`, generates keys and certificates, starts the services and creates the database tables. The dashboard is at **https://localhost:13443**.
- **Database:** use the **direct (unpooled)** connection string; the installer refuses a `-pooler` one. To skip the question, put `AM_DATABASE_URL=<url>` in a file named `agentmesh-db.env` next to `install.sh`. Never commit or publish that file; it is git-ignored.
- **Public URL (permanent):** create a named tunnel in Cloudflare Zero Trust with a public hostname whose service is `https://localhost:13443` with *No TLS Verify*, then run `./install.sh --tunnel-token <token> --public-url https://api.example.com`.
- **Public URL (temporary, for testing):** `./install.sh --quick-tunnel` prints a `trycloudflare.com` address.
- **Use the Mac as an exit node too:** add `--agent-token am_enr_...` (from **Enrollment → New token**).
- **Update:** run the new package's `install.sh`; settings, keys and certificates in `/usr/local/agentmesh/etc` are kept. **Remove:** `./install.sh --uninstall` (add `--purge` to delete settings too).
- **Logs** are in `/usr/local/agentmesh/log/`. Restart a service with `sudo launchctl kickstart -k system/com.agentmesh.control-api`.
- **Dashboard on Vercel:** set the Mac's public URL in [dashboard/vercel.json](dashboard/vercel.json); Vercel forwards `/v1/*` to it, so login cookies keep working.
- **Build the packages:** `scripts/build-macos-server.sh v0.6.3` (needs Go, Node and curl). The Docker-based alternative is `scripts/install/macos-server.sh`.

## 📱 Use a computer's internet on your phone (the 1-minute version)

1. **On the computer:** run `powershell -ExecutionPolicy Bypass -File scripts\start-agentmesh.ps1`. Optionally also run `scripts\install-autostart.ps1` so it keeps running and heals itself after sleep or a reboot.
2. **Create a QR code or pairing code:** open **http://localhost:13000**, sign in with the admin account, and go to **Connect a phone**. Then click **Create connect QR code**, or click **Create pairing code** for a 6-digit code that works for 15 minutes.
3. **On any phone:** open the AgentMesh app and tap **Connect to remote server**. Then either scan the QR code, or type the 6-digit code. Opening the QR link from the camera also works. No username, password or domain is needed; the app finds the computer's current address by itself.
4. **Result:** the phone's internet now goes out through the computer. "What is my IP" shows the computer's IP, on mobile data or any Wi-Fi.

Other details:

- **Many phones, one link:** many phones can use the same QR code at the same time.
- **See who's connected:** **Connect a phone → Phones connected now** lists them, and you can disconnect any of them.
- **Revoke:** revoking a link disconnects its phones immediately.
- **Saved servers:** the app keeps them in a list; tap one to reconnect, or use **Edit** to rename it or change its address.
- **Speed:** everything the phone downloads is *uploaded* by the computer, so the phone's speed is capped by the computer's **upload** speed (check it at speed.cloudflare.com), not by the phone's own connection. On a connection with 2 Mbit/s upload, video will be slow. Use an exit computer with good upload for full speed.
- **Network changes:** when the phone switches between Wi-Fi and mobile data, or the link to the computer drops, the app reconnects in the background. The VPN stays on, so apps don't lose their network.
- **Address changes:** the public address changes whenever the free tunnel restarts. The start script publishes the current address to a private GitHub gist, and the app looks it up automatically, so **old QR codes keep working**.

A cross-platform device management and secure remote-access platform. You install a lightweight **agent** on each device. The agent dials **out** to a Go **control plane**, so the device never needs an inbound port. You manage the fleet from a **web dashboard** or the **Android app**.

- **Design:** [docs/architecture.md](docs/architecture.md) covers all 20 architecture sections. It also has an **Implementation notes** section recording where the MVP differs from the original plan.
- **REST contract:** [docs/api.md](docs/api.md)
- **Agent protocol:** [protocols/agent/v1/agent.proto](protocols/agent/v1/agent.proto)

**Status: Phase 1 (MVP) is complete.**

| Area | What it covers |
|---|---|
| Backend | Go backend (control-api, agent-gateway, worker), PostgreSQL, NATS |
| Devices | Enrollment tokens with approval; device authentication with a P-256 key proof → short-lived token |
| Agent connection | WSS + Protobuf protocol; heartbeat and inventory; online/offline status |
| Agents | Linux agent (systemd, .deb/.rpm), Windows agent (Windows Service, DPAPI), macOS agent (launchd; built but not yet tested on a Mac) |
| Android | Control app with exit node: route the phone's traffic through an agent device |
| Commands | Signed command execution with live output, cancel, timeouts and replay protection |
| Security | RBAC (Super Admin, Admin, Operator, Viewer); append-only, hash-chained audit log |
| Dashboard | React web dashboard |
| Deployment | Docker Compose; reference Kubernetes manifests |

Phases 2 to 4 (macOS, artifacts and updates, metrics, mobile, remote sessions) are planned in the architecture doc.

---

## Downloads

Every file is attached to this repo's **[Releases](https://github.com/devopshubtech/AgentMesh/releases/latest)**, along with a `SHA256SUMS` file.

| Platform | Download (v0.6.3) |
|---|---|
| **Android app** (control + exit node) | [agentmesh-control.apk](https://github.com/devopshubtech/AgentMesh/releases/latest/download/agentmesh-control.apk) |
| Windows x64 | [agentmesh-agent_0.6.3_windows_amd64.zip](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_windows_amd64.zip) |
| Windows ARM64 | [agentmesh-agent_0.6.3_windows_arm64.zip](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_windows_arm64.zip) |
| macOS Apple Silicon | [agentmesh-agent_0.6.3_darwin_arm64.tar.gz](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_darwin_arm64.tar.gz) |
| macOS Intel | [agentmesh-agent_0.6.3_darwin_amd64.tar.gz](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_darwin_amd64.tar.gz) |
| Debian / Ubuntu x64 | [agentmesh-agent_0.6.3_amd64.deb](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_amd64.deb) |
| Debian / Ubuntu ARM64 | [agentmesh-agent_0.6.3_arm64.deb](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_arm64.deb) |
| Raspberry Pi / ARMv7 (.deb) | [agentmesh-agent_0.6.3_armhf.deb](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_armhf.deb) |
| RHEL / Fedora / Rocky x64 | [agentmesh-agent-0.6.3-1.x86_64.rpm](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent-0.6.3-1.x86_64.rpm) |
| RHEL / Fedora ARM64 | [agentmesh-agent-0.6.3-1.aarch64.rpm](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent-0.6.3-1.aarch64.rpm) |
| Any Linux (static binary) | [x64](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_linux_amd64.tar.gz) · [ARM64](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_linux_arm64.tar.gz) · [ARMv7](https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.3/agentmesh-agent_0.6.3_linux_armv7.tar.gz) |

**One-line install.** These always pull the latest release and check the SHA-256 before installing. Get `<gateway>` and the token from the dashboard's **Enrollment** page. Add `--ca-file ca.pem` when the server uses a private or development CA, and `--enable-exit-node` to let this device act as an exit node.

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/devopshubtech/AgentMesh/main/scripts/install/install.sh \
  | sudo sh -s -- --server https://<gateway>:18443 --token am_enr_...
```

```powershell
# Windows (elevated PowerShell)
irm https://raw.githubusercontent.com/devopshubtech/AgentMesh/main/scripts/install/install.ps1 -OutFile install.ps1
.\install.ps1 -Server https://<gateway>:18443 -Token am_enr_...
```

**Installing a package directly:**
- `.deb` / `.rpm`: `sudo apt install ./agentmesh-agent_*.deb` or `sudo dnf install ./agentmesh-agent-*.rpm`, then `sudo agentmesh-agent install --server … --token …`.
- zip / tar.gz: extract, then run the bundled `install.ps1` / `install.sh` with the same arguments.

**Code signing (not done yet).** The agent binaries are not code-signed. Windows SmartScreen may warn; choose "More info → Run anyway". On macOS, the installer clears the quarantine flag. The APK is signed with the project's release key; allow "Install unknown apps" on the phone.

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

## Android control app + exit node

The Android app (`mobile/android-control`) lets an operator:

- sign in and see devices, their status and inventory;
- run actions and commands, and approve, disable or enable devices;
- **route the phone's internet traffic through an agent device** (the *exit node*). Websites then see that device's IP address.

**How it works.** The phone runs an Android VPN (`VpnService`). A Go engine, bound with gomobile, terminates each TCP or UDP flow and carries it over the AgentMesh relay (WSS to the agent-gateway) to the agent. The agent dials the real destination with ordinary sockets, so it needs no drivers and works on Windows, macOS and Linux, wherever the agent runs, as long as it can reach the backend.

```text
Phone apps ─▶ Android VPN (TUN) ─▶ Go engine ═WSS/yamux═▶ agent-gateway relay ═WSS═▶ agent ─▶ internet
```

**1. Allow the exit node on the device.** This is the device owner's decision, so it lives in the local policy (off by default):

```powershell
agentmesh-agent install --server ... --token ... --enable-exit-node
```

Or edit `<state-dir>/agent.json` → `"policy": { "allow_exit_node": true }` and restart the agent. By default the agent refuses destinations on its own machine or private LAN. Add `"exit_node_allow_lan": true` to permit LAN destinations.

**2. Build the APK.** Everything runs in Docker; no local Android SDK is needed.

```powershell
docker build -f infrastructure/docker/android-build.Dockerfile -t agentmesh/android-build:dev infrastructure/docker
docker run --rm -v "${PWD}:/src" -v agentmesh-gradle:/root/.gradle -v agentmesh-gomod:/root/go/pkg/mod `
  agentmesh/android-build:dev sh mobile/android-control/build.sh
# → dist/agentmesh-control.apk (release-signed with a local key in mobile/android-control/keystore/, git-ignored)
```

**Use it on mobile data or any network.** Give the server a public HTTPS address. With the Cloudflare quick tunnel it takes one command, needs no router changes, and comes with a trusted certificate:

```powershell
docker compose -f infrastructure/docker/docker-compose.yml --profile public up -d tunnel
docker compose -f infrastructure/docker/docker-compose.yml logs tunnel | Select-String trycloudflare.com
```

- **Point the server at it:** set `AM_PUBLIC_GATEWAY_URL` in `infrastructure/docker/.env` to that URL, then run `docker compose ... up -d control-api`.
- **In the app:** go to **Switch server → Add**, enter the URL and tap *Continue (publicly trusted certificate)*.
- **Relay path:** the relay (`/v1/relay`) and agent endpoints (`/v1/agent/*`) are served on the same URL, so remote agents can enroll with it too.
- **The address changes when the tunnel restarts.** For a permanent one, use a named Cloudflare tunnel with your own domain, or deploy the server on a public host.
- **Privacy:** Cloudflare terminates TLS for the tunnel. Websites' own HTTPS stays end to end, but Cloudflare can see the traffic metadata.

**3. Connect the phone.**

- **Server URL:** `https://<laptop-LAN-IP>:13443` (for example `https://192.168.30.144:13443`).
- **Settings:** set `AM_PUBLIC_GATEWAY_URL=https://<LAN-IP>:18443` in `infrastructure/docker/.env`. Then re-issue the certificate so it includes the LAN IP: `go run ./backend/cmd/amctl dev-certs -out infrastructure/docker/certs -hosts localhost,127.0.0.1,agent-gateway,dashboard,<LAN-IP>`. This reuses the existing CA.
- **Development CA:** choose *"Private / development server: trust its CA…"* in the app. Compare the SHA-256 fingerprint it shows with the one `amctl dev-certs` prints.
- **Sign in:** use a user with the `sessions.exit_node` permission (Admin or Super Admin). Open the device and tap **Use as exit node**, then **Check my IP**.

**Speed.** Exit-node traffic runs phone → relay → agent → internet, so it can never be faster than the agent's **upload** bandwidth or the relay path. Measured on the dev laptop: 115-125 Mbit/s through the relay directly, but only about 11 Mbit/s through a free Cloudflare *quick* tunnel, which is throttled. For near line-speed on mobile data, run the server on a public host (VPS) or use a named Cloudflare tunnel, or port-forward 13443 on the router. The app blocks QUIC (UDP/443) inside the tunnel, so browsers use TCP, which is much faster over the relay.

**Scope and limits (MVP).**

- **DNS:** the phone uses 1.1.1.1 / 1.0.0.1, resolved through the tunnel.
- **Local network:** on Android 13+ private LAN ranges stay on the local network. On older versions all traffic goes through the tunnel, so LAN destinations are refused by the agent.
- **Gateways:** both sides of a relay must reach the same gateway replica.
- **Duration:** sessions last at most 12 hours and are fully audited.

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
