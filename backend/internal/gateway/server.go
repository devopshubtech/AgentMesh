// Package gateway terminates agent connections: enrollment, device
// authentication and the persistent WebSocket control channel.
package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/commands"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/enrollment"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/platform/keys"
	"github.com/enfec/agentmesh/backend/internal/platform/ratelimit"
	"github.com/enfec/agentmesh/protocols/agentapi"
)

const challengeTTL = 60 * time.Second

// Options configures the gateway.
type Options struct {
	GatewayID         string
	MaxConnections    int
	HeartbeatInterval time.Duration
	TrustProxy        bool
}

// Gateway serves the agent endpoints.
type Gateway struct {
	opts       Options
	pool       *pgxpool.Pool
	bus        *bus.Bus
	log        *slog.Logger
	tokens     *auth.TokenIssuer
	commandKey ed25519.PublicKey
	enroll     *enrollment.Service
	commands   *commands.Service
	registry   *registry
	presence   *presence
	relays     *relayHub

	enrollRL    *ratelimit.Keyed
	challengeRL *ratelimit.Keyed
	deviceRL    *ratelimit.Keyed
	connectRL   *ratelimit.Keyed
}

// New constructs a Gateway.
func New(opts Options, pool *pgxpool.Pool, b *bus.Bus, log *slog.Logger, tokens *auth.TokenIssuer, commandKey ed25519.PublicKey) *Gateway {
	devSvc := devices.NewService(pool, b)
	g := &Gateway{
		opts: opts, pool: pool, bus: b, log: log, tokens: tokens, commandKey: commandKey,
		enroll:      enrollment.NewService(pool, b),
		commands:    commands.NewService(pool, b, devSvc, nil, 0),
		registry:    newRegistry(),
		relays:      newRelayHub(),
		enrollRL:    ratelimit.New(10, 5),
		challengeRL: ratelimit.New(120, 30),
		deviceRL:    ratelimit.New(20, 10),
		connectRL:   ratelimit.New(60, 20),
	}
	g.presence = newPresence(pool, log)
	return g
}

// Run starts background loops until ctx ends.
func (g *Gateway) Run(ctx context.Context) { g.presence.run(ctx) }

// Drain asks all agents to reconnect elsewhere (spread over maxWait) and
// waits for their sessions to close.
func (g *Gateway) Drain(maxWait time.Duration) {
	g.registry.drainAll(int(maxWait.Seconds()))
	done := make(chan struct{})
	go func() { g.registry.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(maxWait / 2):
		g.log.Warn("drain timeout; closing remaining sessions", "remaining", g.registry.count.Load())
	}
}

// bounded applies read/write deadlines to short request/response endpoints
// (the server itself has none, because WebSockets are long-lived).
func bounded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(15 * time.Second))
		_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
		next.ServeHTTP(w, r)
	})
}

// Handler returns the HTTP handler for agent traffic.
func (g *Gateway) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware, httpx.ClientIPMiddleware(g.opts.TrustProxy), httpx.Recover)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if g.pool.Ping(ctx) != nil || !g.bus.Connected() || g.registry.draining.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	r.With(bounded, httpx.AccessLog(g.log)).Post(agentapi.PathEnroll, httpx.H(g.handleEnroll))
	r.With(bounded).Post(agentapi.PathChallenge, httpx.H(g.handleChallenge))
	r.With(bounded).Post(agentapi.PathToken, httpx.H(g.handleToken))
	r.Get(agentapi.PathConnect, httpx.H(g.handleConnect))
	r.Get(agentapi.PathRelay, httpx.H(g.handleRelay))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, httpx.NotFound("route")) })
	return r
}

func (g *Gateway) commandKeys() []agentapi.CommandKey {
	return []agentapi.CommandKey{{KeyID: keys.ID(g.commandKey), PublicKey: g.commandKey}}
}

func (g *Gateway) handleEnroll(w http.ResponseWriter, r *http.Request) error {
	ip := httpx.ClientIP(r.Context())
	if ok, wait := g.enrollRL.Allow(ip); !ok {
		return httpx.RateLimited(wait)
	}
	var req agentapi.EnrollRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	res, err := g.enroll.Enroll(r.Context(), req, ip, httpx.RequestID(r.Context()))
	if errors.Is(err, enrollment.ErrInvalidToken) {
		g.log.Warn("enrollment rejected", "ip", ip, "reason", "invalid token")
		return httpx.WithCode(http.StatusUnauthorized, agentapi.CodeInvalidToken, "enrollment token is invalid, expired, revoked or exhausted")
	}
	if err != nil {
		return err
	}
	g.log.Info("device enrolled", "device_id", res.DeviceID, "status", res.Status, "ip", ip)
	httpx.WriteJSON(w, http.StatusCreated, agentapi.EnrollResponse{
		DeviceID: res.DeviceID.String(), OrgID: res.OrgID.String(), Status: res.Status, CommandKeys: g.commandKeys(),
	})
	return nil
}

func hashNonce(n []byte) []byte {
	s := sha256.Sum256(n)
	return s[:]
}

func (g *Gateway) handleChallenge(w http.ResponseWriter, r *http.Request) error {
	if ok, wait := g.challengeRL.Allow(httpx.ClientIP(r.Context())); !ok {
		return httpx.RateLimited(wait)
	}
	var req agentapi.ChallengeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return httpx.BadRequest("invalid device_id")
	}
	if ok, wait := g.deviceRL.Allow(deviceID.String()); !ok {
		return httpx.RateLimited(wait)
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	// Issued regardless of whether the device exists, so the endpoint does not
	// reveal which device ids are registered.
	if _, err := g.pool.Exec(r.Context(), `INSERT INTO auth_challenges (nonce_hash, device_id, expires_at) VALUES ($1, $2, $3)`,
		hashNonce(nonce), deviceID, time.Now().Add(challengeTTL)); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, agentapi.ChallengeResponse{Nonce: nonce, ExpiresIn: int(challengeTTL.Seconds())})
	return nil
}

func (g *Gateway) handleToken(w http.ResponseWriter, r *http.Request) error {
	ip := httpx.ClientIP(r.Context())
	if ok, wait := g.challengeRL.Allow(ip); !ok {
		return httpx.RateLimited(wait)
	}
	var req agentapi.TokenRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil || len(req.Nonce) != 32 || len(req.Signature) == 0 || len(req.Signature) > 128 {
		return httpx.Unauthorized("invalid token request")
	}
	// Consume the nonce first: single use even if verification fails.
	tag, err := g.pool.Exec(r.Context(), `DELETE FROM auth_challenges WHERE nonce_hash = $1 AND device_id = $2 AND expires_at > now()`,
		hashNonce(req.Nonce), deviceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Unauthorized("challenge is unknown or expired")
	}

	var (
		orgID  uuid.UUID
		status string
	)
	err = g.pool.QueryRow(r.Context(), `SELECT org_id, status FROM devices WHERE id = $1`, deviceID).Scan(&orgID, &status)
	if db.IsNoRows(err) {
		return httpx.Unauthorized("authentication failed")
	}
	if err != nil {
		return err
	}
	if status == devices.StatusRevoked {
		return httpx.WithCode(http.StatusUnauthorized, agentapi.CodeDeviceRevoked, "device has been revoked")
	}
	rows, err := g.pool.Query(r.Context(), `SELECT public_key FROM device_credentials WHERE device_id = $1 AND revoked_at IS NULL`, deviceID)
	if err != nil {
		return err
	}
	var pubs [][]byte
	for rows.Next() {
		var k []byte
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		pubs = append(pubs, k)
	}
	rows.Close()
	digest := agentapi.AuthDigest(deviceID.String(), req.Nonce)
	verified := false
	for _, der := range pubs {
		pub, err := enrollment.ParseDeviceKey(der)
		if err == nil && ecdsa.VerifyASN1(pub, digest, req.Signature) {
			verified = true
			break
		}
	}
	if !verified {
		g.log.Warn("device authentication failed", "device_id", deviceID, "ip", ip)
		return httpx.Unauthorized("authentication failed")
	}
	switch status {
	case devices.StatusPending:
		return httpx.WithCode(http.StatusForbidden, agentapi.CodeDevicePending, "device is awaiting approval")
	case devices.StatusDisabled:
		return httpx.WithCode(http.StatusForbidden, agentapi.CodeDeviceDisabled, "device is disabled")
	}
	tok, exp, err := g.tokens.Issue(deviceID, orgID, nil)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, agentapi.TokenResponse{AccessToken: tok, ExpiresIn: int(time.Until(exp).Seconds())})
	return nil
}

// bearer extracts a device token from the Authorization header.
func bearer(r *http.Request) string {
	raw, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return raw
}
