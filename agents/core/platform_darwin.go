//go:build darwin

package core

import (
	"context"
	"errors"
	"io"

	"github.com/shirou/gopsutil/v4/host"
)

// macOS support is scheduled for Phase 2 (launchd daemon, notarized .pkg,
// Keychain-backed keys). The core already builds and runs in the foreground.

func defaultStateDir() string { return "/Library/Application Support/AgentMesh" }

type osDetails struct{ name, version, build string }

func osInfo(ctx context.Context) osDetails {
	_, _, version, err := host.PlatformInformationWithContext(ctx)
	if err != nil {
		return osDetails{name: "macOS"}
	}
	return osDetails{name: "macOS", version: version}
}

func runAsService(string, func(context.Context, io.Writer) error) (bool, error) { return false, nil }

func installService(string, string) error {
	return errors.New("service installation on macOS arrives in Phase 2; run 'agentmesh-agent run' under launchd for now")
}

func uninstallService() error { return errors.New("service management on macOS arrives in Phase 2") }
