#!/usr/bin/env bash
# Builds the AgentMesh iPhone app on a Mac (Xcode, Go and Homebrew installed):
#   1. gomobile-binds the exit-node engine (mobile/tunnel) into Frameworks/Tunnel.xcframework
#   2. generates AgentMesh.xcodeproj from project.yml (XcodeGen)
#   3. with --archive: archives and exports an .ipa for TestFlight / App Store
#
#   DEVELOPMENT_TEAM=ABCDE12345 ./build.sh              then open AgentMesh.xcodeproj
#   DEVELOPMENT_TEAM=ABCDE12345 ./build.sh --archive    -> dist/AgentMesh-<version>.ipa
#
# Optional: AGENTMESH_BUNDLE_ID (default io.agentmesh.control), AGENTMESH_VERSION,
# AGENTMESH_BUILD (default: date-based, always growing).
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)

: "${DEVELOPMENT_TEAM:?set DEVELOPMENT_TEAM to your Apple Developer Team ID (developer.apple.com → Membership)}"
export DEVELOPMENT_TEAM
export AGENTMESH_BUNDLE_ID=${AGENTMESH_BUNDLE_ID:-io.agentmesh.control}
export AGENTMESH_VERSION=${AGENTMESH_VERSION:-0.6.4}
export AGENTMESH_BUILD=${AGENTMESH_BUILD:-$(date +%Y%m%d%H%M)}

[ "$(uname -s)" = Darwin ] || { echo "error: iPhone apps can only be built on a Mac with Xcode" >&2; exit 1; }
command -v xcodebuild >/dev/null || { echo "error: install Xcode from the App Store, then run: sudo xcode-select -s /Applications/Xcode.app" >&2; exit 1; }
command -v go >/dev/null || { echo "error: install Go: brew install go" >&2; exit 1; }
command -v xcodegen >/dev/null || { echo ">> installing XcodeGen"; brew install xcodegen; }
export PATH="$PATH:$(go env GOPATH)/bin"
if ! command -v gomobile >/dev/null; then
  echo ">> installing gomobile"
  (cd "$ROOT" && go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind)
  gomobile init
fi

echo ">> gomobile bind (iOS device + simulator)"
rm -rf Frameworks/Tunnel.xcframework
(cd "$ROOT" && gomobile bind -target=ios,iossimulator -iosversion=16.0 -ldflags="-s -w" -trimpath \
  -o mobile/ios-control/Frameworks/Tunnel.xcframework ./mobile/tunnel)

echo ">> xcodegen (bundle $AGENTMESH_BUNDLE_ID, version $AGENTMESH_VERSION build $AGENTMESH_BUILD)"
xcodegen generate --quiet

if [ "${1:-}" = --archive ]; then
  echo ">> archive"
  rm -rf build/AgentMesh.xcarchive
  xcodebuild -project AgentMesh.xcodeproj -scheme AgentMesh -configuration Release -destination 'generic/platform=iOS' \
    -archivePath build/AgentMesh.xcarchive -allowProvisioningUpdates archive | tail -n 5
  cat > build/ExportOptions.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>method</key><string>app-store-connect</string>
  <key>teamID</key><string>$DEVELOPMENT_TEAM</string>
  <key>signingStyle</key><string>automatic</string>
</dict></plist>
EOF
  xcodebuild -exportArchive -archivePath build/AgentMesh.xcarchive -exportOptionsPlist build/ExportOptions.plist \
    -exportPath build/export -allowProvisioningUpdates | tail -n 3
  mkdir -p "$ROOT/dist"
  cp build/export/AgentMesh.ipa "$ROOT/dist/AgentMesh-$AGENTMESH_VERSION.ipa"
  echo ">> dist/AgentMesh-$AGENTMESH_VERSION.ipa  (upload with Xcode → Organizer, or the Transporter app)"
else
  echo ">> done: open AgentMesh.xcodeproj, pick your iPhone, press Run"
fi
