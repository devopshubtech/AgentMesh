#!/bin/sh
# AgentMesh agent installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/devopshubtech/AgentMesh/main/scripts/install/install.sh \
#     | sudo sh -s -- --server https://<gateway>:18443 --token am_enr_... [--ca-file ca.pem] [--enable-exit-node]
#
# If an agentmesh-agent binary sits next to this script (extracted release
# archive), it is used; otherwise the matching archive is downloaded from the
# latest GitHub release and verified against SHA256SUMS before installing.
set -eu

REPO_URL=${AGENTMESH_RELEASE_URL:-https://github.com/devopshubtech/AgentMesh/releases/latest/download}

die() { echo "error: $*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || die "run as root (sudo)"
[ $# -gt 0 ] || die "usage: install.sh --server URL --token TOKEN [--ca-file FILE] [--enable-exit-node]"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in linux|darwin) ;; *) die "unsupported OS: $os" ;; esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7l|armv7*) arch=armv7 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

here=$(cd "$(dirname "$0")" 2>/dev/null && pwd || echo "")
if [ -n "$here" ] && [ -x "$here/agentmesh-agent" ]; then
  bin="$here/agentmesh-agent"
else
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  fetch() { if command -v curl >/dev/null; then curl -fsSL "$1" -o "$2"; else wget -qO "$2" "$1"; fi; }
  echo ">> finding latest release for ${os}/${arch}"
  fetch "$REPO_URL/SHA256SUMS" "$tmp/SHA256SUMS"
  asset=$(grep -oE "agentmesh-agent_[^ ]+_${os}_${arch}\.tar\.gz" "$tmp/SHA256SUMS" | head -n1)
  [ -n "$asset" ] || die "no release archive for ${os}/${arch}"
  echo ">> downloading $asset"
  fetch "$REPO_URL/$asset" "$tmp/$asset"
  want=$(grep " $asset\$" "$tmp/SHA256SUMS" | awk '{print $1}')
  if command -v sha256sum >/dev/null; then got=$(sha256sum "$tmp/$asset" | awk '{print $1}');
  else got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}'); fi
  [ "$want" = "$got" ] || die "checksum mismatch for $asset"
  echo ">> checksum verified"
  tar -xzf "$tmp/$asset" -C "$tmp"
  bin="$tmp/agentmesh-agent"
  [ "$os" = darwin ] && xattr -d com.apple.quarantine "$bin" 2>/dev/null || true
fi

"$bin" version
exec "$bin" install "$@"
