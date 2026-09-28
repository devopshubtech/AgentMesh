// Package commands creates signed device commands and tracks their lifecycle.
package commands

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/platform/keys"
	"github.com/enfec/agentmesh/protocols/agentapi"
	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

// Kinds and statuses.
const (
	KindExec   = "exec"
	KindAction = "action"

	StatusQueued    = "queued"
	StatusSent      = "sent"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusTimedOut  = "timed_out"
	StatusCanceled  = "canceled"
	StatusExpired   = "expired"
	StatusRejected  = "rejected"

	// MaxStoredOutput caps each stored output stream.
	MaxStoredOutput = 256 << 10
)

// Actions known to the MVP agents. A device must also declare "action.<name>".
var Actions = map[string]bool{"ping": true, "process.list": true, "inventory.refresh": true}

// Terminal reports whether status is final.
func Terminal(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusTimedOut, StatusCanceled, StatusExpired, StatusRejected:
		return true
	}
	return false
}

// Result is the stored outcome of a command.
type Result struct {
	ExitCode   *int    `json:"exit_code"`
	Stdout     string  `json:"stdout"`
	Stderr     string  `json:"stderr"`
	Truncated  bool    `json:"truncated"`
	Error      *string `json:"error"`
	DurationMS int64   `json:"duration_ms"`
}

// Command is the API representation.
type Command struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"-"`
	DeviceID    uuid.UUID `json:"device_id"`
	RequestedBy struct {
		ID    uuid.UUID `json:"id"`
		Email string    `json:"email"`
	} `json:"requested_by"`
	Kind       string     `json:"kind"`
	Action     *string    `json:"action"`
	Argv       []string   `json:"argv"`
	Shell      bool       `json:"shell"`
	TimeoutS   int        `json:"timeout_s"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	SentAt     *time.Time `json:"sent_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Result     *Result    `json:"result"`
}

// UpdatedEvent is published on each status change.
type UpdatedEvent struct {
	CommandID uuid.UUID `json:"command_id"`
	DeviceID  uuid.UUID `json:"device_id"`
	Status    string    `json:"status"`
}

const selectCommand = `
	SELECT c.id, c.org_id, c.device_id, u.id, u.email, c.kind, c.action, c.argv, c.shell, c.timeout_s, c.status,
	       c.created_at, c.expires_at, c.sent_at, c.started_at, c.finished_at,
	       r.command_id IS NOT NULL, r.exit_code, coalesce(r.stdout, ''), coalesce(r.stderr, ''),
	       coalesce(r.truncated, false), r.error, coalesce(r.duration_ms, 0)
	FROM device_commands c
	JOIN users u ON u.id = c.requested_by
	LEFT JOIN command_results r ON r.command_id = c.id`

func scanCommand(row pgx.Row) (*Command, error) {
	var c Command
	var hasResult bool
	var r Result
	if err := row.Scan(&c.ID, &c.OrgID, &c.DeviceID, &c.RequestedBy.ID, &c.RequestedBy.Email, &c.Kind, &c.Action, &c.Argv,
		&c.Shell, &c.TimeoutS, &c.Status, &c.CreatedAt, &c.ExpiresAt, &c.SentAt, &c.StartedAt, &c.FinishedAt,
		&hasResult, &r.ExitCode, &r.Stdout, &r.Stderr, &r.Truncated, &r.Error, &r.DurationMS); err != nil {
		return nil, err
	}
	if hasResult {
		c.Result = &r
	}
	return &c, nil
}

// Service implements command operations for the API and gateway.
type Service struct {
	pool    *pgxpool.Pool
	bus     *bus.Bus
	devices *devices.Service
	signer  ed25519.PrivateKey
	keyID   string
	ttl     time.Duration
}

// NewService constructs a Service. signer may be nil for gateway-only use.
func NewService(pool *pgxpool.Pool, b *bus.Bus, d *devices.Service, signer ed25519.PrivateKey, ttl time.Duration) *Service {
	s := &Service{pool: pool, bus: b, devices: d, signer: signer, ttl: ttl}
	if signer != nil {
		s.keyID = keys.ID(signer.Public().(ed25519.PublicKey))
	}
	return s
}

// Get returns a command within org.
func (s *Service) Get(ctx context.Context, q db.DBTX, orgID, id uuid.UUID) (*Command, error) {
	c, err := scanCommand(q.QueryRow(ctx, selectCommand+` WHERE c.org_id = $1 AND c.id = $2`, orgID, id))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("command")
	}
	return c, err
}

// ListForDevice returns a device's commands newest first.
func (s *Service) ListForDevice(ctx context.Context, orgID, deviceID uuid.UUID, before *uuid.UUID, limit int) ([]Command, *uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, selectCommand+`
		WHERE c.org_id = $1 AND c.device_id = $2 AND ($3::uuid IS NULL OR c.id < $3)
		ORDER BY c.id DESC LIMIT $4`, orgID, deviceID, before, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, *c)
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

// CreateInput is the create payload.
type CreateInput struct {
	Kind     string   `json:"kind"`
	Action   string   `json:"action"`
	Argv     []string `json:"argv"`
	Shell    bool     `json:"shell"`
	TimeoutS int      `json:"timeout_s"`
}

// Validate checks the payload shape (not permissions).
func (in *CreateInput) Validate() error {
	v := httpx.Validation{}
	if in.TimeoutS == 0 {
		in.TimeoutS = 60
	}
	if in.TimeoutS < 1 || in.TimeoutS > 3600 {
		v["timeout_s"] = "must be between 1 and 3600"
	}
	switch in.Kind {
	case KindAction:
		if !Actions[in.Action] {
			v["action"] = "unknown action"
		}
		if len(in.Argv) > 0 || in.Shell {
			v["argv"] = "not allowed for actions"
		}
	case KindExec:
		if in.Action != "" {
			v["action"] = "not allowed for exec"
		}
		switch {
		case len(in.Argv) == 0:
			v["argv"] = "required"
		case in.Shell && len(in.Argv) != 1:
			v["argv"] = "shell commands take exactly one element (the script)"
		case len(in.Argv) > 256:
			v["argv"] = "at most 256 arguments"
		default:
			total := 0
			for _, a := range in.Argv {
				total += len(a)
				if strings.ContainsRune(a, 0) {
					v["argv"] = "arguments must not contain NUL bytes"
				}
			}
			if total > 64<<10 {
				v["argv"] = "arguments exceed 64 KiB"
			}
			if in.Argv[0] == "" {
				v["argv"] = "program must not be empty"
			}
		}
	default:
		v["kind"] = "must be exec or action"
	}
	return v.Err()
}

// Capability returns the agent capability a command requires.
func (in *CreateInput) Capability() string {
	if in.Kind == KindExec {
		if in.Shell {
			return "exec.shell"
		}
		return "exec"
	}
	return "action." + in.Action
}

// Create signs, stores and dispatches a command.
func (s *Service) Create(ctx context.Context, a devices.Actor, deviceID uuid.UUID, in CreateInput, idemKey string) (*Command, bool, error) {
	if len(idemKey) > 128 {
		return nil, false, httpx.BadRequest("Idempotency-Key too long")
	}
	var out *Command
	created := false
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if idemKey != "" {
			var existing uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM device_commands WHERE requested_by = $1 AND idempotency_key = $2`, a.P.UserID, idemKey).Scan(&existing)
			if err == nil {
				c, err := s.Get(ctx, tx, a.P.OrgID, existing)
				if err != nil {
					return err
				}
				if c.DeviceID != deviceID {
					return httpx.Conflict("Idempotency-Key already used for a different request")
				}
				out = c
				return nil
			}
			if !db.IsNoRows(err) {
				return err
			}
		}
		d, err := s.devices.Get(ctx, tx, a.P.OrgID, deviceID)
		if err != nil {
			return err
		}
		if d.Status != devices.StatusActive {
			return httpx.InvalidState("device is " + d.Status)
		}
		if !d.HasCapability(in.Capability()) {
			return httpx.InvalidState("device does not support " + in.Capability())
		}
		id := uuid.Must(uuid.NewV7())
		now := time.Now().UTC().Truncate(time.Microsecond)
		exp := now.Add(s.ttl)
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		spec := &agentv1.CommandSpec{
			CommandId: id.String(), OrgId: a.P.OrgID.String(), DeviceId: deviceID.String(), Kind: in.Kind,
			Action: in.Action, Argv: in.Argv, Shell: in.Shell, TimeoutS: uint32(in.TimeoutS),
			IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(exp), IssuedBy: a.P.UserID.String(), Nonce: nonce,
		}
		specBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(spec)
		if err != nil {
			return err
		}
		sig := agentapi.SignCommand(s.signer, specBytes)
		var action *string
		var argv []string
		if in.Kind == KindAction {
			action = &in.Action
		} else {
			argv = in.Argv
		}
		var idem *string
		if idemKey != "" {
			idem = &idemKey
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO device_commands (id, org_id, device_id, requested_by, user_session_id, idempotency_key, request_id,
			    client_ip, kind, action, argv, shell, timeout_s, status, spec, signature, signing_key_id, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'queued', $14, $15, $16, $17, $18)`,
			id, a.P.OrgID, deviceID, a.P.UserID, a.P.SessionID, idem, a.RequestID, a.IP, in.Kind, action, argv,
			in.Shell, in.TimeoutS, specBytes, sig, s.keyID, now, exp); err != nil {
			return err
		}
		details := map[string]any{"kind": in.Kind, "timeout_s": in.TimeoutS, "device_name": d.Name}
		if in.Kind == KindAction {
			details["action"] = in.Action
		} else {
			details["argv"] = RedactArgv(in.Argv)
			details["shell"] = in.Shell
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "command.create", TargetType: "device", TargetID: &deviceID,
			Details: mergeID(details, id),
		}); err != nil {
			return err
		}
		out, err = s.Get(ctx, tx, a.P.OrgID, id)
		created = true
		return err
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, false, httpx.Conflict("duplicate Idempotency-Key, retry to fetch the original command")
		}
		return nil, false, err
	}
	if created {
		s.bus.PublishJSON(bus.DeviceCommandSubject(deviceID), bus.CommandMsg{CommandID: out.ID})
		s.bus.PublishEvent(a.P.OrgID, bus.EventCommandUpdate, UpdatedEvent{CommandID: out.ID, DeviceID: deviceID, Status: out.Status})
	}
	return out, created, nil
}

func mergeID(m map[string]any, id uuid.UUID) map[string]any {
	m["command_id"] = id
	return m
}

// Cancel cancels a queued command immediately, or asks the agent to stop a
// delivered one (the agent then reports a canceled result).
func (s *Service) Cancel(ctx context.Context, a devices.Actor, c *Command) (*Command, error) {
	if Terminal(c.Status) {
		return nil, httpx.InvalidState("command already " + c.Status)
	}
	var out *Command
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE device_commands SET status = 'canceled', finished_at = now() WHERE id = $1 AND status = 'queued'`, c.ID)
		if err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID, ActorLabel: a.P.Email,
			ActorIP: a.IP, SessionID: &a.P.SessionID, Action: "command.cancel", TargetType: "device", TargetID: &c.DeviceID,
			Details: map[string]any{"command_id": c.ID, "status_before": c.Status, "immediate": tag.RowsAffected() == 1},
		}); err != nil {
			return err
		}
		out, err = s.Get(ctx, tx, a.P.OrgID, c.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if out.Status == StatusCanceled {
		s.bus.PublishEvent(a.P.OrgID, bus.EventCommandUpdate, UpdatedEvent{CommandID: out.ID, DeviceID: out.DeviceID, Status: out.Status})
	} else {
		s.bus.PublishJSON(bus.DeviceCancelSubject(c.DeviceID), bus.CommandMsg{CommandID: c.ID})
	}
	return out, nil
}

// ---------------------------------------------------------------- gateway side

// Delivery is a signed command ready to send to an agent.
type Delivery struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Spec      []byte
	Signature []byte
	KeyID     string
}

// Claim marks a command sent and returns it for delivery. Commands already
// in "sent" are returned again (redelivery after reconnect); the agent's
// replay protection makes redelivery harmless.
func (s *Service) Claim(ctx context.Context, deviceID, commandID uuid.UUID) (*Delivery, error) {
	var d Delivery
	err := s.pool.QueryRow(ctx, `
		UPDATE device_commands SET status = 'sent', sent_at = coalesce(sent_at, now())
		WHERE id = $1 AND device_id = $2 AND status IN ('queued', 'sent') AND expires_at > now()
		RETURNING id, org_id, spec, signature, signing_key_id`, commandID, deviceID).
		Scan(&d.ID, &d.OrgID, &d.Spec, &d.Signature, &d.KeyID)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.bus.PublishEvent(d.OrgID, bus.EventCommandUpdate, UpdatedEvent{CommandID: d.ID, DeviceID: deviceID, Status: StatusSent})
	return &d, nil
}

// PendingIDs lists undelivered, unexpired commands for a device.
func (s *Service) PendingIDs(ctx context.Context, deviceID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM device_commands
		WHERE device_id = $1 AND status IN ('queued', 'sent') AND expires_at > now()
		ORDER BY id LIMIT 100`, deviceID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// MarkRunning records the agent's acknowledgement.
func (s *Service) MarkRunning(ctx context.Context, orgID, deviceID, commandID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE device_commands SET status = 'running', started_at = coalesce(started_at, now())
		WHERE id = $1 AND device_id = $2 AND status IN ('queued', 'sent', 'acked')`, commandID, deviceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		s.bus.PublishEvent(orgID, bus.EventCommandUpdate, UpdatedEvent{CommandID: commandID, DeviceID: deviceID, Status: StatusRunning})
	}
	return nil
}

func sanitizeOutput(b []byte) (string, bool) {
	truncated := false
	if len(b) > MaxStoredOutput {
		b = b[:MaxStoredOutput]
		truncated = true
	}
	s := strings.ToValidUTF8(string(b), "�")
	return strings.ReplaceAll(s, "\x00", ""), truncated
}

var resultStatus = map[agentv1.CommandStatus]string{
	agentv1.CommandStatus_COMMAND_STATUS_SUCCEEDED: StatusSucceeded,
	agentv1.CommandStatus_COMMAND_STATUS_FAILED:    StatusFailed,
	agentv1.CommandStatus_COMMAND_STATUS_TIMED_OUT: StatusTimedOut,
	agentv1.CommandStatus_COMMAND_STATUS_CANCELED:  StatusCanceled,
	agentv1.CommandStatus_COMMAND_STATUS_REJECTED:  StatusRejected,
}

// DeviceRef identifies the reporting device for audit.
type DeviceRef struct {
	ID    uuid.UUID
	OrgID uuid.UUID
	Name  string
	IP    string
}

// StoreResult persists an agent-reported result exactly once.
func (s *Service) StoreResult(ctx context.Context, dev DeviceRef, r *agentv1.CommandResult) error {
	cmdID, err := uuid.Parse(r.GetCommandId())
	if err != nil {
		return nil // malformed id: ignore
	}
	status, ok := resultStatus[r.GetStatus()]
	if !ok {
		status = StatusFailed
	}
	stdout, t1 := sanitizeOutput(r.GetStdout())
	stderr, t2 := sanitizeOutput(r.GetStderr())
	var exit *int
	if r.GetHasExitCode() {
		e := int(r.GetExitCode())
		exit = &e
	}
	var errMsg *string
	if e := strings.TrimSpace(r.GetError()); e != "" {
		if len(e) > 2000 {
			e = e[:2000]
		}
		errMsg = &e
	}
	dur := int64(r.GetDurationMs())
	stored := false
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE device_commands
			SET status = $3, started_at = coalesce(started_at, $4), finished_at = coalesce($5, now())
			WHERE id = $1 AND device_id = $2 AND status IN ('queued', 'sent', 'acked', 'running')`,
			cmdID, dev.ID, status, tsOrNil(r.GetStartedAt()), tsOrNil(r.GetFinishedAt()))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil // unknown, foreign or already-final command
		}
		stored = true
		if _, err := tx.Exec(ctx, `
			INSERT INTO command_results (command_id, exit_code, stdout, stderr, truncated, error, duration_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (command_id) DO NOTHING`,
			cmdID, exit, stdout, stderr, r.GetTruncated() || t1 || t2, errMsg, dur); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: dev.OrgID, ActorType: audit.ActorDevice, ActorID: &dev.ID, ActorLabel: dev.Name, ActorIP: dev.IP,
			Action: "command.result", TargetType: "device", TargetID: &dev.ID,
			Details: map[string]any{"command_id": cmdID, "status": status, "exit_code": exit, "duration_ms": dur, "error": errMsg},
		})
	})
	if err != nil {
		return err
	}
	if stored {
		s.bus.PublishEvent(dev.OrgID, bus.EventCommandUpdate, UpdatedEvent{CommandID: cmdID, DeviceID: dev.ID, Status: status})
	}
	return nil
}

func tsOrNil(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil || !ts.IsValid() || ts.AsTime().IsZero() {
		return nil
	}
	t := ts.AsTime()
	// Never trust device clocks beyond a sane window.
	if d := time.Since(t); d < -5*time.Minute || d > 48*time.Hour {
		return nil
	}
	return &t
}
