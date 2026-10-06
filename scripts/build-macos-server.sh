#!/usr/bin/env bash
# Builds the native macOS server packages (no Docker on the Mac):
#   dist/release/agentmesh-server_<ver>_macos_arm64.tar.gz   (Apple Silicon)
#   dist/release/agentmesh-server_<ver>_macos_amd64.tar.gz   (Intel)
# Each holds: AgentMesh server (control-api/agent-gateway/worker/amctl),
# NATS, Caddy (web front), cloudflared, the dashboard, this Mac's agent and
# packaging/macos/install.sh. Needs go, node/npm, curl, tar.
#   scripts/build-macos-server.sh v0.6.3
set -euo pipefail
VERSION=${1:?usage: build-macos-server.sh vX.Y.Z}
PKGVER=${VERSION#v}
NATS_VER=v2.11.17
CADDY_VER=2.11.7
CLOUDFLARED_VER=2026.10.0

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
OUT=dist/release STAGE=dist/stage-macos DL=dist/cache-macos
rm -rf "$STAGE" && mkdir -p "$OUT" "$STAGE" "$DL"

fetch() { [ -s "$2" ] || { echo ">> download $1"; curl -fsSL "$1" -o "$2.part" && mv "$2.part" "$2"; }; }

if [ -z "${SKIP_DASHBOARD:-}" ]; then # SKIP_DASHBOARD=1: reuse an existing dashboard/dist
  echo ">> dashboard"
  (cd dashboard && npm ci --no-audit --no-fund >/dev/null && npm run build >/dev/null)
fi
[ -f dashboard/dist/index.html ] || { echo "dashboard/dist is missing" >&2; exit 1; }

for arch in arm64 amd64; do
  d=$STAGE/agentmesh-server
  rm -rf "$d" && mkdir -p "$d/bin" "$d/agent" "$d/web"
  echo ">> agentmesh-server darwin/$arch"
  CGO_ENABLED=0 GOOS=darwin GOARCH=$arch GOTOOLCHAIN=local go build -trimpath \
    -ldflags "-s -w -X github.com/enfec/agentmesh/backend/internal/api.Version=${PKGVER}" \
    -o "$d/bin/agentmesh-server" ./backend/cmd/agentmesh-server
  echo ">> agentmesh-agent darwin/$arch"
  CGO_ENABLED=0 GOOS=darwin GOARCH=$arch GOTOOLCHAIN=local go build -trimpath \
    -ldflags "-s -w -X github.com/enfec/agentmesh/agents/core.Version=${PKGVER}" \
    -o "$d/agent/agentmesh-agent" ./agents/macos
  tr -d '\r' < scripts/install/install.sh > "$d/agent/install.sh"

  fetch "https://github.com/nats-io/nats-server/releases/download/$NATS_VER/nats-server-$NATS_VER-darwin-$arch.tar.gz" "$DL/nats-$arch.tgz"
  tar -xzf "$DL/nats-$arch.tgz" -C "$STAGE" && mv "$STAGE/nats-server-$NATS_VER-darwin-$arch/nats-server" "$d/bin/" && rm -rf "$STAGE/nats-server-$NATS_VER-darwin-$arch"
  fetch "https://github.com/caddyserver/caddy/releases/download/v$CADDY_VER/caddy_${CADDY_VER}_mac_$arch.tar.gz" "$DL/caddy-$arch.tgz"
  tar -xzf "$DL/caddy-$arch.tgz" -C "$d/bin" caddy
  fetch "https://github.com/cloudflare/cloudflared/releases/download/$CLOUDFLARED_VER/cloudflared-darwin-$arch.tgz" "$DL/cloudflared-$arch.tgz"
  tar -xzf "$DL/cloudflared-$arch.tgz" -C "$d/bin"

  cp -R dashboard/dist/. "$d/web/"
  # tr: a Windows checkout may have CRLF line endings; macOS sh needs LF.
  tr -d '\r' < packaging/macos/install.sh > "$d/install.sh"
  tr -d '\r' < packaging/macos/Caddyfile > "$d/Caddyfile"
  tr -d '\r' < packaging/macos/publicurl.sh > "$d/bin/agentmesh-publicurl"
  chmod 755 "$d/install.sh" "$d/agent/install.sh" "$d"/bin/* "$d/agent/agentmesh-agent"
  tar -C "$STAGE" -czf "$OUT/agentmesh-server_${PKGVER}_macos_${arch}.tar.gz" agentmesh-server
  echo ">> $OUT/agentmesh-server_${PKGVER}_macos_${arch}.tar.gz"
done
rm -rf "$STAGE"
