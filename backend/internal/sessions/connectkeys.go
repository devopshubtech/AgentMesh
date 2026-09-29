package sessions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/platform/textutil"
)

// Connect keys let people route their phone through a device without an
// account: an admin creates a key (shown as a QR code / link), anyone holding
// it can open exit-node sessions to that one device until it is revoked.

const connectKeyPrefix = "amk_"

// ErrInvalidConnectKey covers unknown, revoked and expired keys.
var ErrInvalidConnectKey = errors.New("invalid connect key")

// ConnectKey is the API representation (never includes the secret).
type ConnectKey struct {
	ID         uuid.UUID  `json:"id"`
	DeviceID   uuid.UUID  `json:"device_id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	Uses       int64      `json:"uses"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedBy  string     `json:"created_by"`
	Active     int        `json:"active_sessions"`
}

// CreatedKey is returned once, with the secret and a ready-to-share link.
type CreatedKey struct {
	ConnectKey
	Key  string `json:"key"`
	Link string `json:"link"`
}

// ConnectLink builds the shareable link. The web page at /join.html opens the
// app; the fragment (#...) never reaches server logs. rendezvous is an
// optional URL that always returns the server's current address.
func ConnectLink(publicURL, key, rendezvous string) string {
	v := url.Values{}
	v.Set("k", key)
	if rendezvous != "" {
		v.Set("r", rendezvous)
	}
	return strings.TrimRight(publicURL, "/") + "/join.html#" + v.Encode()
}

const selectKey = `
	SELECT k.id, k.device_id, k.label, k.created_at, k.expires_at, k.revoked_at, k.uses, k.last_used_at, u.email,
	       (SELECT count(*) FROM remote_sessions s WHERE s.connect_key_id = k.id AND s.status = 'active')
	FROM connect_keys k JOIN users u ON u.id = k.created_by`

func scanKey(row pgx.Row) (*ConnectKey, error) {
	var k ConnectKey
	err := row.Scan(&k.ID, &k.DeviceID, &k.Label, &k.CreatedAt, &k.ExpiresAt, &k.RevokedAt, &k.Uses, &k.LastUsedAt, &k.CreatedBy, &k.Active)
	return &k, err
}

// CreateConnectKey issues a key for deviceID. expiresInS 0 means no expiry.
func (s *Service) CreateConnectKey(ctx context.Context, a devices.Actor, deviceID uuid.UUID, label string, expiresInS int, publicURL, rendezvous string) (*CreatedKey, error) {
	label = textutil.Clean(label, 100)
	if label == "" {
		label = "Phone link"
	}
	if expiresInS < 0 || expiresInS > 3650*86400 {
		return nil, httpx.Validation{"expires_in_s": "must be between 0 (never) and 10 years"}.Err()
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	secret := connectKeyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	id := uuid.Must(uuid.NewV7())
	var exp *time.Time
	if expiresInS > 0 {
		t := time.Now().Add(time.Duration(expiresInS) * time.Second)
		exp = &t
	}
	var out *CreatedKey
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		d, err := s.devices.Get(ctx, tx, a.P.OrgID, deviceID)
		if err != nil {
			return err
		}
		if d.Status == devices.StatusRevoked {
			return httpx.InvalidState("device is revoked")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO connect_keys (id, org_id, device_id, key_hash, label, created_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, a.P.OrgID, deviceID, HashTicket(secret), label, a.P.UserID, exp); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "connect_key.create", TargetType: "device", TargetID: &deviceID,
			Details: map[string]any{"connect_key_id": id, "label": label, "expires_in_s": expiresInS, "device_name": d.Name},
		}); err != nil {
			return err
		}
		k, err := scanKey(tx.QueryRow(ctx, selectKey+` WHERE k.id = $1`, id))
		if err != nil {
			return err
		}
		out = &CreatedKey{ConnectKey: *k, Key: secret, Link: ConnectLink(publicURL, secret, rendezvous)}
		return nil
	})
	return out, err
}

// ListConnectKeys returns a device's keys, newest first.
func (s *Service) ListConnectKeys(ctx context.Context, orgID, deviceID uuid.UUID) ([]ConnectKey, error) {
	rows, err := s.pool.Query(ctx, selectKey+` WHERE k.org_id = $1 AND k.device_id = $2 ORDER BY k.id DESC LIMIT 200`, orgID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConnectKey{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

// RevokeConnectKey disables a key and ends the sessions opened with it.
func (s *Service) RevokeConnectKey(ctx context.Context, a devices.Actor, id uuid.UUID) error {
	var live []uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var deviceID uuid.UUID
		err := tx.QueryRow(ctx, `UPDATE connect_keys SET revoked_at = coalesce(revoked_at, now()) WHERE id = $1 AND org_id = $2 RETURNING device_id`,
			id, a.P.OrgID).Scan(&deviceID)
		if db.IsNoRows(err) {
			return httpx.NotFound("connect key")
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM remote_sessions WHERE connect_key_id = $1 AND status <> 'ended'`, id)
		if err != nil {
			return err
		}
		live, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "connect_key.revoke", TargetType: "device", TargetID: &deviceID,
			Details: map[string]any{"connect_key_id": id, "ended_sessions": len(live)},
		})
	})
	if err != nil {
		return err
	}
	for _, sid := range live {
		s.bus.PublishJSON(bus.RelayKillSubject(sid), map[string]string{"reason": "terminated"})
	}
	return nil
}

// OpenWithKey opens an exit-node session using a connect key (no login).
func (s *Service) OpenWithKey(ctx context.Context, key, clientLabel, ip, requestID, relayURL string) (*Created, error) {
	if !strings.HasPrefix(key, connectKeyPrefix) || len(key) > 128 {
		return nil, ErrInvalidConnectKey
	}
	var (
		id, orgID, deviceID, creator uuid.UUID
		label, creatorStatus         string
		expires, revoked             *time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT k.id, k.org_id, k.device_id, k.created_by, k.label, k.expires_at, k.revoked_at, u.status
		FROM connect_keys k JOIN users u ON u.id = k.created_by WHERE k.key_hash = $1`, HashTicket(key)).
		Scan(&id, &orgID, &deviceID, &creator, &label, &expires, &revoked, &creatorStatus)
	if db.IsNoRows(err) {
		return nil, ErrInvalidConnectKey
	}
	if err != nil {
		return nil, err
	}
	if revoked != nil || (expires != nil && time.Now().After(*expires)) || creatorStatus != "active" {
		return nil, ErrInvalidConnectKey
	}
	c, err := s.create(ctx, requester{
		OrgID: orgID, UserID: creator, ConnectKeyID: &id, ActorType: audit.ActorSystem,
		ActorLabel: "connect-key:" + label, RequestID: requestID, IP: ip, ClientLabel: clientLabel,
	}, deviceID, relayURL)
	if err != nil {
		return nil, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE connect_keys SET uses = uses + 1, last_used_at = now() WHERE id = $1`, id)
	return c, nil
}
