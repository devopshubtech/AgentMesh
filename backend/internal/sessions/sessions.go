// Package sessions manages relayed data sessions between an operator client
// and an agent. The MVP kind is "exit_node": the client's traffic leaves the
// internet through the agent's network.
package sessions

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/platform/keys"
	"github.com/enfec/agentmesh/backend/internal/platform/textutil"
	"github.com/enfec/agentmesh/protocols/agentapi"
	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

const (
	KindExitNode = "exit_node"

	StatusPending = "pending"
	StatusActive  = "active"
	StatusEnded   = "ended"

	// CapabilityExitNode must be declared by the agent (opt-in via local policy).
	CapabilityExitNode = "exit_node"

	joinWindow             = 60 * time.Second
	maxDuration            = 12 * time.Hour
	maxOpenSessionsPerUser = 3
	maxOpenSessionsPerKey  = 50
)

// Session is the API representation.
type Session struct {
	ID       uuid.UUID `json:"id"`
	DeviceID uuid.UUID `json:"device_id"`
	Kind     string    `json:"kind"`
	Status   string    `json:"status"`
	User     struct {
		ID    uuid.UUID `json:"id"`
		Email string    `json:"email"`
	} `json:"user"`
	ClientIP    string     `json:"client_ip"`
	ClientLabel string     `json:"client_label"`
	CreatedAt   time.Time  `json:"created_at"`
	MaxEndsAt   time.Time  `json:"max_ends_at"`
	StartedAt   *time.Time `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	EndReason   *string    `json:"end_reason"`
	BytesUp     int64      `json:"bytes_up"`
	BytesDown   int64      `json:"bytes_down"`
	// ConnectKeyLabel names the connect link (QR code) the phone used, if any.
	ConnectKeyLabel *string `json:"connect_key_label"`
}

// Created is returned to the client that opened a session.
type Created struct {
	Session         *Session  `json:"session"`
	RelayURL        string    `json:"relay_url"`
	Ticket          string    `json:"ticket"`
	TicketExpiresAt time.Time `json:"ticket_expires_at"`
	DeviceName      string    `json:"device_name"`
}

const selectSession = `
	SELECT s.id, s.device_id, s.kind, s.status, u.id, u.email, s.client_ip, s.client_label, s.created_at,
	       s.max_ends_at, s.started_at, s.ended_at, s.end_reason, s.bytes_up, s.bytes_down, k.label
	FROM remote_sessions s JOIN users u ON u.id = s.user_id
	LEFT JOIN connect_keys k ON k.id = s.connect_key_id`

func scanSession(row pgx.Row) (*Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.DeviceID, &s.Kind, &s.Status, &s.User.ID, &s.User.Email, &s.ClientIP, &s.ClientLabel,
		&s.CreatedAt, &s.MaxEndsAt, &s.StartedAt, &s.EndedAt, &s.EndReason, &s.BytesUp, &s.BytesDown, &s.ConnectKeyLabel)
	return &s, err
}

// Service implements session operations for the API.
type Service struct {
	pool    *pgxpool.Pool
	bus     *bus.Bus
	devices *devices.Service
	signer  ed25519.PrivateKey
	keyID   string
}

// NewService constructs a Service.
func NewService(pool *pgxpool.Pool, b *bus.Bus, d *devices.Service, signer ed25519.PrivateKey) *Service {
	return &Service{pool: pool, bus: b, devices: d, signer: signer, keyID: keys.ID(signer.Public().(ed25519.PublicKey))}
}

// HashTicket is how tickets are stored and looked up.
func HashTicket(t string) []byte {
	s := sha256.Sum256([]byte(t))
	return s[:]
}

func newTicket() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "am_rly_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// Get returns a session within org.
func (s *Service) Get(ctx context.Context, q db.DBTX, orgID, id uuid.UUID) (*Session, error) {
	sess, err := scanSession(q.QueryRow(ctx, selectSession+` WHERE s.org_id = $1 AND s.id = $2`, orgID, id))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("session")
	}
	return sess, err
}

// requester describes who opens a session: a signed-in user, or a connect
// key (QR code / link) that was issued by a user.
type requester struct {
	OrgID        uuid.UUID
	UserID       uuid.UUID // the user, or the key's creator
	UserSession  *uuid.UUID
	ConnectKeyID *uuid.UUID
	ActorType    string
	ActorID      *uuid.UUID
	ActorLabel   string
	RequestID    string
	IP           string
	ClientLabel  string
}

// CreateExitSession authorizes a relayed exit-node session to deviceID for a signed-in user.
func (s *Service) CreateExitSession(ctx context.Context, a devices.Actor, deviceID uuid.UUID, label, relayURL string) (*Created, error) {
	return s.create(ctx, requester{
		OrgID: a.P.OrgID, UserID: a.P.UserID, UserSession: &a.P.SessionID, ActorType: audit.ActorUser,
		ActorID: &a.P.UserID, ActorLabel: a.P.Email, RequestID: a.RequestID, IP: a.IP, ClientLabel: label,
	}, deviceID, relayURL)
}

func (s *Service) create(ctx context.Context, r requester, deviceID uuid.UUID, relayURL string) (*Created, error) {
	label := textutil.Clean(r.ClientLabel, 100)
	clientTicket, err := newTicket()
	if err != nil {
		return nil, err
	}
	agentTicket, err := newTicket()
	if err != nil {
		return nil, err
	}
	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC().Truncate(time.Microsecond)
	deadline := now.Add(joinWindow)
	var out *Created
	var specBytes, sig []byte
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		d, err := s.devices.Get(ctx, tx, r.OrgID, deviceID)
		if err != nil {
			return err
		}
		switch {
		case d.Status != devices.StatusActive:
			return httpx.InvalidState("device is " + d.Status)
		case d.Connectivity != devices.Online:
			return httpx.InvalidState("device is offline")
		case !d.HasCapability(CapabilityExitNode):
			return httpx.InvalidState("device does not allow exit-node sessions (enable allow_exit_node in the agent's local policy)")
		}
		var open int
		if r.ConnectKeyID != nil {
			err = tx.QueryRow(ctx, `SELECT count(*) FROM remote_sessions WHERE connect_key_id = $1 AND status <> 'ended'`, *r.ConnectKeyID).Scan(&open)
			if err == nil && open >= maxOpenSessionsPerKey {
				return httpx.InvalidState("too many phones are connected with this link right now")
			}
		} else {
			err = tx.QueryRow(ctx, `SELECT count(*) FROM remote_sessions WHERE user_id = $1 AND connect_key_id IS NULL AND status <> 'ended'`, r.UserID).Scan(&open)
			if err == nil && open >= maxOpenSessionsPerUser {
				return httpx.InvalidState("too many open sessions; disconnect one first")
			}
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO remote_sessions (id, org_id, device_id, user_id, kind, status, client_ip, client_label, join_deadline, max_ends_at, created_at, connect_key_id)
			VALUES ($1, $2, $3, $4, 'exit_node', 'pending', $5, $6, $7, $8, $9, $10)`,
			id, r.OrgID, deviceID, r.UserID, r.IP, label, deadline, now.Add(maxDuration), now, r.ConnectKeyID); err != nil {
			return err
		}
		for side, t := range map[string]string{"client": clientTicket, "agent": agentTicket} {
			if _, err := tx.Exec(ctx, `INSERT INTO relay_tickets (ticket_hash, session_id, side, expires_at) VALUES ($1, $2, $3, $4)`,
				HashTicket(t), id, side, deadline); err != nil {
				return err
			}
		}
		spec := &agentv1.SessionSpec{
			SessionId: id.String(), OrgId: r.OrgID.String(), DeviceId: deviceID.String(), Kind: KindExitNode,
			IssuedBy: r.UserID.String(), IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(deadline),
			RelayTicket: agentTicket, MaxDurationS: uint32(maxDuration / time.Second),
		}
		specBytes, err = proto.MarshalOptions{Deterministic: true}.Marshal(spec)
		if err != nil {
			return err
		}
		sig = agentapi.SignSession(s.signer, specBytes)
		details := map[string]any{"session_id": id, "kind": KindExitNode, "client_label": label, "device_name": d.Name}
		if r.ConnectKeyID != nil {
			details["connect_key_id"] = *r.ConnectKeyID
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: r.OrgID, RequestID: r.RequestID, ActorType: r.ActorType, ActorID: r.ActorID, ActorLabel: r.ActorLabel,
			ActorIP: r.IP, SessionID: r.UserSession, Action: "session.create", TargetType: "device", TargetID: &deviceID,
			Details: details,
		}); err != nil {
			return err
		}
		sess, err := s.Get(ctx, tx, r.OrgID, id)
		if err != nil {
			return err
		}
		out = &Created{Session: sess, RelayURL: relayURL, Ticket: clientTicket, TicketExpiresAt: deadline, DeviceName: d.Name}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.bus.PublishJSON(bus.DeviceControlSubject(deviceID), bus.ControlMsg{
		Type: bus.ControlSessionOpen, SessionID: id, Spec: specBytes, Signature: sig, KeyID: s.keyID})
	s.bus.PublishEvent(r.OrgID, bus.EventSessionUpdate, map[string]any{"session_id": id, "device_id": deviceID, "status": StatusPending})
	return out, nil
}

// Filter narrows List.
type Filter struct {
	DeviceID   *uuid.UUID
	UserID     *uuid.UUID
	ActiveOnly bool
}

// List returns sessions newest first.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, f Filter, before *uuid.UUID, limit int) ([]Session, *uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, selectSession+`
		WHERE s.org_id = $1 AND ($2::uuid IS NULL OR s.device_id = $2) AND ($3::uuid IS NULL OR s.user_id = $3)
		  AND (NOT $4 OR s.status <> 'ended') AND ($5::uuid IS NULL OR s.id < $5)
		ORDER BY s.id DESC LIMIT $6`, orgID, f.DeviceID, f.UserID, f.ActiveOnly, before, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, *sess)
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

// Terminate ends a session. Users may end their own sessions; ending
// someone else's requires devices.manage.
func (s *Service) Terminate(ctx context.Context, a devices.Actor, id uuid.UUID) (*Session, error) {
	sess, err := s.Get(ctx, s.pool, a.P.OrgID, id)
	if err != nil {
		return nil, err
	}
	if sess.User.ID != a.P.UserID && !a.P.Can(auth.PermDevicesManage) {
		return nil, httpx.Forbidden("only the session owner or a device manager can end this session")
	}
	if sess.Status == StatusEnded {
		return sess, nil
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// Pending sessions are ended here; active ones are ended by the relay
		// when it receives the kill message (it owns the byte counters).
		if _, err := tx.Exec(ctx, `
			UPDATE remote_sessions SET status = 'ended', ended_at = now(), end_reason = 'terminated'
			WHERE id = $1 AND status = 'pending'`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM relay_tickets WHERE session_id = $1`, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "session.terminate", TargetType: "device", TargetID: &sess.DeviceID,
			Details: map[string]any{"session_id": id, "status_before": sess.Status},
		})
	})
	if err != nil {
		return nil, err
	}
	s.bus.PublishJSON(bus.RelayKillSubject(id), map[string]string{"reason": "terminated"})
	return s.Get(ctx, s.pool, a.P.OrgID, id)
}

// ParseActive parses the ?active= query flag.
func ParseActive(v string) bool { return strings.EqualFold(v, "true") || v == "1" }
