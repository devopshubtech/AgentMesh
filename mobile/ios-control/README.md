# AgentMesh for iPhone

The iPhone version of the Android control app (`mobile/android-control`): connect with a
QR code (camera **or a picture from Photos**) or a 6-digit pairing code, and the iPhone's
internet goes out through an AgentMesh exit-node device.

- **App** (`App/`, SwiftUI): saved servers, scan / pick / pair, status, "Check my IP".
- **Packet tunnel extension** (`Tunnel/`): the VPN. It runs the same Go engine as Android
  (`mobile/tunnel`, bound with gomobile) on the iPhone's utun interface, swaps in a new relay
  link when the connection drops, and looks up a changed server address via the rendezvous gist.
- **Shared** (`Shared/`): connect links, the connect/pairing API and the saved servers
  (list in the App Group, connect keys in the Keychain).

## Requirements

- A Mac with **Xcode 15+**, **Go 1.26** (`brew install go`) and Homebrew.
- A **paid Apple Developer account** ($99/year). The VPN uses Apple's Network Extension
  capability, which free accounts cannot use.

## Build

```bash
cd mobile/ios-control
DEVELOPMENT_TEAM=<your Team ID> ./build.sh
open AgentMesh.xcodeproj      # pick your iPhone, press Run
```

`build.sh` builds the Go engine (`Frameworks/Tunnel.xcframework`) and generates the Xcode
project from `project.yml` with XcodeGen. Your Team ID is on developer.apple.com →
Membership details.

The default bundle ID is `io.agentmesh.control` (tunnel: `io.agentmesh.control.tunnel`,
App Group: `group.io.agentmesh.control`). If Apple says it is taken, use your own:
`AGENTMESH_BUNDLE_ID=com.yourcompany.agentmesh ./build.sh`.

## Share with testers (TestFlight)

```bash
DEVELOPMENT_TEAM=<Team ID> AGENTMESH_VERSION=0.6.4 ./build.sh --archive   # -> dist/AgentMesh-0.6.4.ipa
```

1. In App Store Connect → Apps → **+** create the app with the same bundle ID.
2. Upload the `.ipa` with Xcode → Window → Organizer (or the Transporter app).
3. TestFlight → add testers or create a public link. Testers install the **TestFlight** app,
   open the link, then install AgentMesh.

Every upload needs a higher build number; `build.sh` uses the date and time automatically.

## How it differs from Android

| | Android | iPhone |
|---|---|---|
| App file | `.apk`, installed directly | `.ipa`, installed through TestFlight / App Store |
| VPN | `VpnService` | `NEPacketTunnelProvider` extension |
| IPv6 | blocked (no IPv6 address) | routed into the tunnel and dropped, so it cannot leak; apps fall back to IPv4 |
| Local network | excluded on Android 13+ | excluded (10/8, 172.16/12, 192.168/16, 169.254/16, 100.64/10) |

## Checked without a Mac

The Go engine compiles for iOS and its utun packet handling has unit tests
(`go test ./mobile/tunnel`). The Swift sources pass the Swift compiler's syntax check, and the
shared code (links, pairing codes, status messages, and the connect / pairing API against a live
server) is tested on Linux. The app and extension themselves are first compiled by Xcode, so the
first build on the Mac may need small fixes.
