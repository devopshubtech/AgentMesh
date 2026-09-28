#!/bin/sh
# Enrolls on first start (token from AGENTMESH_ENROLL_TOKEN), then runs the agent.
# AGENTMESH_ENABLE_EXIT_NODE=1 opts this device in as an exit node.
set -eu

STATE_DIR=/var/lib/agentmesh

if [ ! -f "$STATE_DIR/identity.json" ]; then
  if [ -z "${AGENTMESH_ENROLL_TOKEN:-}" ]; then
    echo "not enrolled and AGENTMESH_ENROLL_TOKEN is empty; create a token in the dashboard" >&2
    exit 1
  fi
  set -- --server "${AGENTMESH_SERVER:?AGENTMESH_SERVER is required}" --state-dir "$STATE_DIR"
  if [ -n "${AGENTMESH_CA_FILE:-}" ]; then
    set -- "$@" --ca-file "$AGENTMESH_CA_FILE"
  fi
  if [ "${AGENTMESH_ENABLE_EXIT_NODE:-}" = "1" ]; then
    set -- "$@" --enable-exit-node
  fi
  agentmesh-agent enroll "$@"
fi

exec agentmesh-agent run --state-dir "$STATE_DIR"
