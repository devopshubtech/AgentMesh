#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  if [ -f /var/lib/agentmesh/identity.json ]; then
    # Upgrade of an enrolled device: restart on the new version.
    systemctl try-restart agentmesh-agent.service || true
  fi
fi
if [ ! -f /var/lib/agentmesh/identity.json ]; then
  echo "AgentMesh agent installed. Enroll this device with:"
  echo "  sudo agentmesh-agent install --server https://<gateway-host>:18443 --token <am_enr_...> [--ca-file ca.pem] [--enable-exit-node]"
fi
