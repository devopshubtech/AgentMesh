#!/usr/bin/env bash
# AgentMesh server for macOS (e.g. a Mac mini): runs the whole backend
# (postgres, nats, control-api, agent-gateway, worker, dashboard) in Docker.
#
#   ./install-server.sh                                  install or update
#   ./install-server.sh --public-url https://api.example.com
#   ./install-server.sh --tunnel-token <cloudflare-tunnel-token>   permanent URL (named tunnel)
#   ./install-server.sh --quick-tunnel                   temporary trycloudflare.com URL
#   ./install-server.sh --agent-token am_enr_...         also install this Mac's agent as an exit node
#   ./install-server.sh --database-url 'postgresql://...'  use an external Postgres (e.g. Neon/Vercel, unpooled URL)
#
# First install asks for the admin email and password. A private agentmesh-db.env
# (AM_DATABASE_URL=...) next to this script selects an external database.
#
# Safe to re-run: existing secrets, keys, certificates and data are kept.
# Needs Docker Desktop (or OrbStack); no Go or Node toolchain.
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }
say() { printf '\n>> %s\n' "$*"; }

PUBLIC_URL="" TUNNEL_TOKEN="" QUICK_TUNNEL="" AGENT_TOKEN="" DATABASE_URL=""
while [ $# -gt 0 ]; do
  case "$1" in
    --public-url)   PUBLIC_URL=${2:?--public-url needs a value}; shift 2 ;;
    --tunnel-token) TUNNEL_TOKEN=${2:?--tunnel-token needs a value}; shift 2 ;;
    --quick-tunnel) QUICK_TUNNEL=1; shift ;;
    --agent-token)  AGENT_TOKEN=${2:?--agent-token needs a value}; shift 2 ;;
    --database-url) DATABASE_URL=${2:?--database-url needs a value}; shift 2 ;;
    -h|--help)      sed -n '2,17p' "$0"; exit 0 ;;
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
set_env() { # key value: replace or append in .env; single quotes = no $ interpolation by compose
  local tmp; tmp=$(mktemp)
  grep -v "^$1=" "$DOCKER/.env" > "$tmp" || true
  printf "%s='%s'\n" "$1" "$2" >> "$tmp"
  cat "$tmp" > "$DOCKER/.env" && rm -f "$tmp"
}
get_env() { grep "^$1=" "$DOCKER/.env" 2>/dev/null | tail -n1 | cut -d= -f2- | sed -e "s/^'//" -e "s/'\$//"; }

# Database connection kept out of the public package: a private agentmesh-db.env
# (AM_DATABASE_URL=...) placed next to this script is picked up automatically.
if [ -z "$DATABASE_URL" ]; then
  for f in "$HERE/agentmesh-db.env" "$ROOT/agentmesh-db.env"; do
    if [ -f "$f" ]; then
      DATABASE_URL=$(grep '^AM_DATABASE_URL=' "$f" | tail -n1 | cut -d= -f2- | sed -e "s/^['\"]//" -e "s/['\"]\$//")
      [ -n "$DATABASE_URL" ] && echo "using the database from $f" && break
    fi
  done
fi

NEW_ADMIN_EMAIL="" NEW_ADMIN_PW="" ADMIN_PW_GENERATED=""
if [ ! -f "$DOCKER/.env" ]; then
  NEW_ADMIN_EMAIL=admin@agentmesh.local
  if [ -t 0 ]; then
    say "first install: set the dashboard admin account"
    read -r -p "  Admin email (username) [admin@agentmesh.local]: " ans
    [ -n "$ans" ] && NEW_ADMIN_EMAIL=$ans
    case "$NEW_ADMIN_EMAIL" in *@*.*) ;; *) die "admin email must look like name@example.com" ;; esac
    while :; do
      read -r -s -p "  Admin password (12+ characters, Enter = generate one): " NEW_ADMIN_PW; echo
      [ -z "$NEW_ADMIN_PW" ] && break
      case "$NEW_ADMIN_PW" in *"'"*) echo "  the password cannot contain a single quote ('), try again"; continue ;; esac
      [ ${#NEW_ADMIN_PW} -ge 12 ] || { echo "  too short (${#NEW_ADMIN_PW} characters), try again"; continue; }
      read -r -s -p "  Repeat the password: " again; echo
      [ "$again" = "$NEW_ADMIN_PW" ] && break
      echo "  passwords do not match, try again"
    done
    if [ -z "$DATABASE_URL" ]; then
      read -r -p "  External Postgres URL, e.g. Neon (Enter = database on this Mac): " DATABASE_URL
    fi
  fi
  [ -n "$NEW_ADMIN_PW" ] || { NEW_ADMIN_PW=$(secret); ADMIN_PW_GENERATED=1; }
  cp "$DOCKER/.env.example" "$DOCKER/.env"
  chmod 600 "$DOCKER/.env"
  set_env AM_PG_PASSWORD "$(secret)"
  set_env AM_ADMIN_EMAIL "$NEW_ADMIN_EMAIL"
  set_env AM_ADMIN_PASSWORD "$NEW_ADMIN_PW"
  say "created $DOCKER/.env"
fi
case "$DATABASE_URL" in ""|postgres://*|postgresql://*) ;; *) die "database URL must start with postgres:// or postgresql://" ;; esac
case "$DATABASE_URL" in *-pooler.*) die "use the direct (unpooled) connection string, not the -pooler one" ;; esac
[ -n "$PUBLIC_URL" ] && set_env AM_PUBLIC_GATEWAY_URL "$PUBLIC_URL"
[ -n "$TUNNEL_TOKEN" ] && set_env AM_TUNNEL_TOKEN "$TUNNEL_TOKEN"
[ -n "$DATABASE_URL" ] && set_env AM_DATABASE_URL "$DATABASE_URL"

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
if [ -n "$(get_env AM_TUNNEL_TOKEN)" ]; then PROFILES+=(--profile named)
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
if [ -n "$QUICK_TUNNEL" ] && ! [ -n "$(get_env AM_TUNNEL_TOKEN)" ]; then
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
PUB=$(get_env AM_PUBLIC_GATEWAY_URL)
DB=$(get_env AM_DATABASE_URL)
echo
echo "=============================================================="
echo " AgentMesh server is running"
echo "   Dashboard (this Mac):  http://localhost:13000"
[ -n "$LAN_IP" ] && echo "   Dashboard (your LAN):  https://$LAN_IP:13443   (self-signed CA)"
echo "   Public URL:            ${PUB:-not set}"
if [ -n "$DB" ]; then
  echo "   Database:              external ($(echo "$DB" | sed -E 's|^[a-z]+://[^@]*@([^/:?]+).*|\1|'))"
else
  echo "   Database:              on this Mac (Docker volume pgdata)"
fi
if [ -n "$ADMIN_PW_GENERATED" ]; then
  echo "   Admin login:           $NEW_ADMIN_EMAIL / $NEW_ADMIN_PW   (generated; save it)"
elif [ -n "$NEW_ADMIN_PW" ]; then
  echo "   Admin login:           $NEW_ADMIN_EMAIL / the password you just set"
else
  echo "   Admin login:           AM_ADMIN_EMAIL / AM_ADMIN_PASSWORD in $DOCKER/.env"
fi
[ -n "$DB" ] && echo "   (The admin is created only if the database has no users yet; an existing login stays.)"
echo
echo " Keep it running:"
echo "   - Docker Desktop → Settings → General → 'Start Docker Desktop when you sign in'"
echo "   - System Settings → Energy → 'Prevent automatic sleeping' (or: sudo pmset -a sleep 0)"
echo "   Containers restart by themselves after a reboot once Docker is up."
echo "=============================================================="
