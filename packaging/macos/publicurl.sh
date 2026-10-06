#!/bin/sh
# Keeps the server's public address current while it uses a free Cloudflare
# quick tunnel, whose trycloudflare.com address changes on every restart:
#   - points AGENTMESH_PUBLIC_GATEWAY_URL at the live address (QR codes,
#     pairing codes and install commands use it) and restarts the API/gateway;
#   - publishes it to the rendezvous gist the Android app looks up, so typed
#     6-digit codes and older QR codes find this server;
#   - restarts the tunnel when Cloudflare has dropped it.
# Runs as root under launchd (com.agentmesh.publicurl); installed by install.sh.
P=${AM_PREFIX:-/usr/local/agentmesh}
ENVF="$P/etc/agentmesh.env" TLOG="$P/log/tunnel.log" STATE="$P/var/rendezvous-published"

get_env() { grep "^$1=" "$ENVF" 2>/dev/null | tail -n1 | cut -d= -f2- | sed -e "s/^'//" -e "s/'\$//"; }
set_env() { # rewrite in place so the file keeps its owner and mode
  tmp=$(mktemp); grep -v "^$1=" "$ENVF" > "$tmp"; printf "%s='%s'\n" "$1" "$2" >> "$tmp"
  cat "$tmp" > "$ENVF"; rm -f "$tmp"
}
log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*"; }
reachable() { curl -fsS -o /dev/null -m 10 "$1/agentmesh-ca.pem"; }

publish() { # url → rendezvous gist (same JSON as scripts/start-agentmesh.ps1)
  token=$(get_env AM_RENDEZVOUS_GITHUB_TOKEN) gist=$(get_env AM_RENDEZVOUS_GIST_ID)
  [ -n "$token" ] && [ -n "$gist" ] || return 0
  [ "$(cat "$STATE" 2>/dev/null)" = "$1" ] && return 0
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  body=$(printf '{"files":{"agentmesh-endpoint.json":{"content":"{\\"url\\":\\"%s\\",\\"updated\\":\\"%s\\"}"}}}' "$1" "$now")
  if curl -fsS -o /dev/null -m 20 -X PATCH -H "Authorization: Bearer $token" \
       -H "Accept: application/vnd.github+json" -d "$body" "https://api.github.com/gists/$gist"; then
    echo "$1" > "$STATE"; log "rendezvous gist updated: $1"
  else
    log "could not update the rendezvous gist (check AM_RENDEZVOUS_GITHUB_TOKEN)"
  fi
}

misses=0
while :; do
  url=$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$TLOG" 2>/dev/null | tail -n1)
  if [ -n "$url" ] && reachable "$url"; then
    misses=0
    if [ "$(get_env AGENTMESH_PUBLIC_GATEWAY_URL)" != "$url" ]; then
      set_env AGENTMESH_PUBLIC_GATEWAY_URL "$url"
      log "public address is now $url; restarting control-api and agent-gateway"
      launchctl kickstart -k system/com.agentmesh.control-api
      launchctl kickstart -k system/com.agentmesh.agent-gateway
    fi
    publish "$url"
  elif [ -n "$url" ]; then
    misses=$((misses + 1))
    if [ "$misses" -ge 9 ]; then # ~3 minutes unreachable: Cloudflare dropped the quick tunnel
      log "$url is unreachable; restarting the tunnel"
      : > "$TLOG"; launchctl kickstart -k system/com.agentmesh.tunnel; misses=0
    fi
  fi
  sleep 20
done
