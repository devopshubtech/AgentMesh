// Package core is the shared AgentMesh desktop agent (Linux, Windows, macOS).
// Platform differences live in *_linux.go, *_windows.go and *_darwin.go.
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Version is overridden at build time with -ldflags "-X .../core.Version=...".
var Version = "0.1.0-dev"

const (
	configFile   = "agent.json"
	identityFile = "identity.json"
	seenFile     = "seen.json"
)

// Policy is the device-local override that the control plane cannot change.
// It is stored in the root/Administrator-owned config file.
type Policy struct {
	// AllowExec permits arbitrary commands (kind=exec). Default true.
	AllowExec *bool `json:"allow_exec,omitempty"`
	// AllowShell permits shell scripts (exec with shell=true). Default true.
	AllowShell *bool `json:"allow_shell,omitempty"`
	// DisabledActions lists predefined actions to refuse.
	DisabledActions []string `json:"disabled_actions,omitempty"`
	// MaxConcurrentCommands bounds parallel executions. Default 8.
	MaxConcurrentCommands int `json:"max_concurrent_commands,omitempty"`
}

func (p Policy) execAllowed() bool  { return p.AllowExec == nil || *p.AllowExec }
func (p Policy) shellAllowed() bool { return p.execAllowed() && (p.AllowShell == nil || *p.AllowShell) }

func (p Policy) actionAllowed(name string) bool {
	for _, a := range p.DisabledActions {
		if a == name {
			return false
		}
	}
	return true
}

func (p Policy) maxConcurrent() int {
	if p.MaxConcurrentCommands > 0 {
		return p.MaxConcurrentCommands
	}
	return 8
}

// Config is persisted in <state-dir>/agent.json.
type Config struct {
	ServerURL string `json:"server_url"`
	// CAFile is an extra PEM CA bundle to trust (private deployments / dev).
	CAFile string `json:"ca_file,omitempty"`
	// AllowInsecureHTTP permits http:// server URLs. Development only.
	AllowInsecureHTTP bool   `json:"allow_insecure_http,omitempty"`
	Policy            Policy `json:"policy"`

	stateDir string
}

// NewConfig returns a config for serverURL whose state lives in stateDir.
func NewConfig(stateDir, serverURL string) *Config {
	return &Config{ServerURL: serverURL, stateDir: stateDir}
}

// StateDir returns the directory holding config and identity.
func (c *Config) StateDir() string { return c.stateDir }

func (c *Config) path(name string) string { return filepath.Join(c.stateDir, name) }

// Validate checks the server URL.
func (c *Config) Validate() error {
	u, err := url.Parse(c.ServerURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid server URL %q", c.ServerURL)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !c.AllowInsecureHTTP {
			return errors.New("server URL must use https (use --allow-insecure-http only for local development)")
		}
	default:
		return fmt.Errorf("unsupported server URL scheme %q", u.Scheme)
	}
	c.ServerURL = strings.TrimRight(c.ServerURL, "/")
	return nil
}

// LoadConfig reads <stateDir>/agent.json.
func LoadConfig(stateDir string) (*Config, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, configFile))
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configFile, err)
	}
	c.stateDir = stateDir
	return &c, c.Validate()
}

// Save writes the config atomically.
func (c *Config) Save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(c.path(configFile), b, 0o600)
}

// writeFileAtomic writes data to a temp file and renames it into place.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ensureStateDir creates the state directory with restrictive permissions.
func ensureStateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return restrictDir(dir)
}
