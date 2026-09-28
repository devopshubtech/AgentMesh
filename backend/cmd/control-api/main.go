// Command control-api serves the REST API used by the dashboard and mobile app.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/enfec/agentmesh/backend/internal/api"
	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/commands"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/enrollment"
	"github.com/enfec/agentmesh/backend/internal/events"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/config"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/keys"
	"github.com/enfec/agentmesh/backend/internal/platform/logging"
	"github.com/enfec/agentmesh/backend/internal/sessions"
	"github.com/enfec/agentmesh/backend/internal/users"
	"github.com/enfec/agentmesh/backend/migrations"
)

func main() {
	cfg := config.MustLoad[config.API]()
	log := logging.New("control-api", cfg.LogLevel)
	if err := run(cfg, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.API, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	userKey, err := keys.ParsePrivate(cfg.UserTokenKey)
	if err != nil {
		return errors.New("AGENTMESH_USER_TOKEN_KEY: " + err.Error())
	}
	cmdKey, err := keys.ParsePrivate(cfg.CommandSigningKey)
	if err != nil {
		return errors.New("AGENTMESH_COMMAND_SIGNING_KEY: " + err.Error())
	}
	if !cfg.CookieSecure && !cfg.Dev() {
		log.Warn("AGENTMESH_COOKIE_SECURE=false outside dev mode; refresh cookies may travel over plain HTTP")
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cfg.AutoMigrate {
		if err := migrations.Up(ctx, pool, log); err != nil {
			return err
		}
	}
	if err := users.EnsureBootstrapAdmin(ctx, pool, cfg.BootstrapEmail, cfg.BootstrapPassword, log); err != nil {
		return err
	}

	b, err := bus.Connect(cfg.NATSURL, "control-api", log)
	if err != nil {
		return err
	}
	defer b.Close()
	hub, err := events.NewHub(b, log)
	if err != nil {
		return err
	}
	defer hub.Close()

	devSvc := devices.NewService(pool, b)
	srv := api.New(api.Deps{
		Pool: pool, Bus: b, Log: log,
		Sessions:       auth.NewSessionService(pool, auth.NewTokenIssuer(userKey, auth.AudienceAPI, cfg.AccessTokenTTL), log),
		Users:          users.NewService(pool),
		Devices:        devSvc,
		Commands:       commands.NewService(pool, b, devSvc, cmdKey, cfg.CommandDefaultTTL),
		Enrollment:     enrollment.NewService(pool, b),
		RemoteSessions: sessions.NewService(pool, b, devSvc, cmdKey),
		Hub:            hub,
		GatewayURL:     cfg.PublicGatewayURL,
		CookieSecure:   cfg.CookieSecure,
		TrustProxy:     cfg.TrustProxyHeaders,
	})

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("control-api listening", "addr", cfg.ListenAddr, "version", api.Version)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}
	log.Info("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shCtx)
}
