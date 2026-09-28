#!/usr/bin/env sh
# Generates local development secrets for the Docker Compose stack
# (see dev-setup.ps1 for details). Existing files are kept unless --force.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
DOCKER="$ROOT/infrastructure/docker"
FORCE=${1:-}

secret() { head -c 24 /dev/urandom | base64 | tr -d '+/=' ; }

if [ "$FORCE" = "--force" ] || [ ! -f "$DOCKER/.env" ]; then
  ADMIN_PW=$(secret)
  sed -e "s|^AM_PG_PASSWORD=.*|AM_PG_PASSWORD=$(secret)|" \
      -e "s|^AM_ADMIN_PASSWORD=.*|AM_ADMIN_PASSWORD=$ADMIN_PW|" \
      "$DOCKER/.env.example" > "$DOCKER/.env"
  echo "Created $DOCKER/.env"
  echo "  Dashboard login: admin@agentmesh.local / $ADMIN_PW"
fi

cd "$ROOT"
if [ "$FORCE" = "--force" ] || [ ! -f "$DOCKER/.env.keys" ]; then
  go run ./backend/cmd/amctl keygen > "$DOCKER/.env.keys"
  echo "Created $DOCKER/.env.keys"
fi
if [ "$FORCE" = "--force" ] || [ ! -f "$DOCKER/certs/gateway.pem" ]; then
  go run ./backend/cmd/amctl dev-certs -out "$DOCKER/certs"
fi
echo
echo "Next: docker compose -f infrastructure/docker/docker-compose.yml up -d --build"
