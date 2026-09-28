//go:build linux

package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	systemdUnitPath = "/etc/systemd/system/agentmesh-agent.service"
	installedBinary = "/usr/local/bin/agentmesh-agent"
)

func defaultStateDir() string { return "/var/lib/agentmesh" }

type osDetails struct{ name, version, build string }

func osInfo(_ context.Context) osDetails {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return osDetails{name: "Linux"}
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok {
			kv[k] = strings.Trim(v, `"'`)
		}
	}
	d := osDetails{name: kv["NAME"], version: kv["VERSION_ID"], build: kv["VERSION"]}
	if d.name == "" {
		d.name = "Linux"
	}
	return d
}

// runAsService: systemd runs "agentmesh-agent run" as a normal foreground
// process, so there is nothing special to do.
func runAsService(string, func(context.Context, io.Writer) error) (bool, error) { return false, nil }

const unitTemplate = `[Unit]
Description=AgentMesh device agent
Documentation=https://github.com/enfec/agentmesh
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run --state-dir %s
Restart=always
RestartSec=5
UMask=0077
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installService(exe, stateDir string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemd not found; run '%s run --state-dir %s' under your init system", exe, stateDir)
	}
	bin := installedBinary
	abs, _ := filepath.EvalSymlinks(exe)
	switch {
	case strings.HasPrefix(abs, "/usr/bin/"):
		bin = abs // installed by the .deb/.rpm package: use it in place
	case abs != installedBinary:
		if err := copyFile(exe, installedBinary, 0o755); err != nil {
			return fmt.Errorf("install binary: %w", err)
		}
	}
	unit := fmt.Sprintf(unitTemplate, bin, stateDir)
	if err := os.WriteFile(systemdUnitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := systemctl("enable", "agentmesh-agent.service"); err != nil {
		return err
	}
	return systemctl("restart", "agentmesh-agent.service")
}

func uninstallService() error {
	_ = systemctl("disable", "--now", "agentmesh-agent.service")
	if err := os.Remove(systemdUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = systemctl("daemon-reload")
	if err := os.Remove(installedBinary); err != nil && !os.IsNotExist(err) { // package-managed /usr/bin copy is left to apt/dnf
		return err
	}
	return nil
}
