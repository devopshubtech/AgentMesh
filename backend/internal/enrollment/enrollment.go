// Package enrollment issues enrollment tokens and redeems them to register
// new device identities.
package enrollment

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/platform/textutil"
	"github.com/enfec/agentmesh/protocols/agentapi"
)

const tokenPrefix = "am_enr_"

// Token is the API representation of an enrollment token.
type Token struct {
	ID          uuid.UUID  `json:"id"`
	Description string     `json:"description"`
	AutoApprove bool       `json:"auto_approve"`
	MaxUses     *int       `json:"max_uses"`
	Uses        int        `json:"uses"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
	CreatedBy   struct {
		ID    uuid.UUID `json:"id"`
		Email string    `json:"email"`
	} `json:"created_by"`
}

// Created additionally carries the plaintext token, shown exactly once.
type Created struct {
	Token
	Secret string `json:"token"`
}

// Service implements enrollment.
type Service struct {
	pool *pgxpool.Pool
	bus  *bus.Bus
}

// NewService constructs a Service.
func NewService(pool *pgxpool.Pool, b *bus.Bus) *Service { return &Service{pool: pool, bus: b} }

func hashToken(t string) []byte {
	s := sha256.Sum256([]byte(t))
	return s[:]
}

const selectToken = `
	SELECT t.id, t.description, t.auto_approve, t.max_uses, t.uses, t.expires_at, t.revoked_at, t.created_at, u.id, u.email
	FROM enrollment_tokens t JOIN users u ON u.id = t.created_by`

func scanToken(row pgx.Row) (*Token, error) {
	var t Token
	err := row.Scan(&t.ID, &t.Description, &t.AutoApprove, &t.MaxUses, &t.Uses, &t.ExpiresAt, &t.RevokedAt,
		&t.CreatedAt, &t.CreatedBy.ID, &t.CreatedBy.Email)
	return &t, err
}

// NeverExpires as expires_in_s creates a token without an expiry; it is stored
// as NeverExpiresAt because expires_at is NOT NULL.
const NeverExpires = -1

// NeverExpiresAt is the expires_at of a token created with NeverExpires.
var NeverExpiresAt = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// CreateInput is the create payload.
type CreateInput struct {
	Description string `json:"description"`
	AutoApprove bool   `json:"auto_approve"`
	MaxUses     *int   `json:"max_uses"`
	ExpiresInS  int    `json:"expires_in_s"`
}

// Create issues a new token.
func (s *Service) Create(ctx context.Context, a devices.Actor, in CreateInput) (*Created, error) {
	v := httpx.Validation{}
	in.Description = strings.TrimSpace(in.Description)
	if len(in.Description) > 200 {
		v["description"] = "must be at most 200 characters"
	}
	if in.MaxUses != nil && (*in.MaxUses < 1 || *in.MaxUses > 100000) {
		v["max_uses"] = "must be between 1 and 100000"
	}
	if in.ExpiresInS == 0 {
		in.ExpiresInS = 86400
	}
	if in.ExpiresInS != NeverExpires && (in.ExpiresInS < 300 || in.ExpiresInS > 30*86400) {
		v["expires_in_s"] = "must be between 300 and 2592000, or -1 for no expiry"
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	secret := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	id := uuid.Must(uuid.NewV7())
	expiresAt := NeverExpiresAt
	if in.ExpiresInS != NeverExpires {
		expiresAt = time.Now().Add(time.Duration(in.ExpiresInS) * time.Second)
	}
	var out *Created
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO enrollment_tokens (id, org_id, token_hash, description, auto_approve, max_uses, expires_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, a.P.OrgID, hashToken(secret), in.Description, in.AutoApprove, in.MaxUses, expiresAt, a.P.UserID); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "enrollment_token.create", TargetType: "enrollment_token", TargetID: &id,
			Details: map[string]any{"description": in.Description, "auto_approve": in.AutoApprove, "max_uses": in.MaxUses, "expires_in_s": in.ExpiresInS},
		}); err != nil {
			return err
		}
		t, err := scanToken(tx.QueryRow(ctx, selectToken+` WHERE t.id = $1`, id))
		if err != nil {
			return err
		}
		out = &Created{Token: *t, Secret: secret}
		return nil
	})
	return out, err
}

// List returns tokens newest first.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, before *uuid.UUID, limit int) ([]Token, *uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, selectToken+`
		WHERE t.org_id = $1 AND ($2::uuid IS NULL OR t.id < $2) ORDER BY t.id DESC LIMIT $3`, orgID, before, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *uuid.UUID
	if len(out) > limit {
		out = out[:limit]
		id := out[limit-1].ID
		next = &id
	}
	return out, next, nil
}

// Revoke disables a token.
func (s *Service) Revoke(ctx context.Context, a devices.Actor, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE enrollment_tokens SET revoked_at = now() WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL`, id, a.P.OrgID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM enrollment_tokens WHERE id = $1 AND org_id = $2)`, id, a.P.OrgID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return httpx.NotFound("enrollment token")
			}
			return nil // already revoked: idempotent
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "enrollment_token.revoke", TargetType: "enrollment_token", TargetID: &id,
		})
	})
}

// ErrInvalidToken is returned for unknown, expired, revoked or exhausted tokens.
var ErrInvalidToken = errors.New("invalid enrollment token")

// Enrolled is the outcome of a successful Enroll.
type Enrolled struct {
	DeviceID uuid.UUID
	OrgID    uuid.UUID
	Status   string
}

// ParseDeviceKey parses and validates a P-256 PKIX public key.
func ParseDeviceKey(der []byte) (*ecdsa.PublicKey, error) {
	if len(der) == 0 || len(der) > 512 {
		return nil, errors.New("invalid public key")
	}
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, errors.New("invalid public key")
	}
	ek, ok := k.(*ecdsa.PublicKey)
	if !ok || ek.Curve != elliptic.P256() {
		return nil, errors.New("public key must be ECDSA P-256")
	}
	return ek, nil
}

// Enroll redeems a token and registers the device key. Called by the gateway.
func (s *Service) Enroll(ctx context.Context, req agentapi.EnrollRequest, ip, requestID string) (*Enrolled, error) {
	if !strings.HasPrefix(req.Token, tokenPrefix) || len(req.Token) > 128 {
		return nil, ErrInvalidToken
	}
	pub, err := ParseDeviceKey(req.PublicKey)
	if err != nil {
		return nil, httpx.BadRequest(err.Error())
	}
	if !ecdsa.VerifyASN1(pub, agentapi.EnrollDigest(req.Token, req.PublicKey), req.Signature) {
		return nil, httpx.BadRequest("proof-of-possession signature is invalid")
	}
	f := req.Facts
	f.Hostname = textutil.Clean(f.Hostname, 255)
	f.Platform = strings.ToLower(textutil.Clean(f.Platform, 32))
	f.Arch = strings.ToLower(textutil.Clean(f.Arch, 32))
	f.OSName = textutil.Clean(f.OSName, 100)
	f.OSVersion = textutil.Clean(f.OSVersion, 100)
	f.AgentVersion = textutil.Clean(f.AgentVersion, 64)
	f.MachineIDHash = textutil.Clean(f.MachineIDHash, 64)
	fp := sha256.Sum256(req.PublicKey)
	fingerprint := hex.EncodeToString(fp[:])
	name := f.Hostname
	if name == "" {
		name = "device-" + fingerprint[:8]
	}

	var out *Enrolled
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			tokenID, orgID uuid.UUID
			autoApprove    bool
			maxUses        *int
			uses           int
			expiresAt      time.Time
			revokedAt      *time.Time
		)
		err := tx.QueryRow(ctx, `
			SELECT id, org_id, auto_approve, max_uses, uses, expires_at, revoked_at
			FROM enrollment_tokens WHERE token_hash = $1 FOR UPDATE`, hashToken(req.Token)).
			Scan(&tokenID, &orgID, &autoApprove, &maxUses, &uses, &expiresAt, &revokedAt)
		if db.IsNoRows(err) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		if revokedAt != nil || time.Now().After(expiresAt) || (maxUses != nil && uses >= *maxUses) {
			return ErrInvalidToken
		}
		var replaces *uuid.UUID
		if f.MachineIDHash != "" {
			var prev uuid.UUID
			err := tx.QueryRow(ctx, `
				SELECT id FROM devices WHERE org_id = $1 AND machine_id_hash = $2 ORDER BY id DESC LIMIT 1`,
				orgID, f.MachineIDHash).Scan(&prev)
			if err == nil {
				replaces = &prev
			} else if !db.IsNoRows(err) {
				return err
			}
		}
		status := devices.StatusPending
		if autoApprove {
			status = devices.StatusActive
		}
		deviceID := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO devices (id, org_id, name, hostname, status, platform, arch, os_name, os_version, agent_version,
			                     machine_id_hash, enrolled_via, replaces_device_id, approved_at, last_ip)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, CASE WHEN $5 = 'active' THEN now() END, $14)`,
			deviceID, orgID, name, f.Hostname, status, f.Platform, f.Arch, f.OSName, f.OSVersion, f.AgentVersion,
			f.MachineIDHash, tokenID, replaces, ip); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO device_credentials (id, device_id, public_key, algorithm, fingerprint) VALUES ($1, $2, $3, 'ecdsa-p256', $4)`,
			uuid.Must(uuid.NewV7()), deviceID, req.PublicKey, fingerprint); err != nil {
			if db.IsUniqueViolation(err) {
				return httpx.Conflict("this device key is already enrolled")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE enrollment_tokens SET uses = uses + 1 WHERE id = $1`, tokenID); err != nil {
			return err
		}
		details := map[string]any{"hostname": f.Hostname, "platform": f.Platform, "arch": f.Arch, "os": f.OSName + " " + f.OSVersion,
			"agent_version": f.AgentVersion, "enrollment_token_id": tokenID, "status": status, "key_fingerprint": fingerprint}
		if replaces != nil {
			details["replaces_device_id"] = *replaces
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: orgID, RequestID: requestID, ActorType: audit.ActorDevice, ActorID: &deviceID, ActorLabel: name, ActorIP: ip,
			Action: "device.enroll", TargetType: "device", TargetID: &deviceID, Details: details,
		}); err != nil {
			return err
		}
		out = &Enrolled{DeviceID: deviceID, OrgID: orgID, Status: status}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.bus.PublishEvent(out.OrgID, bus.EventDeviceStatus, devices.StatusEvent{DeviceID: out.DeviceID, Status: out.Status, Connectivity: devices.Offline})
	return out, nil
}
