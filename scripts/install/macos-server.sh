#!/usr/bin/env bash
# AgentMesh server for macOS (e.g. a Mac mini): runs the whole backend
# (postgres, nats, control-api, agent-gateway, worker, dashboard) in Docker.
#
#   ./install-server.sh                                  install or update
#   ./install-server.sh --public-url https://api.example.com
#   ./install-server.sh --tunnel-token <cloudflare-tunnel-token>   permanent URL (named tunnel)
#   ./install-server.sh --quick-tunnel                   temporary trycloudflare.com URL
#   ./install-server.sh --agent-token am_enr_...         also install this Mac's agent as an exit node
#
# Safe to re-run: existing secrets, keys, certificates and data are kept.
# Needs Docker Desktop (or OrbStack); no Go or Node toolchain.
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }
say() { printf '\n>> %s\n' "$*"; }

PUBLIC_URL="" TUNNEL_TOKEN="" QUICK_TUNNEL="" AGENT_TOKEN=""
while [ $# -gt 0 ]; do
  case "$1" in
    --public-url)   PUBLIC_URL=${2:?--public-url needs a value}; shift 2 ;;
    --tunnel-token) TUNNEL_TOKEN=${2:?--tunnel-token needs a value}; shift 2 ;;
    --quick-tunnel) QUICK_TUNNEL=1; shift ;;
    --agent-token)  AGENT_TOKEN=${2:?--agent-token needs a value}; shift 2 ;;
    -h|--help)      sed -n '2,13p' "$0"; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done
case "$PUBLIC_URL" in ""|https://*) ;; *) die "--public-url must start with https://" ;; esac

# Works both from the release archive (script at the top) and from a git checkout.
HERE=$(cd "$(dirname "$0")" && pwd)
if [ -d "$HERE/infrastructure/docker" ]; then ROOT=$HERE; else ROOT=$(cd "$HERE/../.." && pwd); fi
DOCKER="$ROOT/infrastructure/docker"
[ -f "$DOCKER/docker-compose.yml" ] || die "cannot find infrastructure/docker/docker-compose.yml under $ROOT"
COMPOSE=(docker compose -f "$DOCKER/docker-compose.yml")

[ "$(uname -s)" = Darwin ] || echo "note: written for macOS; continuing on $(uname -s)"
[ "$(id -u)" -ne 0 ] || die "run as your normal user, not with sudo (Docker Desktop runs per user)"

# ---- Docker
command -v docker >/dev/null || die "Docker is not installed. Install Docker Desktop for Mac:
  https://www.docker.com/products/docker-desktop/   (or: brew install --cask docker)
then open it once, and re-run this script."
if ! docker info >/dev/null 2>&1; then
  say "starting Docker Desktop"
  open -a Docker 2>/dev/null || true
  for _ in $(seq 1 90); do docker info >/dev/null 2>&1 && break; sleep 2; done
  docker info >/dev/null 2>&1 || die "Docker is not running. Open Docker Desktop and re-run."
fi
docker compose version >/dev/null 2>&1 || die "docker compose v2 is required (bundled with Docker Desktop)"

# ---- .env (secrets are generated once and kept)
secret() { openssl rand -base64 24 | tr -d '+/=\n'; }
set_env() { # key value: replace or append in .env
  local tmp; tmp=$(mktemp)
  grep -v "^$1=" "$DOCKER/.env" > "$tmp" || true
  printf '%s=%s\n' "$1" "$2" >> "$tmp"
  cat "$tmp" > "$DOCKER/.env" && rm -f "$tmp"
}
NEW_ADMIN_PW=""
if [ ! -f "$DOCKER/.env" ]; then
  NEW_ADMIN_PW=$(secret)
  sed -e "s|^AM_PG_PASSWORD=.*|AM_PG_PASSWORD=$(secret)|" \
      -e "s|^AM_ADMIN_PASSWORD=.*|AM_ADMIN_PASSWORD=$NEW_ADMIN_PW|" \
      "$DOCKER/.env.example" > "$DOCKER/.env"
  chmod 600 "$DOCKER/.env"
  say "created $DOCKER/.env"
fi
[ -n "$PUBLIC_URL" ] && set_env AM_PUBLIC_GATEWAY_URL "$PUBLIC_URL"
[ -n "$TUNNEL_TOKEN" ] && set_env AM_TUNNEL_TOKEN "$TUNNEL_TOKEN"

# ---- Images (built here for this Mac's CPU)
say "building images (first run takes a few minutes)"
"${COMPOSE[@]}" build control-api dashboard

# ---- Signing keys and TLS certificates (via amctl inside the backend image)
if [ ! -s "$DOCKER/.env.keys" ]; then
  docker run --rm agentmesh/backend:dev amctl keygen > "$DOCKER/.env.keys"
  chmod 600 "$DOCKER/.env.keys"
  say "created $DOCKER/.env.keys"
fi
LAN_IP=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || true)
if [ ! -f "$DOCKER/certs/gateway.pem" ] || { [ -n "$LAN_IP" ] && ! grep -qx "$LAN_IP" "$DOCKER/certs/.hosts" 2>/dev/null; }; then
  mkdir -p "$DOCKER/certs"
  HOSTS="localhost,127.0.0.1,::1,host.docker.internal,agent-gateway,dashboard,$(hostname -s).local${LAN_IP:+,$LAN_IP}"
  docker run --rm --user "$(id -u):$(id -g)" -v "$DOCKER/certs:/certs" \
    agentmesh/backend:dev amctl dev-certs -out /certs -hosts "$HOSTS"
  echo "$HOSTS" | tr ',' '\n' > "$DOCKER/certs/.hosts"
  say "issued TLS certificate for: $HOSTS"
fi

# ---- Start
PROFILES=()
if grep -q '^AM_TUNNEL_TOKEN=.' "$DOCKER/.env"; then PROFILES+=(--profile named)
elif [ -n "$QUICK_TUNNEL" ]; then PROFILES+=(--profile public); fi
say "starting AgentMesh"
"${COMPOSE[@]}" "${PROFILES[@]+"${PROFILES[@]}"}" up -d

say "waiting for the API to become healthy"
for _ in $(seq 1 90); do
  [ "$(docker inspect -f '{{.State.Health.Status}}' agentmesh-control-api-1 2>/dev/null)" = healthy ] && break
  sleep 2
done
[ "$(docker inspect -f '{{.State.Health.Status}}' agentmesh-control-api-1 2>/dev/null)" = healthy ] \
  || die "control-api did not become healthy; check: docker compose -f $DOCKER/docker-compose.yml logs control-api"

QUICK_URL=""
if [ -n "$QUICK_TUNNEL" ] && ! grep -q '^AM_TUNNEL_TOKEN=.' "$DOCKER/.env"; then
  for _ in $(seq 1 30); do
    QUICK_URL=$("${COMPOSE[@]}" logs tunnel 2>/dev/null | grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' | tail -n1 || true)
    [ -n "$QUICK_URL" ] && break; sleep 2
  done
  if [ -n "$QUICK_URL" ] && [ -z "$PUBLIC_URL" ]; then
    set_env AM_PUBLIC_GATEWAY_URL "$QUICK_URL"
    "${COMPOSE[@]}" up -d control-api >/dev/null
  fi
fi

# ---- Optional: this Mac's own agent (exit node), via the regular agent installer
if [ -n "$AGENT_TOKEN" ]; then
  say "installing this Mac's agent (sudo password needed)"
  sudo sh "$ROOT/scripts/install/install.sh" --server https://localhost:18443 \
    --token "$AGENT_TOKEN" --ca-file "$DOCKER/certs/ca.pem" --enable-exit-node
fi

# ---- Summary
PUB=$(grep '^AM_PUBLIC_GATEWAY_URL=' "$DOCKER/.env" | tail -n1 | cut -d= -f2-)
echo
echo "=============================================================="
echo " AgentMesh server is running"
echo "   Dashboard (this Mac):  http://localhost:13000"
[ -n "$LAN_IP" ] && echo "   Dashboard (your LAN):  https://$LAN_IP:13443   (self-signed CA)"
echo "   Public URL:            ${PUB:-not set}"
if [ -n "$NEW_ADMIN_PW" ]; then
  echo "   Admin login:           admin@agentmesh.local / $NEW_ADMIN_PW"
else
  echo "   Admin login:           see AM_ADMIN_EMAIL / AM_ADMIN_PASSWORD in $DOCKER/.env"
fi
echo
echo " Keep it running:"
echo "   - Docker Desktop → Settings → General → 'Start Docker Desktop when you sign in'"
echo "   - System Settings → Energy → 'Prevent automatic sleeping' (or: sudo pmset -a sleep 0)"
echo "   Containers restart by themselves after a reboot once Docker is up."
echo "=============================================================="
