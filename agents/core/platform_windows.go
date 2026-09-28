//go:build windows

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName        = "AgentMesh"
	serviceDisplayName = "AgentMesh Agent"
	dpapiScheme        = "dpapi-machine"
)

var dpapiEntropy = []byte("agentmesh-device-key-v1")

func defaultStateDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "AgentMesh")
}

func installDir() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "AgentMesh")
}

// restrictDir limits the state directory to SYSTEM and Administrators.
func restrictDir(dir string) error {
	if !isPrivileged() {
		return nil // developer foreground runs; ACLs need elevation
	}
	out, err := exec.Command("icacls", dir, "/inheritance:r",
		"/grant:r", "*S-1-5-18:(OI)(CI)F", // SYSTEM
		"/grant:r", "*S-1-5-32-544:(OI)(CI)F", // Administrators
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func blobBytes(d *windows.DataBlob) []byte {
	out := make([]byte, d.Size)
	copy(out, unsafe.Slice(d.Data, d.Size))
	windows.LocalFree(windows.Handle(unsafe.Pointer(d.Data)))
	return out
}

// protectSecret encrypts with DPAPI in machine scope (the service runs as
// SYSTEM while enrollment runs as an administrator). Combined with the
// SYSTEM/Administrators-only ACL on the state directory, only privileged
// principals on this machine can recover the key.
func protectSecret(b []byte) ([]byte, string, error) {
	var out windows.DataBlob
	desc, _ := windows.UTF16PtrFromString("AgentMesh device key")
	if err := windows.CryptProtectData(blob(b), desc, blob(dpapiEntropy), 0, nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN|windows.CRYPTPROTECT_LOCAL_MACHINE, &out); err != nil {
		return nil, "", err
	}
	return blobBytes(&out), dpapiScheme, nil
}

func unprotectSecret(b []byte, scheme string) ([]byte, error) {
	if scheme != dpapiScheme {
		return nil, errors.New("unsupported key protection " + scheme)
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(b), nil, blob(dpapiEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return blobBytes(&out), nil
}

type osDetails struct{ name, version, build string }

func osInfo(_ context.Context) osDetails {
	d := osDetails{name: "Windows"}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return d
	}
	defer k.Close()
	product, _, _ := k.GetStringValue("ProductName")
	display, _, _ := k.GetStringValue("DisplayVersion")
	build, _, _ := k.GetStringValue("CurrentBuild")
	ubr, _, _ := k.GetIntegerValue("UBR")
	if n, err := strconv.Atoi(build); err == nil && n >= 22000 {
		// ProductName still says "Windows 10" on Windows 11.
		product = strings.Replace(product, "Windows 10", "Windows 11", 1)
	}
	if product != "" {
		d.name = product
	}
	d.version = display
	if build != "" {
		d.build = fmt.Sprintf("%s.%d", build, ubr)
	}
	return d
}

func shellArgv(script string) []string {
	return []string{"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script}
}

// prepareCommand hides console windows and kills the whole process tree on
// timeout or cancellation.
func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

func isPrivileged() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// ---------------------------------------------------------------- service

type winService struct {
	stateDir string
	run      func(context.Context, io.Writer) error
}

func (s *winService) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	logw, err := newRotatingLog(filepath.Join(s.stateDir, "agent.log"), 10<<20, 3)
	if err != nil {
		return true, 1
	}
	defer logw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.run(ctx, logw) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
				}
				return false, 0
			}
		case err := <-done:
			if err != nil {
				fmt.Fprintln(logw, "agent exited:", err)
				return true, 1
			}
			return false, 0
		}
	}
}

func runAsService(stateDir string, run func(context.Context, io.Writer) error) (bool, error) {
	isSvc, err := svc.IsWindowsService()
	if err != nil || !isSvc {
		return false, err
	}
	return true, svc.Run(serviceName, &winService{stateDir: stateDir, run: run})
}

func copyExe(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func installService(exe, stateDir string) error {
	dir := installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(dir, "agentmesh-agent.exe")
	if !strings.EqualFold(filepath.Clean(exe), filepath.Clean(target)) {
		_ = stopService()
		if err := copyExe(exe, target); err != nil {
			return fmt.Errorf("install binary: %w", err)
		}
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		if err := removeService(m); err != nil {
			return err
		}
	}
	s, err = m.CreateService(serviceName, target, mgr.Config{
		DisplayName:      serviceDisplayName,
		Description:      "Connects this device to the AgentMesh control plane.",
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
	}, "run", "--state-dir", stateDir)
	if err != nil {
		return err
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 2 * time.Minute},
	}, 86400)
	return s.Start()
}

func stopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return nil
	}
	defer s.Close()
	st, err := s.Control(svc.Stop)
	if err != nil {
		return nil
	}
	deadline := time.Now().Add(20 * time.Second)
	for st.State != svc.Stopped && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			break
		}
	}
	return nil
}

func removeService(m *mgr.Mgr) error {
	_ = stopService()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return nil
	}
	defer s.Close()
	return s.Delete()
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if err := removeService(m); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(installDir(), "agentmesh-agent.exe"))
	return nil
}
