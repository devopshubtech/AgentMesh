#!/usr/bin/env bash
# Builds all agent release artifacts into dist/release:
#   Windows  .zip  (amd64, arm64)          - agentmesh-agent.exe + install.ps1
#   macOS    .tar.gz (amd64, arm64)        - agentmesh-agent + install.sh
#   Linux    .tar.gz (amd64, arm64, armv7) - agentmesh-agent + install.sh
#   Linux    .deb / .rpm (amd64, arm64, armv7)
#   SHA256SUMS
# Run inside golang:1.26 (see README "Release"):
#   docker run --rm -v "$PWD:/src" -w /src golang:1.26 bash scripts/build-release.sh v0.2.0
set -euo pipefail
VERSION=${1:?usage: build-release.sh vX.Y.Z}
PKGVER=${VERSION#v}
OUT=dist/release
rm -rf "$OUT" && mkdir -p "$OUT" dist/stage
export CGO_ENABLED=0 GOTOOLCHAIN=local
LDFLAGS="-s -w -X github.com/enfec/agentmesh/agents/core.Version=${PKGVER}"

command -v nfpm >/dev/null || go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.43.0
command -v zip >/dev/null || (apt-get update -qq && apt-get install -y -qq zip >/dev/null)

build() { # goos goarch goarm pkg outname
  local goos=$1 goarch=$2 goarm=$3 pkg=$4 out=$5
  echo ">> $out"
  GOOS=$goos GOARCH=$goarch GOARM=$goarm go build -trimpath -ldflags "$LDFLAGS" -o "$out" "$pkg"
}

# ---- Windows
for arch in amd64 arm64; do
  d=dist/stage/windows-$arch && rm -rf "$d" && mkdir -p "$d"
  build windows $arch "" ./agents/windows "$d/agentmesh-agent.exe"
  cp scripts/install/install.ps1 "$d/"
  (cd "$d" && zip -q -9 "../../../$OUT/agentmesh-agent_${PKGVER}_windows_${arch}.zip" agentmesh-agent.exe install.ps1)
done

# ---- macOS + Linux tarballs
for t in darwin/amd64/ darwin/arm64/ linux/amd64/ linux/arm64/ linux/arm/7; do
  IFS=/ read -r goos goarch goarm <<<"$t"
  label=$goarch; [ "$goarch" = arm ] && label=armv7
  d=dist/stage/$goos-$label && rm -rf "$d" && mkdir -p "$d"
  pkg=./agents/linux; [ "$goos" = darwin ] && pkg=./agents/macos
  build "$goos" "$goarch" "$goarm" "$pkg" "$d/agentmesh-agent"
  cp scripts/install/install.sh "$d/"
  tar -C "$d" -czf "$OUT/agentmesh-agent_${PKGVER}_${goos}_${label}.tar.gz" agentmesh-agent install.sh
  if [ "$goos" = linux ]; then
    debarch=$goarch; rpmarch=$goarch
    case $goarch in amd64) rpmarch=x86_64;; arm64) rpmarch=aarch64;; arm) debarch=armhf; rpmarch=armv7hl;; esac
    for fmt in deb rpm; do
      arch=$debarch; [ $fmt = rpm ] && arch=$rpmarch
      mkdir -p dist/stage/pkgbin && cp "$d/agentmesh-agent" dist/stage/pkgbin/agentmesh-agent
      ARCH=$arch VERSION=$PKGVER \
        nfpm package --config packaging/linux/nfpm.yaml --packager $fmt --target "$OUT/" >/dev/null
    done
  fi
done

# ---- Android control app (built separately with the android-build image)
if [ -f dist/agentmesh-control.apk ]; then
  cp dist/agentmesh-control.apk "$OUT/agentmesh-control.apk"
fi

cp scripts/install/install.sh scripts/install/install.ps1 "$OUT/"
(cd "$OUT" && sha256sum -- * | grep -v SHA256SUMS > SHA256SUMS)
rm -rf dist/stage
ls -lh "$OUT"
