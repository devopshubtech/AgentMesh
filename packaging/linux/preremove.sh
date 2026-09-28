#!/bin/sh
# Stop the service on removal (not on upgrade). Identity in /var/lib/agentmesh is kept.
case "$1" in
  remove|purge|0)
    if command -v systemctl >/dev/null 2>&1; then
      systemctl disable --now agentmesh-agent.service >/dev/null 2>&1 || true
      rm -f /etc/systemd/system/agentmesh-agent.service
      systemctl daemon-reload || true
    fi
    ;;
esac
exit 0
