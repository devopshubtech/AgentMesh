// Command agent-gateway terminates agent enrollment, authentication and
// persistent WebSocket connections.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/gateway"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/config"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/keys"
	"github.com/enfec/agentmesh/backend/internal/platform/logging"
)

func main() {
	cfg := config.MustLoad[config.Gateway]()
	log := logging.New("agent-gateway", cfg.LogLevel)
	if err := run(cfg, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Gateway, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	devKey, err := keys.ParsePrivate(cfg.DeviceTokenKey)
	if err != nil {
		return errors.New("AGENTMESH_DEVICE_TOKEN_KEY: " + err.Error())
	}
	cmdPub, err := keys.ParsePublic(cfg.CommandPublicKey)
	if err != nil {
		return errors.New("AGENTMESH_COMMAND_PUBLIC_KEY: " + err.Error())
	}
	if cfg.GatewayID == "" {
		host, _ := os.Hostname()
		cfg.GatewayID = host + "-" + uuid.NewString()[:8]
	}
	tlsEnabled := cfg.TLSCertFile != "" && cfg.TLSKeyFile != ""
	if !tlsEnabled {
		if !cfg.Dev() {
			return errors.New("TLS is required outside dev mode: set AGENTMESH_GATEWAY_TLS_CERT and AGENTMESH_GATEWAY_TLS_KEY (or terminate TLS at the load balancer and set AGENTMESH_ENV=dev only for local testing)")
		}
		log.Warn("gateway serving PLAIN HTTP (dev mode only)")
	}
	if strings.HasPrefix(cfg.PublicURL, "http://") && !cfg.Dev() {
		log.Warn("AGENTMESH_PUBLIC_GATEWAY_URL is not https")
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	b, err := bus.Connect(cfg.NATSURL, "agent-gateway/"+cfg.GatewayID, log)
	if err != nil {
		return err
	}
	defer b.Close()

	gw := gateway.New(gateway.Options{
		GatewayID: cfg.GatewayID, MaxConnections: cfg.MaxConnections,
		HeartbeatInterval: cfg.HeartbeatInterval, TrustProxy: cfg.TrustProxyHeaders,
	}, pool, b, log, auth.NewTokenIssuer(devKey, auth.AudienceGateway, cfg.DeviceTokenTTL), cmdPub)
	bgCtx, bgCancel := context.WithCancel(context.Background())
	bgDone := make(chan struct{})
	go func() { gw.Run(bgCtx); close(bgDone) }()

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: gw.Handler(),
		// No Read/WriteTimeout: they would kill long-lived WebSockets. Non-WS
		// handlers bound their bodies via MaxBytesReader.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	metricsSrv := &http.Server{Addr: cfg.MetricsListenAddr, Handler: promhttp.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics server", "err", err)
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		log.Info("agent-gateway listening", "addr", cfg.ListenAddr, "tls", tlsEnabled, "gateway_id", cfg.GatewayID)
		var err error
		if tlsEnabled {
			err = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		bgCancel()
		return err
	}
	log.Info("draining agent connections")
	gw.Drain(30 * time.Second)
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	_ = metricsSrv.Shutdown(shCtx)
	bgCancel()
	<-bgDone
	return nil
}
