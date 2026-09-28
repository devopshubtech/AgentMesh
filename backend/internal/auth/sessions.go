package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
)

// Session errors surfaced to handlers.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidRefresh     = errors.New("invalid refresh token")
	ErrSessionInvalid     = errors.New("session no longer valid")
)

// Client kinds.
const (
	ClientWeb    = "web"
	ClientMobile = "mobile"
)

const refreshPrefix = "am_rt_"

type sessionPolicy struct{ sliding, absolute time.Duration }

var sessionPolicies = map[string]sessionPolicy{
	ClientWeb:    {sliding: 12 * time.Hour, absolute: 7 * 24 * time.Hour},
	ClientMobile: {sliding: 30 * 24 * time.Hour, absolute: 90 * 24 * time.Hour},
}

// SessionService implements login, refresh-token rotation and logout.
type SessionService struct {
	pool   *pgxpool.Pool
	tokens *TokenIssuer
	log    *slog.Logger
}

// NewSessionService constructs a SessionService.
func NewSessionService(pool *pgxpool.Pool, tokens *TokenIssuer, log *slog.Logger) *SessionService {
	return &SessionService{pool: pool, tokens: tokens, log: log}
}

// Tokens exposes the access-token issuer.
func (s *SessionService) Tokens() *TokenIssuer { return s.tokens }

// ClientInfo describes the caller for session bookkeeping and audit.
type ClientInfo struct {
	Client    string
	UserAgent string
	IP        string
	RequestID string
}

// Issued is the outcome of a successful login or refresh.
type Issued struct {
	UserID       uuid.UUID
	OrgID        uuid.UUID
	FamilyID     uuid.UUID
	Client       string
	AccessToken  string
	AccessExpiry time.Time
	RefreshToken string
	RefreshExp   time.Time
}

func newRefreshToken() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = refreshPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, hashRefresh(token), nil
}

func hashRefresh(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Login verifies credentials and opens a new session family.
func (s *SessionService) Login(ctx context.Context, email, password string, ci ClientInfo) (*Issued, error) {
	pol, ok := sessionPolicies[ci.Client]
	if !ok {
		return nil, fmt.Errorf("unknown client %q", ci.Client)
	}
	email = strings.TrimSpace(email)
	var (
		userID, orgID uuid.UUID
		hash, status  string
	)
	err := s.pool.QueryRow(ctx, `SELECT id, org_id, password_hash, status FROM users WHERE email = $1`, email).
		Scan(&userID, &orgID, &hash, &status)
	if db.IsNoRows(err) {
		_, _ = VerifyPassword(password, dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	match, err := VerifyPassword(password, hash)
	if err != nil {
		return nil, fmt.Errorf("verify password: %w", err)
	}
	if !match || status != "active" {
		reason := "bad_password"
		if match {
			reason = "user_disabled"
		}
		_ = audit.RecordStandalone(ctx, s.pool, audit.Event{
			OrgID: orgID, RequestID: ci.RequestID, ActorType: audit.ActorUser, ActorID: &userID, ActorLabel: email,
			ActorIP: ci.IP, Action: "auth.login", TargetType: "user", TargetID: &userID, Outcome: audit.Denied,
			Details: map[string]any{"reason": reason, "client": ci.Client},
		})
		return nil, ErrInvalidCredentials
	}

	now := time.Now()
	family := uuid.Must(uuid.NewV7())
	refresh, refreshHash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	absolute := now.Add(pol.absolute)
	exp := now.Add(pol.sliding)
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_sessions (id, family_id, user_id, refresh_token_hash, client, user_agent, ip, expires_at, absolute_expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			uuid.Must(uuid.NewV7()), family, userID, refreshHash, ci.Client, truncate(ci.UserAgent, 512), ci.IP, exp, absolute); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: orgID, RequestID: ci.RequestID, ActorType: audit.ActorUser, ActorID: &userID, ActorLabel: email,
			ActorIP: ci.IP, SessionID: &family, Action: "auth.login", TargetType: "user", TargetID: &userID,
			Details: map[string]any{"client": ci.Client},
		})
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	access, accessExp, err := s.tokens.Issue(userID, orgID, &family)
	if err != nil {
		return nil, err
	}
	return &Issued{UserID: userID, OrgID: orgID, FamilyID: family, Client: ci.Client, AccessToken: access,
		AccessExpiry: accessExp, RefreshToken: refresh, RefreshExp: exp}, nil
}

// Refresh rotates a refresh token. Presenting an already-rotated token is
// treated as theft: the entire session family is revoked.
func (s *SessionService) Refresh(ctx context.Context, refreshToken string, ci ClientInfo) (*Issued, error) {
	if !strings.HasPrefix(refreshToken, refreshPrefix) || len(refreshToken) > 128 {
		return nil, ErrInvalidRefresh
	}
	var out *Issued
	reuse := false
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			id, family, userID, orgID uuid.UUID
			client, userStatus, email string
			expires, absolute         time.Time
			rotatedAt, revokedAt      *time.Time
		)
		err := tx.QueryRow(ctx, `
			SELECT s.id, s.family_id, s.user_id, u.org_id, s.client, u.status, u.email,
			       s.expires_at, s.absolute_expires_at, s.rotated_at, s.revoked_at
			FROM user_sessions s JOIN users u ON u.id = s.user_id
			WHERE s.refresh_token_hash = $1
			FOR UPDATE OF s`, hashRefresh(refreshToken)).
			Scan(&id, &family, &userID, &orgID, &client, &userStatus, &email, &expires, &absolute, &rotatedAt, &revokedAt)
		if db.IsNoRows(err) {
			return ErrInvalidRefresh
		}
		if err != nil {
			return err
		}
		if revokedAt != nil {
			return ErrInvalidRefresh
		}
		if rotatedAt != nil {
			reuse = true
			if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`, family); err != nil {
				return err
			}
			return audit.Record(ctx, tx, audit.Event{
				OrgID: orgID, RequestID: ci.RequestID, ActorType: audit.ActorUser, ActorID: &userID, ActorLabel: email,
				ActorIP: ci.IP, SessionID: &family, Action: "auth.refresh_reuse", TargetType: "user", TargetID: &userID,
				Outcome: audit.Denied, Details: map[string]any{"note": "rotated refresh token presented; session family revoked"},
			})
		}
		now := time.Now()
		if now.After(expires) || now.After(absolute) || userStatus != "active" {
			return ErrInvalidRefresh
		}
		pol := sessionPolicies[client]
		newToken, newHash, err := newRefreshToken()
		if err != nil {
			return err
		}
		newExp := now.Add(pol.sliding)
		if newExp.After(absolute) {
			newExp = absolute
		}
		if _, err := tx.Exec(ctx, `UPDATE user_sessions SET rotated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_sessions (id, family_id, user_id, refresh_token_hash, client, user_agent, ip, expires_at, absolute_expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			uuid.Must(uuid.NewV7()), family, userID, newHash, client, truncate(ci.UserAgent, 512), ci.IP, newExp, absolute); err != nil {
			return err
		}
		access, accessExp, err := s.tokens.Issue(userID, orgID, &family)
		if err != nil {
			return err
		}
		out = &Issued{UserID: userID, OrgID: orgID, FamilyID: family, Client: client, AccessToken: access,
			AccessExpiry: accessExp, RefreshToken: newToken, RefreshExp: newExp}
		return nil
	})
	if reuse {
		s.log.Warn("refresh token reuse detected; session family revoked", "ip", ci.IP)
		return nil, ErrInvalidRefresh
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Logout revokes a session family.
func (s *SessionService) Logout(ctx context.Context, p *Principal, ci ClientInfo) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`, p.SessionID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: p.OrgID, RequestID: ci.RequestID, ActorType: audit.ActorUser, ActorID: &p.UserID, ActorLabel: p.Email,
			ActorIP: ci.IP, SessionID: &p.SessionID, Action: "auth.logout", TargetType: "user", TargetID: &p.UserID,
		})
	})
}

// LogoutByRefresh revokes the family owning a refresh token (web logout
// when the access token has already expired).
func (s *SessionService) LogoutByRefresh(ctx context.Context, refreshToken string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE user_sessions SET revoked_at = now()
		WHERE revoked_at IS NULL AND family_id = (SELECT family_id FROM user_sessions WHERE refresh_token_hash = $1)`,
		hashRefresh(refreshToken))
	return err
}

// LoadPrincipal resolves the live principal for an access token. It checks
// that the user is active and the session family is still valid, so logout,
// disable and role changes take effect immediately.
func (s *SessionService) LoadPrincipal(ctx context.Context, userID, familyID uuid.UUID) (*Principal, error) {
	var (
		p      = Principal{SessionID: familyID}
		status string
		perms  []string
		live   bool
	)
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.org_id, u.email, u.status,
		       coalesce((array_agg(DISTINCT r.name))[1], ''),
		       coalesce(array_agg(DISTINCT rp.permission_key) FILTER (WHERE rp.permission_key IS NOT NULL), '{}'),
		       EXISTS (SELECT 1 FROM user_sessions s
		               WHERE s.family_id = $2 AND s.user_id = u.id AND s.revoked_at IS NULL
		                 AND s.rotated_at IS NULL AND s.expires_at > now())
		FROM users u
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		LEFT JOIN roles r ON r.id = ur.role_id
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE u.id = $1
		GROUP BY u.id`, userID, familyID).
		Scan(&p.UserID, &p.OrgID, &p.Email, &status, &p.Role, &perms, &live)
	if db.IsNoRows(err) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("load principal: %w", err)
	}
	if status != "active" || !live {
		return nil, ErrSessionInvalid
	}
	p.Permissions = make(map[string]bool, len(perms))
	for _, k := range perms {
		p.Permissions[k] = true
	}
	return &p, nil
}
