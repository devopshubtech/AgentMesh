#!/usr/bin/env bash
# shellcheck disable=SC2016  # service command strings are expanded later, by sh under launchd
# AgentMesh server, native macOS install (no Docker) - e.g. a Mac mini.
#
#   ./install.sh                                   install or update
#   ./install.sh --public-url https://api.example.com
#   ./install.sh --tunnel-token <token>            permanent public URL (named Cloudflare tunnel)
#   ./install.sh --quick-tunnel                    free trycloudflare.com URL (changes on restart; kept current)
#   ./install.sh --rendezvous-token <github-token> publish the current URL to the app's rendezvous gist
#                [--rendezvous-gist <id>]           (needed for 6-digit codes; token needs only the "gist" scope)
#   ./install.sh --telegram-token <bot-token> --telegram-chat <chat-id> [--dashboard-url <url>]
#                                                  send the new address to Telegram whenever it changes
#   ./install.sh --agent-token am_enr_...          also run this Mac's agent as an exit node
#   ./install.sh --database-url 'postgresql://...' external Postgres (Neon: direct/unpooled URL)
#   ./install.sh --uninstall [--purge]             stop and remove (--purge also deletes settings)
#
# First install asks for the dashboard admin email and password. A private
# agentmesh-db.env next to this script can hold AM_DATABASE_URL,
# AM_RENDEZVOUS_GITHUB_TOKEN, AM_TELEGRAM_BOT_TOKEN, AM_TELEGRAM_CHAT_ID and
# AM_DASHBOARD_URL instead of passing them as options.
# Services run as launchd daemons under your user account and start at boot.
set -euo pipefail

PREFIX=${AM_PREFIX:-/usr/local/agentmesh}
LAUNCHD_DIR=${AM_LAUNCHD_DIR:-/Library/LaunchDaemons}
SERVICES="nats control-api agent-gateway worker web tunnel publicurl"
# Rendezvous gist built into the Android app (mobile/android-control/gradle.properties).
DEFAULT_RENDEZVOUS_GIST=9b876c950f541c735c8a817c96362ca9

die() { echo "error: $*" >&2; exit 1; }
say() { printf '\n>> %s\n' "$*"; }

PUBLIC_URL="" TUNNEL_TOKEN="" QUICK_TUNNEL="" AGENT_TOKEN="" DATABASE_URL="" UNINSTALL="" PURGE="" RDV_TOKEN="" RDV_GIST="" TG_TOKEN="" TG_CHAT="" DASH_URL=""
ARGS=("$@")
while [ $# -gt 0 ]; do
  case "$1" in
    --public-url)   PUBLIC_URL=${2:?--public-url needs a value}; shift 2 ;;
    --tunnel-token) TUNNEL_TOKEN=${2:?--tunnel-token needs a value}; shift 2 ;;
    --quick-tunnel) QUICK_TUNNEL=1; shift ;;
    --agent-token)  AGENT_TOKEN=${2:?--agent-token needs a value}; shift 2 ;;
    --database-url) DATABASE_URL=${2:?--database-url needs a value}; shift 2 ;;
    --rendezvous-token) RDV_TOKEN=${2:?--rendezvous-token needs a value}; shift 2 ;;
    --rendezvous-gist)  RDV_GIST=${2:?--rendezvous-gist needs a value}; shift 2 ;;
    --telegram-token)   TG_TOKEN=${2:?--telegram-token needs a value}; shift 2 ;;
    --telegram-chat)    TG_CHAT=${2:?--telegram-chat needs a value}; shift 2 ;;
    --dashboard-url)    DASH_URL=${2:?--dashboard-url needs a value}; shift 2 ;;
    --uninstall)    UNINSTALL=1; shift ;;
    --purge)        PURGE=1; shift ;;
    -h|--help)      sed -n '3,24p' "$0"; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done
case "$PUBLIC_URL" in ""|https://*) ;; *) die "--public-url must start with https://" ;; esac

# Needs root for /usr/local and /Library/LaunchDaemons; services run as the invoking user.
if [ "$(id -u)" -ne 0 ]; then
  echo "Administrator rights are needed; macOS will ask for your password."
  exec sudo "$0" "${ARGS[@]+"${ARGS[@]}"}"
fi
RUN_USER=${SUDO_USER:-}
[ -n "$RUN_USER" ] && [ "$RUN_USER" != root ] || die "run as your normal user (it asks for sudo itself), not as root"
RUN_GROUP=$(id -gn "$RUN_USER")
ROOT_GROUP=$(id -gn root) # wheel on macOS

HERE=$(cd "$(dirname "$0")" && pwd)
ETC="$PREFIX/etc" ENVF="$PREFIX/etc/agentmesh.env" LOG="$PREFIX/log"

label() { echo "com.agentmesh.$1"; }
plist() { echo "$LAUNCHD_DIR/$(label "$1").plist"; }
svc_stop() { launchctl bootout "system/$(label "$1")" 2>/dev/null || true; }
svc_start() { launchctl bootstrap system "$(plist "$1")" 2>/dev/null || launchctl kickstart -k "system/$(label "$1")"; }

# ---- Uninstall
if [ -n "$UNINSTALL" ]; then
  for s in $SERVICES; do svc_stop "$s"; rm -f "$(plist "$s")"; done
  if [ -n "$PURGE" ]; then rm -rf "${PREFIX:?}"; echo "AgentMesh server removed (settings, keys and certificates deleted)."
  else rm -rf "${PREFIX:?}/bin" "${PREFIX:?}/web"; echo "AgentMesh server removed. Settings kept in $ETC (use --purge to delete)."; fi
  echo "The database itself is not touched."
  exit 0
fi

[ "$(uname -s)" = Darwin ] || [ -n "${AM_LAUNCHD_DIR:-}" ] || die "this installer is for macOS"
[ -x "$HERE/bin/agentmesh-server" ] || die "bin/agentmesh-server not found next to install.sh; extract the whole package first"

# ---- Files
say "installing to $PREFIX"
for s in $SERVICES; do svc_stop "$s"; done
mkdir -p "$PREFIX" "$ETC/certs" "$LOG" "$PREFIX/var/caddy"
rm -rf "${PREFIX:?}/bin" "${PREFIX:?}/web"
cp -R "$HERE/bin" "$HERE/web" "$PREFIX/"
for c in control-api agent-gateway worker amctl; do ln -sf agentmesh-server "$PREFIX/bin/$c"; done
xattr -dr com.apple.quarantine "$PREFIX/bin" 2>/dev/null || true
chown -R "root:$ROOT_GROUP" "$PREFIX/bin" "$PREFIX/web"
chmod -R a+rX "$PREFIX/bin" "$PREFIX/web"
chown -R "$RUN_USER:$RUN_GROUP" "$ETC" "$LOG" "$PREFIX/var"
chmod 700 "$ETC"
as_user() { sudo -u "$RUN_USER" "$@"; }

# ---- Settings (agentmesh.env, sh syntax, single-quoted values; created once and kept)
q() { printf "'%s'" "$1"; }
set_env() { # key value
  local tmp; tmp=$(mktemp)
  grep -v "^$1=" "$ENVF" > "$tmp" 2>/dev/null || true
  printf '%s=%s\n' "$1" "$(q "$2")" >> "$tmp"
  cat "$tmp" > "$ENVF" && rm -f "$tmp"
}
get_env() { grep "^$1=" "$ENVF" 2>/dev/null | tail -n1 | cut -d= -f2- | sed -e "s/^'//" -e "s/'\$//"; }
secret() { openssl rand -base64 24 | tr -d '+/=\n'; }

# Private settings file next to this script (never published): command-line
# options win; otherwise these keys are read from it.
PRIVATE=""
for f in "$HERE/agentmesh-db.env" "$HERE/../agentmesh-db.env"; do [ -f "$f" ] && PRIVATE=$f && break; done
priv() { grep "^$1=" "$PRIVATE" | tail -n1 | cut -d= -f2- | sed -e "s/^['\"]//" -e "s/['\"]\$//"; }
if [ -n "$PRIVATE" ]; then
  echo "using private settings from $PRIVATE"
  DATABASE_URL=${DATABASE_URL:-$(priv AM_DATABASE_URL)}
  RDV_TOKEN=${RDV_TOKEN:-$(priv AM_RENDEZVOUS_GITHUB_TOKEN)}
  TG_TOKEN=${TG_TOKEN:-$(priv AM_TELEGRAM_BOT_TOKEN)}
  TG_CHAT=${TG_CHAT:-$(priv AM_TELEGRAM_CHAT_ID)}
  DASH_URL=${DASH_URL:-$(priv AM_DASHBOARD_URL)}
fi

NEW_ADMIN_EMAIL="" NEW_ADMIN_PW="" ADMIN_PW_GENERATED=""
if [ ! -f "$ENVF" ]; then
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
      read -r -p "  Postgres URL (e.g. Neon direct/unpooled connection string): " DATABASE_URL
    fi
  fi
  [ -n "$DATABASE_URL" ] || die "a database is required: pass --database-url, put agentmesh-db.env next to install.sh, or answer the prompt"
  [ -n "$NEW_ADMIN_PW" ] || { NEW_ADMIN_PW=$(secret); ADMIN_PW_GENERATED=1; }
  install -m 600 -o "$RUN_USER" -g "$RUN_GROUP" /dev/null "$ENVF"
  {
    echo "# AgentMesh server settings (read by the launchd services). Keep private."
    echo "AGENTMESH_ENV='production'"
    echo "AGENTMESH_LOG_LEVEL='info'"
    echo "AGENTMESH_NATS_URL='nats://127.0.0.1:14222'"
    echo "AGENTMESH_API_LISTEN='127.0.0.1:18080'"
    echo "AGENTMESH_GATEWAY_LISTEN=':18443'"
    echo "AGENTMESH_GATEWAY_ID='gw-1'"
    echo "AGENTMESH_GATEWAY_TLS_CERT=$(q "$ETC/certs/gateway.pem")"
    echo "AGENTMESH_GATEWAY_TLS_KEY=$(q "$ETC/certs/gateway-key.pem")"
    echo "AGENTMESH_GATEWAY_METRICS_LISTEN='127.0.0.1:19090'"
    echo "AGENTMESH_WORKER_METRICS_LISTEN='127.0.0.1:19091'"
    echo "AGENTMESH_PUBLIC_GATEWAY_URL='https://localhost:13443'"
    echo "AGENTMESH_AUTO_MIGRATE='true'"
    echo "AGENTMESH_COOKIE_SECURE='true'"
    echo "AGENTMESH_TRUST_PROXY_HEADERS='true'"
  } > "$ENVF"
  set_env AGENTMESH_BOOTSTRAP_ADMIN_EMAIL "$NEW_ADMIN_EMAIL"
  set_env AGENTMESH_BOOTSTRAP_ADMIN_PASSWORD "$NEW_ADMIN_PW"
  say "created $ENVF"
fi
case "$DATABASE_URL" in ""|postgres://*|postgresql://*) ;; *) die "database URL must start with postgres:// or postgresql://" ;; esac
case "$DATABASE_URL" in *-pooler.*) die "use the direct (unpooled) connection string, not the -pooler one" ;; esac
[ -n "$DATABASE_URL" ] && set_env AGENTMESH_DATABASE_URL "$DATABASE_URL"
[ -n "$PUBLIC_URL" ] && set_env AGENTMESH_PUBLIC_GATEWAY_URL "$PUBLIC_URL"
[ -n "$TUNNEL_TOKEN" ] && set_env AM_TUNNEL_TOKEN "$TUNNEL_TOKEN"
[ -n "$QUICK_TUNNEL" ] && set_env AM_QUICK_TUNNEL 1
if [ -n "$RDV_TOKEN" ]; then
  RDV_GIST=${RDV_GIST:-$DEFAULT_RENDEZVOUS_GIST}
  set_env AM_RENDEZVOUS_GITHUB_TOKEN "$RDV_TOKEN"
  set_env AM_RENDEZVOUS_GIST_ID "$RDV_GIST"
  # The API always returns the latest content (the raw gist URL is cached ~5 min).
  set_env AGENTMESH_RENDEZVOUS_URL "https://api.github.com/gists/$RDV_GIST"
  rm -f "$PREFIX/var/rendezvous-published"
fi
[ -n "$TG_TOKEN" ] && set_env AM_TELEGRAM_BOT_TOKEN "$TG_TOKEN"
[ -n "$TG_CHAT" ] && set_env AM_TELEGRAM_CHAT_ID "$TG_CHAT"
[ -n "$DASH_URL" ] && set_env AM_DASHBOARD_URL "$DASH_URL"
{ [ -n "$TG_TOKEN" ] || [ -n "$TG_CHAT" ]; } && rm -f "$PREFIX/var/telegram-notified"
if ! grep -q '^AGENTMESH_USER_TOKEN_KEY=' "$ENVF"; then
  as_user "$PREFIX/bin/amctl" keygen | grep '^AGENTMESH_' >> "$ENVF"
  say "generated signing keys"
fi
chown "$RUN_USER:$RUN_GROUP" "$ENVF"; chmod 600 "$ENVF"

# ---- TLS certificate (AgentMesh CA; re-issued when the LAN IP changes, CA kept)
LAN_IP=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || true)
if [ ! -f "$ETC/certs/gateway.pem" ] || { [ -n "$LAN_IP" ] && ! grep -qx "$LAN_IP" "$ETC/certs/.hosts" 2>/dev/null; }; then
  HOSTS="localhost,127.0.0.1,::1,agent-gateway,$(hostname -s).local${LAN_IP:+,$LAN_IP}"
  as_user "$PREFIX/bin/amctl" dev-certs -out "$ETC/certs" -hosts "$HOSTS" >/dev/null
  echo "$HOSTS" | tr ',' '\n' > "$ETC/certs/.hosts"
  chown "$RUN_USER:$RUN_GROUP" "$ETC/certs/.hosts"
  say "issued TLS certificate for: $HOSTS"
fi

# ---- Web front config
sed "s|@PREFIX@|$PREFIX|g" "$HERE/Caddyfile" > "$ETC/Caddyfile"
chown "$RUN_USER:$RUN_GROUP" "$ETC/Caddyfile"

# ---- launchd services
TUNNEL_CMD=""
if [ -n "$(get_env AM_TUNNEL_TOKEN)" ]; then
  TUNNEL_CMD='export TUNNEL_TOKEN="$AM_TUNNEL_TOKEN"; exec "$P/bin/cloudflared" tunnel --no-autoupdate --protocol http2 run'
elif [ -n "$(get_env AM_QUICK_TUNNEL)" ]; then
  TUNNEL_CMD='exec "$P/bin/cloudflared" tunnel --no-autoupdate --protocol http2 --url https://localhost:13443 --no-tls-verify'
fi
cmd_for() {
  case "$1" in
    nats)          echo 'exec "$P/bin/nats-server" -a 127.0.0.1 -p 14222 -m 18222' ;;
    control-api)   echo 'exec "$P/bin/control-api"' ;;
    agent-gateway) echo 'exec "$P/bin/agent-gateway"' ;;
    worker)        echo 'exec "$P/bin/worker"' ;;
    web)           echo 'exec "$P/bin/caddy" run --config "$P/etc/Caddyfile" --adapter caddyfile' ;;
    tunnel)        echo "$TUNNEL_CMD" ;;
    publicurl)     [ -n "$(get_env AM_QUICK_TUNNEL)" ] && [ -z "$(get_env AM_TUNNEL_TOKEN)" ] && echo 'exec "$P/bin/agentmesh-publicurl"' ;;
  esac
}
xml() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }
write_plist() { # service command
  # Everything runs as the installing user, except the public-URL watcher, which
  # restarts other services (root).
  local who=""
  [ "$1" = publicurl ] || who="  <key>UserName</key><string>$RUN_USER</string>
  <key>GroupName</key><string>$RUN_GROUP</string>"
  cat > "$(plist "$1")" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$(label "$1")</string>
$who
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string><string>-c</string>
    <string>$(printf 'P=%s; HOME="$P/var"; export HOME; set -a; . "$P/etc/agentmesh.env"; set +a; %s' "$PREFIX" "$2" | xml)</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>$LOG/$1.log</string>
  <key>StandardErrorPath</key><string>$LOG/$1.log</string>
</dict>
</plist>
EOF
  chown "root:$ROOT_GROUP" "$(plist "$1")"; chmod 644 "$(plist "$1")"
}
say "starting services"
for s in $SERVICES; do
  c=$(cmd_for "$s")
  if [ -z "$c" ]; then rm -f "$(plist "$s")"; continue; fi
  [ "$s" = tunnel ] && : > "$LOG/tunnel.log" && chown "$RUN_USER:$RUN_GROUP" "$LOG/tunnel.log"
  write_plist "$s" "$c"
  # The worker starts once the API has created the tables (see below).
  [ "$s" = worker ] || svc_start "$s"
done

say "waiting for the API (first start creates the database tables)"
ok=""
for _ in $(seq 1 90); do
  curl -fsS -o /dev/null http://127.0.0.1:18080/readyz 2>/dev/null && ok=1 && break
  sleep 2
done
[ -n "$ok" ] || die "control-api is not ready; see $LOG/control-api.log"
svc_start worker
curl -fsSk -o /dev/null https://127.0.0.1:13443/ || echo "warning: web front not answering yet; see $LOG/web.log"

if [ -f "$(plist publicurl)" ]; then
  say "waiting for the public trycloudflare.com address"
  for _ in $(seq 1 60); do
    case "$(get_env AGENTMESH_PUBLIC_GATEWAY_URL)" in *.trycloudflare.com) break ;; esac
    sleep 3
  done
fi

# ---- Optional: this Mac's own agent (exit node)
if [ -n "$AGENT_TOKEN" ]; then
  say "installing this Mac's agent"
  sh "$HERE/agent/install.sh" --server https://127.0.0.1:18443 --token "$AGENT_TOKEN" \
    --ca-file "$ETC/certs/ca.pem" --enable-exit-node
fi

DB=$(get_env AGENTMESH_DATABASE_URL)
echo
echo "=============================================================="
echo " AgentMesh server is running (native, no Docker)"
echo "   Dashboard (this Mac):  https://localhost:13443"
[ -n "$LAN_IP" ] && echo "   Dashboard (your LAN):  https://$LAN_IP:13443"
echo "   Agents connect to:     https://${LAN_IP:-<this-mac>}:18443  (LAN)  or the public URL"
echo "   Public URL:            $(get_env AGENTMESH_PUBLIC_GATEWAY_URL)"
echo "   Database:              $(echo "$DB" | sed -E 's|^[a-z]+://[^@]*@([^/:?]+).*|\1|')"
if [ -n "$ADMIN_PW_GENERATED" ]; then
  echo "   Admin login:           $NEW_ADMIN_EMAIL / $NEW_ADMIN_PW   (generated; save it)"
elif [ -n "$NEW_ADMIN_PW" ]; then
  echo "   Admin login:           $NEW_ADMIN_EMAIL / the password you just set"
fi
echo "   (The admin is created only if the database has no users yet.)"
echo
echo "   Settings: $ENVF     Logs: $LOG/"
echo "   Restart one service:  sudo launchctl kickstart -k system/com.agentmesh.control-api"
echo "   Keep the Mac awake:   System Settings → Energy → Prevent automatic sleeping"
echo "=============================================================="
