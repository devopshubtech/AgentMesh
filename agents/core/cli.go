package core

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const cliUsage = `agentmesh-agent — AgentMesh device agent %s (%s/%s)

Usage:
  agentmesh-agent install   --server URL --token TOKEN [--ca-file FILE]   enroll and install as a system service
  agentmesh-agent enroll    --server URL --token TOKEN [--ca-file FILE] [--force]
  agentmesh-agent run       run in the foreground (used by the service manager)
  agentmesh-agent status    show enrollment state
  agentmesh-agent uninstall [--purge]                                  remove the service (and state with --purge)
  agentmesh-agent version

Common flags:
  --state-dir DIR   state directory (default %s)

The token may also be given via --token-file FILE or AGENTMESH_ENROLL_TOKEN.
`

// Main is the agent entry point; it returns the process exit code.
func Main(args []string) int {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, cliUsage, Version, runtime.GOOS, runtime.GOARCH, defaultStateDir())
		return 2
	}
	var err error
	switch args[1] {
	case "run":
		err = cmdRun(args[2:])
	case "enroll":
		err = cmdEnroll(args[2:], false)
	case "install":
		err = cmdEnroll(args[2:], true)
	case "uninstall":
		err = cmdUninstall(args[2:])
	case "status":
		err = cmdStatus(args[2:])
	case "version", "--version", "-v":
		fmt.Printf("agentmesh-agent %s %s/%s\n", Version, runtime.GOOS, runtime.GOARCH)
	case "help", "--help", "-h":
		fmt.Printf(cliUsage, Version, runtime.GOOS, runtime.GOARCH, defaultStateDir())
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[1])
		fmt.Fprintf(os.Stderr, cliUsage, Version, runtime.GOOS, runtime.GOARCH, defaultStateDir())
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newLogger(w io.Writer, debug bool) *slog.Logger {
	lvl := slog.LevelInfo
	if debug {
		lvl = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	stateDir := fs.String("state-dir", defaultStateDir(), "state directory")
	debug := fs.Bool("debug", false, "debug logging")
	_ = fs.Parse(args)

	runFn := func(ctx context.Context, logOut io.Writer) error {
		log := newLogger(logOut, *debug)
		cfg, err := LoadConfig(*stateDir)
		if err != nil {
			return fmt.Errorf("load config from %s (is the agent enrolled?): %w", *stateDir, err)
		}
		a, err := NewAgent(cfg, log)
		if err != nil {
			return err
		}
		return a.Run(ctx)
	}
	if handled, err := runAsService(*stateDir, runFn); handled {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runFn(ctx, os.Stderr)
}

func readToken(flagVal, file string) (string, error) {
	switch {
	case flagVal != "":
		return flagVal, nil
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	case os.Getenv("AGENTMESH_ENROLL_TOKEN") != "":
		return os.Getenv("AGENTMESH_ENROLL_TOKEN"), nil
	}
	return "", errors.New("an enrollment token is required (--token, --token-file or AGENTMESH_ENROLL_TOKEN)")
}

func cmdEnroll(args []string, install bool) error {
	name := "enroll"
	if install {
		name = "install"
	}
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	server := fs.String("server", "", "gateway URL, e.g. https://mesh.example.com")
	token := fs.String("token", "", "enrollment token")
	tokenFile := fs.String("token-file", "", "file containing the enrollment token")
	caFile := fs.String("ca-file", "", "extra CA certificate (PEM) to trust")
	stateDir := fs.String("state-dir", defaultStateDir(), "state directory")
	insecure := fs.Bool("allow-insecure-http", false, "allow http:// server URL (development only)")
	force := fs.Bool("force", false, "replace an existing identity")
	exitNode := fs.Bool("enable-exit-node", false, "allow authorized operators to route traffic through this device")
	_ = fs.Parse(args)

	if *server == "" {
		return errors.New("--server is required")
	}
	tok, err := readToken(*token, *tokenFile)
	if err != nil {
		return err
	}
	if install && !isPrivileged() {
		return errors.New("install must be run as root / Administrator")
	}
	if err := ensureStateDir(*stateDir); err != nil {
		return err
	}
	cfg := &Config{ServerURL: *server, AllowInsecureHTTP: *insecure, stateDir: *stateDir}
	if old, err := LoadConfig(*stateDir); err == nil {
		cfg.Policy = old.Policy // keep local policy across re-enrollment
	}
	if *exitNode {
		yes := true
		cfg.Policy.AllowExitNode = &yes
	}
	if *caFile != "" {
		// Copy the CA next to the config so the service does not depend on
		// the file the operator downloaded.
		pem, err := os.ReadFile(*caFile)
		if err != nil {
			return fmt.Errorf("read CA file: %w", err)
		}
		dst := filepath.Join(*stateDir, "ca.pem")
		if err := writeFileAtomic(dst, pem, 0o644); err != nil {
			return err
		}
		cfg.CAFile = dst
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	id, status, err := Enroll(ctx, cfg, tok, *force)
	if err != nil {
		return err
	}
	fmt.Printf("Enrolled device %s (status: %s)\n", id.DeviceID, status)
	if status == "pending" {
		fmt.Println("An administrator must approve this device in the AgentMesh dashboard before it can connect.")
	}
	if !install {
		fmt.Printf("Start the agent with: %s run --state-dir %q\n", filepath.Base(os.Args[0]), *stateDir)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := installService(exe, *stateDir); err != nil {
		return fmt.Errorf("install service: %w", err)
	}
	fmt.Println("AgentMesh agent service installed and started.")
	return nil
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	purge := fs.Bool("purge", false, "also delete identity and configuration")
	stateDir := fs.String("state-dir", defaultStateDir(), "state directory")
	_ = fs.Parse(args)
	if !isPrivileged() {
		return errors.New("uninstall must be run as root / Administrator")
	}
	if err := uninstallService(); err != nil {
		return err
	}
	fmt.Println("AgentMesh agent service removed.")
	if *purge {
		if err := os.RemoveAll(*stateDir); err != nil {
			return err
		}
		fmt.Println("State directory removed:", *stateDir)
	}
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	stateDir := fs.String("state-dir", defaultStateDir(), "state directory")
	_ = fs.Parse(args)
	cfg, err := LoadConfig(*stateDir)
	if err != nil {
		fmt.Println("Not enrolled (", err, ")")
		return nil
	}
	id, err := LoadIdentity(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("Device ID:   %s\nServer:      %s\nEnrolled at: %s\nKey storage: %s\nRevoked:     %v\nVersion:     %s\n",
		id.DeviceID, cfg.ServerURL, id.EnrolledAt.Format(time.RFC3339), id.KeyProtection, id.Revoked, Version)
	fmt.Printf("Policy:      exec=%v shell=%v exit_node=%v (lan=%v) disabled_actions=%v\n", cfg.Policy.execAllowed(), cfg.Policy.shellAllowed(),
		cfg.Policy.exitNodeAllowed(), cfg.Policy.ExitNodeAllowLAN, cfg.Policy.DisabledActions)
	return nil
}
