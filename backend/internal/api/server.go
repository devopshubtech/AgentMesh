// Package api is the control-api HTTP transport: routing, authentication
// middleware and handlers. Business rules live in the domain packages.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/commands"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/enrollment"
	"github.com/enfec/agentmesh/backend/internal/events"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/platform/ratelimit"
	"github.com/enfec/agentmesh/backend/internal/sessions"
	"github.com/enfec/agentmesh/backend/internal/users"
)

// Version is set at build time.
var Version = "0.1.0-dev"

// Deps are the services the API needs.
type Deps struct {
	Pool       *pgxpool.Pool
	Bus        *bus.Bus
	Log        *slog.Logger
	Sessions   *auth.SessionService
	Users      *users.Service
	Devices    *devices.Service
	Commands   *commands.Service
	Enrollment *enrollment.Service
	// RemoteSessions manages relayed exit-node sessions (Sessions is user auth).
	RemoteSessions *sessions.Service
	Hub            *events.Hub
	GatewayURL     string
	// RendezvousURL always returns the server's current public address (optional).
	RendezvousURL string
	CookieSecure  bool
	TrustProxy    bool
}

// Server holds handler state.
type Server struct {
	Deps
	loginIP    *ratelimit.Keyed
	loginEmail *ratelimit.Keyed
	refreshIP  *ratelimit.Keyed
	commandsRL *ratelimit.Keyed
	connectRL  *ratelimit.Keyed
}

// New builds the server.
func New(d Deps) *Server {
	return &Server{
		Deps:       d,
		loginIP:    ratelimit.New(20, 10),
		loginEmail: ratelimit.New(10, 5),
		refreshIP:  ratelimit.New(60, 30),
		commandsRL: ratelimit.New(60, 20),
		connectRL:  ratelimit.New(30, 10),
	}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware, httpx.ClientIPMiddleware(s.TrustProxy), httpx.AccessLog(s.Log), httpx.Recover, httpx.SecurityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", s.ready)
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/v1", func(r chi.Router) {
		r.Post("/auth/login", httpx.H(s.login))
		r.Post("/auth/refresh", httpx.H(s.refresh))
		r.Post("/auth/logout", httpx.H(s.logout))
		// Public: the connect key (QR code / link) is the credential.
		r.Post("/connect/session", httpx.H(s.openConnectSession))

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Get("/me", httpx.H(s.me))
			r.Get("/config", httpx.H(s.config))
			r.Get("/events", httpx.H(s.events))

			r.Get("/devices", httpx.H(s.listDevices))
			r.Get("/devices/summary", httpx.H(s.deviceSummary))
			r.Get("/devices/{id}", httpx.H(s.getDevice))
			r.Patch("/devices/{id}", httpx.H(s.patchDevice))
			r.Post("/devices/{id}/{op:approve|disable|enable|revoke}", httpx.H(s.transitionDevice))
			r.Get("/devices/{id}/activity", httpx.H(s.deviceActivity))
			r.Get("/devices/{id}/commands", httpx.H(s.listCommands))
			r.Post("/devices/{id}/commands", httpx.H(s.createCommand))

			r.Post("/devices/{id}/exit-sessions", httpx.H(s.createExitSession))
			r.Get("/exit-sessions", httpx.H(s.listExitSessions))
			r.Delete("/exit-sessions/{id}", httpx.H(s.terminateExitSession))
			r.Get("/devices/{id}/connect-keys", httpx.H(s.listConnectKeys))
			r.Post("/devices/{id}/connect-keys", httpx.H(s.createConnectKey))
			r.Delete("/connect-keys/{id}", httpx.H(s.revokeConnectKey))

			r.Get("/commands/{id}", httpx.H(s.getCommand))
			r.Post("/commands/{id}/cancel", httpx.H(s.cancelCommand))

			r.Get("/enrollment-tokens", httpx.H(s.listTokens))
			r.Post("/enrollment-tokens", httpx.H(s.createToken))
			r.Delete("/enrollment-tokens/{id}", httpx.H(s.revokeToken))

			r.Get("/audit", httpx.H(s.listAudit))
			r.Get("/audit/verify", httpx.H(s.verifyAudit))

			r.Get("/roles", httpx.H(s.listRoles))
			r.Get("/users", httpx.H(s.listUsers))
			r.Post("/users", httpx.H(s.createUser))
			r.Patch("/users/{id}", httpx.H(s.patchUser))
			r.Delete("/users/{id}", httpx.H(s.deleteUser))
		})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, httpx.NotFound("route")) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.WithCode(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed"))
	})
	return r
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Pool.Ping(ctx); err != nil || !s.Bus.Connected() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// authenticate resolves the bearer token into a live Principal.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		raw, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || raw == "" {
			httpx.WriteError(w, r, httpx.Unauthorized("missing bearer token"))
			return
		}
		v, err := s.Sessions.Tokens().Verify(raw)
		if errors.Is(err, auth.ErrTokenExpired) {
			httpx.WriteError(w, r, httpx.WithCode(http.StatusUnauthorized, "token_expired", "access token expired"))
			return
		}
		if err != nil || v.SessionID == nil {
			httpx.WriteError(w, r, httpx.Unauthorized("invalid access token"))
			return
		}
		p, err := s.Sessions.LoadPrincipal(r.Context(), v.Subject, *v.SessionID)
		if errors.Is(err, auth.ErrSessionInvalid) {
			httpx.WriteError(w, r, httpx.Unauthorized("session is no longer valid"))
			return
		}
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		ctx := auth.WithPrincipal(r.Context(), p)
		ctx = context.WithValue(ctx, tokenExpiryKey{}, v.ExpiresAt)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type tokenExpiryKey struct{}

// require checks a permission and audits denials.
func (s *Server) require(r *http.Request, perm string) (*auth.Principal, error) {
	p, err := auth.Require(r.Context(), perm)
	if err != nil && p == nil {
		if pr := auth.FromContext(r.Context()); pr != nil {
			s.auditDenied(r, pr, perm)
		}
	}
	return p, err
}

// requireAny passes when the principal holds at least one permission.
func (s *Server) requireAny(r *http.Request, perms ...string) (*auth.Principal, error) {
	p := auth.FromContext(r.Context())
	if p == nil {
		return nil, httpx.Unauthorized("authentication required")
	}
	for _, perm := range perms {
		if p.Can(perm) {
			return p, nil
		}
	}
	s.auditDenied(r, p, strings.Join(perms, "|"))
	return nil, httpx.Forbidden("missing permission " + strings.Join(perms, " or "))
}

func (s *Server) auditDenied(r *http.Request, p *auth.Principal, perm string) {
	err := audit.RecordStandalone(r.Context(), s.Pool, audit.Event{
		OrgID: p.OrgID, RequestID: httpx.RequestID(r.Context()), ActorType: audit.ActorUser, ActorID: &p.UserID,
		ActorLabel: p.Email, ActorIP: httpx.ClientIP(r.Context()), SessionID: &p.SessionID, Action: "authz.denied",
		Outcome: audit.Denied, Details: map[string]any{"permission": perm, "method": r.Method, "path": r.URL.Path},
	})
	if err != nil {
		s.Log.Error("audit denied write failed", "err", err)
	}
}

func (s *Server) actor(r *http.Request, p *auth.Principal) devices.Actor {
	return devices.Actor{P: p, RequestID: httpx.RequestID(r.Context()), IP: httpx.ClientIP(r.Context())}
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, httpx.NotFound("resource")
	}
	return id, nil
}

func cursorUUID(r *http.Request) (*uuid.UUID, error) {
	c := r.URL.Query().Get("cursor")
	if c == "" {
		return nil, nil
	}
	id, err := uuid.Parse(c)
	if err != nil {
		return nil, httpx.BadRequest("invalid cursor")
	}
	return &id, nil
}

func uuidCursor(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
