package sessions

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/platform/textutil"
)

// Pairing codes are the typed alternative to a QR code: an admin creates a
// 6-digit code, and any phone that enters it before it expires is issued its
// own connect key for the device. Codes are short-lived because six digits
// are guessable given enough attempts; the API also rate-limits redemption.

const (
	pairCodeDigits     = 6
	DefaultPairCodeTTL = 15 * time.Minute
	maxPairCodeTTL     = 24 * time.Hour
)

// ErrInvalidPairCode covers unknown, revoked and expired codes.
var ErrInvalidPairCode = errors.New("invalid pairing code")

// PairCode is the API representation (Code is only set when created).
type PairCode struct {
	ID        uuid.UUID  `json:"id"`
	DeviceID  uuid.UUID  `json:"device_id"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	Uses      int64      `json:"uses"`
	Code      string     `json:"code,omitempty"`
}

// NormalizePairCode strips spaces/dashes people type between digit groups.
func NormalizePairCode(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func newPairCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", pairCodeDigits, n.Int64()), nil
}

// CreatePairCode issues a code for deviceID valid for ttl (0 = default).
func (s *Service) CreatePairCode(ctx context.Context, a devices.Actor, deviceID uuid.UUID, label string, ttl time.Duration) (*PairCode, error) {
	label = textutil.Clean(label, 100)
	if label == "" {
		label = "Pairing code"
	}
	if ttl == 0 {
		ttl = DefaultPairCodeTTL
	}
	if ttl < time.Minute || ttl > maxPairCodeTTL {
		return nil, httpx.Validation{"expires_in_s": "must be between 60 and 86400 seconds"}.Err()
	}
	var out *PairCode
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		d, err := s.devices.Get(ctx, tx, a.P.OrgID, deviceID)
		if err != nil {
			return err
		}
		if d.Status == devices.StatusRevoked {
			return httpx.InvalidState("device is revoked")
		}
		// Pick a code that no other live code is using.
		var code string
		for i := 0; ; i++ {
			if code, err = newPairCode(); err != nil {
				return err
			}
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pair_codes WHERE code_hash = $1 AND revoked_at IS NULL AND expires_at > now())`,
				HashTicket(code)).Scan(&taken); err != nil {
				return err
			}
			if !taken {
				break
			}
			if i > 20 {
				return errors.New("could not allocate a pairing code")
			}
		}
		pc := &PairCode{ID: uuid.Must(uuid.NewV7()), DeviceID: deviceID, Label: label, Code: code}
		if err := tx.QueryRow(ctx, `
			INSERT INTO pair_codes (id, org_id, device_id, code_hash, label, created_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, now() + $7::interval) RETURNING created_at, expires_at`,
			pc.ID, a.P.OrgID, deviceID, HashTicket(code), label, a.P.UserID, fmt.Sprintf("%d seconds", int(ttl.Seconds()))).
			Scan(&pc.CreatedAt, &pc.ExpiresAt); err != nil {
			return err
		}
		out = pc
		return audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "pair_code.create", TargetType: "device", TargetID: &deviceID,
			Details: map[string]any{"pair_code_id": pc.ID, "label": label, "ttl_s": int(ttl.Seconds()), "device_name": d.Name},
		})
	})
	return out, err
}

// ListPairCodes returns a device's codes that are still usable.
func (s *Service) ListPairCodes(ctx context.Context, orgID, deviceID uuid.UUID) ([]PairCode, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, device_id, label, created_at, expires_at, revoked_at, uses FROM pair_codes
		WHERE org_id = $1 AND device_id = $2 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY id DESC LIMIT 50`, orgID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PairCode{}
	for rows.Next() {
		var p PairCode
		if err := rows.Scan(&p.ID, &p.DeviceID, &p.Label, &p.CreatedAt, &p.ExpiresAt, &p.RevokedAt, &p.Uses); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RevokePairCode stops a code from pairing more phones. Phones that already
// paired keep their own connect keys (revoke those individually).
func (s *Service) RevokePairCode(ctx context.Context, a devices.Actor, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var deviceID uuid.UUID
		err := tx.QueryRow(ctx, `UPDATE pair_codes SET revoked_at = coalesce(revoked_at, now()) WHERE id = $1 AND org_id = $2 RETURNING device_id`,
			id, a.P.OrgID).Scan(&deviceID)
		if db.IsNoRows(err) {
			return httpx.NotFound("pairing code")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "pair_code.revoke", TargetType: "device", TargetID: &deviceID,
			Details: map[string]any{"pair_code_id": id},
		})
	})
}

// Paired is what a phone receives for a valid code: its own connect key.
type Paired struct {
	Key        string
	Link       string
	DeviceName string
}

// RedeemPairCode exchanges a code for a new connect key on the code's device.
func (s *Service) RedeemPairCode(ctx context.Context, code, clientLabel, ip, requestID, publicURL, rendezvous string) (*Paired, error) {
	code = NormalizePairCode(code)
	if len(code) != pairCodeDigits {
		return nil, ErrInvalidPairCode
	}
	clientLabel = textutil.Clean(clientLabel, 60)
	var out *Paired
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			pcID, orgID, deviceID, creator uuid.UUID
			label, creatorStatus           string
		)
		err := tx.QueryRow(ctx, `
			SELECT p.id, p.org_id, p.device_id, p.created_by, p.label, u.status
			FROM pair_codes p JOIN users u ON u.id = p.created_by
			WHERE p.code_hash = $1 AND p.revoked_at IS NULL AND p.expires_at > now()
			ORDER BY p.id DESC LIMIT 1 FOR UPDATE OF p`, HashTicket(code)).
			Scan(&pcID, &orgID, &deviceID, &creator, &label, &creatorStatus)
		if db.IsNoRows(err) || (err == nil && creatorStatus != "active") {
			return ErrInvalidPairCode
		}
		if err != nil {
			return err
		}
		d, err := s.devices.Get(ctx, tx, orgID, deviceID)
		if err != nil {
			return err
		}
		if d.Status == devices.StatusRevoked {
			return ErrInvalidPairCode
		}
		secret, err := newConnectSecret()
		if err != nil {
			return err
		}
		keyLabel := label
		if clientLabel != "" {
			keyLabel = textutil.Clean(label+" · "+clientLabel, 100)
		}
		keyID := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO connect_keys (id, org_id, device_id, key_hash, label, created_by, pair_code_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, keyID, orgID, deviceID, HashTicket(secret), keyLabel, creator, pcID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE pair_codes SET uses = uses + 1 WHERE id = $1`, pcID); err != nil {
			return err
		}
		out = &Paired{Key: secret, Link: ConnectLink(publicURL, secret, rendezvous), DeviceName: d.Name}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: orgID, RequestID: requestID, ActorType: audit.ActorSystem, ActorLabel: "pair-code:" + label,
			ActorIP: ip, Action: "pair_code.redeem", TargetType: "device", TargetID: &deviceID,
			Details: map[string]any{"pair_code_id": pcID, "connect_key_id": keyID, "client_label": clientLabel},
		})
	})
	return out, err
}
