//go:build darwin

package core

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/shirou/gopsutil/v4/host"
)

const (
	launchdLabel    = "io.agentmesh.agent"
	launchdPlist    = "/Library/LaunchDaemons/io.agentmesh.agent.plist"
	installedBinary = "/usr/local/bin/agentmesh-agent"
	darwinLogFile   = "/var/log/agentmesh-agent.log"
)

func defaultStateDir() string { return "/Library/Application Support/AgentMesh" }

type osDetails struct{ name, version, build string }

func osInfo(ctx context.Context) osDetails {
	_, _, version, err := host.PlatformInformationWithContext(ctx)
	if err != nil {
		return osDetails{name: "macOS"}
	}
	build, _ := exec.CommandContext(ctx, "sw_vers", "-buildVersion").Output()
	return osDetails{name: "macOS", version: version, build: strings.TrimSpace(string(build))}
}

// launchd runs "agentmesh-agent run" as a normal foreground process.
func runAsService(string, func(context.Context, io.Writer) error) (bool, error) { return false, nil }

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installService(exe, stateDir string) error {
	if err := os.MkdirAll("/usr/local/bin", 0o755); err != nil {
		return err
	}
	if exe != installedBinary {
		if err := copyFile(exe, installedBinary, 0o755); err != nil {
			return fmt.Errorf("install binary: %w", err)
		}
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + launchdLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + installedBinary + `</string>
    <string>run</string>
    <string>--state-dir</string>
    <string>` + xmlEscape(stateDir) + `</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>` + darwinLogFile + `</string>
  <key>StandardErrorPath</key><string>` + darwinLogFile + `</string>
</dict>
</plist>
`
	if err := os.WriteFile(launchdPlist, []byte(plist), 0o644); err != nil {
		return err
	}
	_ = launchctl("bootout", "system/"+launchdLabel) // replace a previous install
	if err := launchctl("bootstrap", "system", launchdPlist); err != nil {
		return err
	}
	_ = launchctl("enable", "system/"+launchdLabel)
	return launchctl("kickstart", "-k", "system/"+launchdLabel)
}

func uninstallService() error {
	_ = launchctl("bootout", "system/"+launchdLabel)
	for _, p := range []string{launchdPlist, installedBinary} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
