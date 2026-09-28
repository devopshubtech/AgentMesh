// Package devices owns device records, their lifecycle and inventory.
package devices

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
)

// Lifecycle states.
const (
	StatusPending  = "pending"
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusRevoked  = "revoked"

	Online  = "online"
	Offline = "offline"
)

// Inventory is the hardware/OS snapshot reported by the agent.
type Inventory struct {
	CPU struct {
		Model   string `json:"model"`
		Cores   uint32 `json:"cores"`
		Threads uint32 `json:"threads"`
	} `json:"cpu"`
	Memory struct {
		TotalBytes uint64 `json:"total_bytes"`
		UsedBytes  uint64 `json:"used_bytes"`
	} `json:"memory"`
	Disks       []Disk         `json:"disks"`
	Network     []NetInterface `json:"network"`
	UptimeS     uint64         `json:"uptime_s"`
	BootTime    *time.Time     `json:"boot_time"`
	CollectedAt time.Time      `json:"collected_at"`
}

// Disk is one mounted filesystem.
type Disk struct {
	Mount      string `json:"mount"`
	FSType     string `json:"fstype"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

// NetInterface is one network interface.
type NetInterface struct {
	Name  string   `json:"name"`
	MAC   string   `json:"mac"`
	Addrs []string `json:"addrs"`
}

// Device is the API representation of a managed device.
type Device struct {
	ID            uuid.UUID  `json:"id"`
	OrgID         uuid.UUID  `json:"-"`
	Name          string     `json:"name"`
	Hostname      string     `json:"hostname"`
	Status        string     `json:"status"`
	Connectivity  string     `json:"connectivity"`
	Platform      string     `json:"platform"`
	Arch          string     `json:"arch"`
	OSName        string     `json:"os_name"`
	OSVersion     string     `json:"os_version"`
	OSBuild       string     `json:"os_build"`
	KernelVersion string     `json:"kernel_version"`
	AgentVersion  string     `json:"agent_version"`
	Capabilities  []string   `json:"capabilities"`
	Inventory     *Inventory `json:"inventory"`
	LastSeenAt    *time.Time `json:"last_seen_at"`
	LastIP        *string    `json:"last_ip"`
	CreatedAt     time.Time  `json:"created_at"`
	ApprovedAt    *time.Time `json:"approved_at"`
}

// HasCapability reports whether the agent declared cap.
func (d *Device) HasCapability(c string) bool {
	for _, x := range d.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

// StatusEvent is published on every status/connectivity change.
type StatusEvent struct {
	DeviceID     uuid.UUID  `json:"device_id"`
	Status       string     `json:"status"`
	Connectivity string     `json:"connectivity"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
}

// UpdatedEvent tells dashboards to refetch a device.
type UpdatedEvent struct {
	DeviceID uuid.UUID `json:"device_id"`
}

const selectDevice = `
	SELECT id, org_id, name, hostname, status, connectivity, platform, arch, os_name, os_version, os_build,
	       kernel_version, agent_version, capabilities, inventory, last_seen_at, last_ip, created_at, approved_at
	FROM devices`

func scanDevice(row pgx.Row) (*Device, error) {
	var d Device
	var inv []byte
	if err := row.Scan(&d.ID, &d.OrgID, &d.Name, &d.Hostname, &d.Status, &d.Connectivity, &d.Platform, &d.Arch,
		&d.OSName, &d.OSVersion, &d.OSBuild, &d.KernelVersion, &d.AgentVersion, &d.Capabilities, &inv,
		&d.LastSeenAt, &d.LastIP, &d.CreatedAt, &d.ApprovedAt); err != nil {
		return nil, err
	}
	if len(inv) > 0 {
		var i Inventory
		if err := json.Unmarshal(inv, &i); err == nil {
			d.Inventory = &i
		}
	}
	if d.Capabilities == nil {
		d.Capabilities = []string{}
	}
	return &d, nil
}

// Service implements device queries and lifecycle operations.
type Service struct {
	pool *pgxpool.Pool
	bus  *bus.Bus
}

// NewService constructs a Service.
func NewService(pool *pgxpool.Pool, b *bus.Bus) *Service { return &Service{pool: pool, bus: b} }

// Get returns a device within org.
func (s *Service) Get(ctx context.Context, q db.DBTX, orgID, id uuid.UUID) (*Device, error) {
	d, err := scanDevice(q.QueryRow(ctx, selectDevice+` WHERE org_id = $1 AND id = $2`, orgID, id))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("device")
	}
	return d, err
}

func getForUpdate(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) (*Device, error) {
	d, err := scanDevice(tx.QueryRow(ctx, selectDevice+` WHERE org_id = $1 AND id = $2 FOR UPDATE`, orgID, id))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("device")
	}
	return d, err
}

// Filter narrows List.
type Filter struct {
	Status       string
	Connectivity string
	Platform     string
	Query        string
}

type cursor struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

func encodeCursor(d Device) string {
	b, _ := json.Marshal(cursor{Name: d.Name, ID: d.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (*cursor, error) {
	if s == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, httpx.BadRequest("invalid cursor")
	}
	var c cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, httpx.BadRequest("invalid cursor")
	}
	return &c, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List returns devices ordered by name with keyset pagination.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, f Filter, cur string, limit int) ([]Device, *string, error) {
	c, err := decodeCursor(cur)
	if err != nil {
		return nil, nil, err
	}
	var afterName *string
	var afterID *uuid.UUID
	if c != nil {
		afterName, afterID = &c.Name, &c.ID
	}
	var like *string
	if q := strings.TrimSpace(f.Query); q != "" {
		l := "%" + escapeLike(strings.ToLower(q)) + "%"
		like = &l
	}
	rows, err := s.pool.Query(ctx, selectDevice+`
		WHERE org_id = $1
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR connectivity = $3)
		  AND ($4 = '' OR platform = $4)
		  AND ($5::text IS NULL OR lower(name) LIKE $5 OR lower(hostname) LIKE $5)
		  AND ($6::text IS NULL OR (name, id) > ($6, $7::uuid))
		ORDER BY name, id
		LIMIT $8`, orgID, f.Status, f.Connectivity, f.Platform, like, afterName, afterID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		n := encodeCursor(out[limit-1])
		next = &n
	}
	return out, next, nil
}

// Summary holds fleet counters.
type Summary struct {
	Total    int `json:"total"`
	Online   int `json:"online"`
	Offline  int `json:"offline"`
	Pending  int `json:"pending"`
	Disabled int `json:"disabled"`
	Revoked  int `json:"revoked"`
}

// Summary counts devices by state.
func (s *Service) Summary(ctx context.Context, orgID uuid.UUID) (Summary, error) {
	var sum Summary
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE status <> 'revoked' AND connectivity = 'online'),
		       count(*) FILTER (WHERE status <> 'revoked' AND connectivity = 'offline'),
		       count(*) FILTER (WHERE status = 'pending'),
		       count(*) FILTER (WHERE status = 'disabled'),
		       count(*) FILTER (WHERE status = 'revoked')
		FROM devices WHERE org_id = $1`, orgID).
		Scan(&sum.Total, &sum.Online, &sum.Offline, &sum.Pending, &sum.Disabled, &sum.Revoked)
	return sum, err
}

// Actor carries request context for auditing.
type Actor struct {
	P         *auth.Principal
	RequestID string
	IP        string
}

func (a Actor) event(action string, deviceID uuid.UUID, details map[string]any) audit.Event {
	return audit.Event{
		OrgID: a.P.OrgID, RequestID: a.RequestID, ActorType: audit.ActorUser, ActorID: &a.P.UserID,
		ActorLabel: a.P.Email, ActorIP: a.IP, SessionID: &a.P.SessionID, Action: action,
		TargetType: "device", TargetID: &deviceID, Details: details,
	}
}

// Rename changes the display name.
func (s *Service) Rename(ctx context.Context, a Actor, id uuid.UUID, name string) (*Device, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, httpx.Validation{"name": "must be 1-100 characters"}.Err()
	}
	var out *Device
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		d, err := getForUpdate(ctx, tx, a.P.OrgID, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET name = $2, updated_at = now() WHERE id = $1`, id, name); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, a.event("device.rename", id, map[string]any{"from": d.Name, "to": name})); err != nil {
			return err
		}
		out, err = s.Get(ctx, tx, a.P.OrgID, id)
		return err
	})
	if err == nil {
		s.bus.PublishEvent(a.P.OrgID, bus.EventDeviceUpdated, UpdatedEvent{DeviceID: id})
	}
	return out, err
}

// Transition kinds.
const (
	OpApprove = "approve"
	OpDisable = "disable"
	OpEnable  = "enable"
	OpRevoke  = "revoke"
)

var transitions = map[string]struct {
	from []string
	to   string
}{
	OpApprove: {from: []string{StatusPending}, to: StatusActive},
	OpDisable: {from: []string{StatusActive, StatusPending}, to: StatusDisabled},
	OpEnable:  {from: []string{StatusDisabled}, to: StatusActive},
	OpRevoke:  {from: []string{StatusPending, StatusActive, StatusDisabled}, to: StatusRevoked},
}

// Transition applies a lifecycle operation, disconnecting the agent when needed.
func (s *Service) Transition(ctx context.Context, a Actor, id uuid.UUID, op string) (*Device, error) {
	tr, ok := transitions[op]
	if !ok {
		return nil, httpx.BadRequest("unknown operation")
	}
	var out *Device
	canceled := 0
	var canceledIDs []uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		d, err := getForUpdate(ctx, tx, a.P.OrgID, id)
		if err != nil {
			return err
		}
		allowed := false
		for _, f := range tr.from {
			if d.Status == f {
				allowed = true
			}
		}
		if !allowed {
			return httpx.InvalidState("cannot " + op + " a device in state " + d.Status)
		}
		switch op {
		case OpApprove:
			_, err = tx.Exec(ctx, `UPDATE devices SET status = $2, approved_by = $3, approved_at = now(), updated_at = now() WHERE id = $1`,
				id, tr.to, a.P.UserID)
		case OpRevoke:
			if _, err = tx.Exec(ctx, `UPDATE devices SET status = $2, connectivity = 'offline', updated_at = now() WHERE id = $1`, id, tr.to); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE device_credentials SET revoked_at = now() WHERE device_id = $1 AND revoked_at IS NULL`, id); err != nil {
				return err
			}
			rows, qerr := tx.Query(ctx, `
				UPDATE device_commands SET status = 'canceled', finished_at = now()
				WHERE device_id = $1 AND status IN ('queued', 'sent', 'acked', 'running')
				RETURNING id`, id)
			if qerr != nil {
				return qerr
			}
			canceledIDs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			canceled = len(canceledIDs)
		case OpDisable:
			_, err = tx.Exec(ctx, `UPDATE devices SET status = $2, connectivity = 'offline', updated_at = now() WHERE id = $1`, id, tr.to)
		default:
			_, err = tx.Exec(ctx, `UPDATE devices SET status = $2, updated_at = now() WHERE id = $1`, id, tr.to)
		}
		if err != nil {
			return err
		}
		details := map[string]any{"from": d.Status, "to": tr.to, "name": d.Name}
		if op == OpRevoke {
			details["canceled_commands"] = canceled
		}
		if err := audit.Record(ctx, tx, a.event("device."+op, id, details)); err != nil {
			return err
		}
		out, err = s.Get(ctx, tx, a.P.OrgID, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, cid := range canceledIDs {
		s.bus.PublishEvent(a.P.OrgID, bus.EventCommandUpdate, map[string]any{
			"command_id": cid, "device_id": id, "status": "canceled"})
	}
	switch op {
	case OpRevoke:
		s.bus.PublishJSON(bus.DeviceControlSubject(id), bus.ControlMsg{Type: bus.ControlRevoke})
	case OpDisable:
		s.bus.PublishJSON(bus.DeviceControlSubject(id), bus.ControlMsg{Type: bus.ControlDisable})
	}
	s.bus.PublishEvent(a.P.OrgID, bus.EventDeviceStatus, StatusEvent{
		DeviceID: id, Status: out.Status, Connectivity: out.Connectivity, LastSeenAt: out.LastSeenAt})
	return out, nil
}
