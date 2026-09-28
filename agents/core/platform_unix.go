//go:build linux || darwin || freebsd

package core

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func restrictDir(dir string) error { return os.Chmod(dir, 0o700) }

// On Unix the key file itself (mode 0600, root-owned) is the protection.
// TPM-backed keys are planned for Phase 4.
func protectSecret(b []byte) ([]byte, string, error) { return b, "file-0600", nil }

func unprotectSecret(b []byte, scheme string) ([]byte, error) {
	if scheme != "file-0600" {
		return nil, errors.New("unsupported key protection " + scheme)
	}
	return b, nil
}

func shellArgv(script string) []string { return []string{"/bin/sh", "-c", script} }

// prepareCommand runs the child in its own process group so that timeouts
// and cancellation kill the whole tree, not just the direct child.
func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func isPrivileged() bool { return os.Geteuid() == 0 }
