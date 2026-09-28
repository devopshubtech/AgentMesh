// Package config loads typed configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
)

// Common settings shared by every backend binary.
type Common struct {
	Env         string `env:"AGENTMESH_ENV" envDefault:"production"`
	LogLevel    string `env:"AGENTMESH_LOG_LEVEL" envDefault:"info"`
	DatabaseURL string `env:"AGENTMESH_DATABASE_URL,required"`
	NATSURL     string `env:"AGENTMESH_NATS_URL" envDefault:"nats://localhost:4222"`
}

// Dev reports whether the process runs in development mode.
func (c Common) Dev() bool { return c.Env == "dev" || c.Env == "development" }

// API configures the control-api binary.
type API struct {
	Common
	ListenAddr        string        `env:"AGENTMESH_API_LISTEN" envDefault:":8080"`
	UserTokenKey      string        `env:"AGENTMESH_USER_TOKEN_KEY,required"`      // base64 Ed25519 seed
	CommandSigningKey string        `env:"AGENTMESH_COMMAND_SIGNING_KEY,required"` // base64 Ed25519 seed
	PublicGatewayURL  string        `env:"AGENTMESH_PUBLIC_GATEWAY_URL,required"`  // shown in install snippets
	AccessTokenTTL    time.Duration `env:"AGENTMESH_ACCESS_TOKEN_TTL" envDefault:"10m"`
	CookieSecure      bool          `env:"AGENTMESH_COOKIE_SECURE" envDefault:"true"`
	TrustProxyHeaders bool          `env:"AGENTMESH_TRUST_PROXY_HEADERS" envDefault:"false"`
	AutoMigrate       bool          `env:"AGENTMESH_AUTO_MIGRATE" envDefault:"false"`
	BootstrapEmail    string        `env:"AGENTMESH_BOOTSTRAP_ADMIN_EMAIL"`
	BootstrapPassword string        `env:"AGENTMESH_BOOTSTRAP_ADMIN_PASSWORD"`
	CommandDefaultTTL time.Duration `env:"AGENTMESH_COMMAND_TTL" envDefault:"5m"`
}

// Gateway configures the agent-gateway binary.
type Gateway struct {
	Common
	ListenAddr        string        `env:"AGENTMESH_GATEWAY_LISTEN" envDefault:":8443"`
	TLSCertFile       string        `env:"AGENTMESH_GATEWAY_TLS_CERT"`
	TLSKeyFile        string        `env:"AGENTMESH_GATEWAY_TLS_KEY"`
	DeviceTokenKey    string        `env:"AGENTMESH_DEVICE_TOKEN_KEY,required"`   // base64 Ed25519 seed
	CommandPublicKey  string        `env:"AGENTMESH_COMMAND_PUBLIC_KEY,required"` // base64 Ed25519 public key
	PublicURL         string        `env:"AGENTMESH_PUBLIC_GATEWAY_URL,required"`
	GatewayID         string        `env:"AGENTMESH_GATEWAY_ID"`
	MaxConnections    int           `env:"AGENTMESH_GATEWAY_MAX_CONNECTIONS" envDefault:"20000"`
	HeartbeatInterval time.Duration `env:"AGENTMESH_HEARTBEAT_INTERVAL" envDefault:"30s"`
	DeviceTokenTTL    time.Duration `env:"AGENTMESH_DEVICE_TOKEN_TTL" envDefault:"15m"`
	TrustProxyHeaders bool          `env:"AGENTMESH_TRUST_PROXY_HEADERS" envDefault:"false"`
	MetricsListenAddr string        `env:"AGENTMESH_GATEWAY_METRICS_LISTEN" envDefault:":9090"`
}

// Worker configures the worker binary.
type Worker struct {
	Common
	MetricsListenAddr  string        `env:"AGENTMESH_WORKER_METRICS_LISTEN" envDefault:":9091"`
	OfflineAfter       time.Duration `env:"AGENTMESH_OFFLINE_AFTER" envDefault:"90s"`
	HeartbeatRetention time.Duration `env:"AGENTMESH_HEARTBEAT_RETENTION" envDefault:"720h"`
}

// Load parses environment variables into T.
func Load[T any]() (T, error) {
	var cfg T
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// MustLoad is Load that exits the process on error.
func MustLoad[T any]() T {
	cfg, err := Load[T]()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return cfg
}
