// Package bus is the NATS-backed internal message bus.
//
// NATS carries routing and notifications only. PostgreSQL remains the source
// of truth, so a lost message degrades latency, never correctness.
package bus

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// Subjects.
func DeviceCommandSubject(deviceID uuid.UUID) string { return "am.dev." + deviceID.String() + ".cmd" }
func DeviceCancelSubject(deviceID uuid.UUID) string  { return "am.dev." + deviceID.String() + ".cancel" }
func DeviceControlSubject(deviceID uuid.UUID) string { return "am.dev." + deviceID.String() + ".ctl" }
func DeviceWildcard(deviceID uuid.UUID) string       { return "am.dev." + deviceID.String() + ".>" }
func EventSubject(orgID uuid.UUID, kind string) string {
	return "am.evt." + orgID.String() + "." + kind
}

const EventWildcard = "am.evt.>"

// Event kinds.
const (
	EventDeviceStatus  = "device.status"
	EventDeviceUpdated = "device.updated"
	EventCommandUpdate = "command.updated"
	EventCommandOutput = "command.output"
)

// Control message types sent on DeviceControlSubject.
const (
	ControlKick    = "kick"
	ControlRevoke  = "revoke"
	ControlDisable = "disable"
)

// CommandMsg announces a queued command.
type CommandMsg struct {
	CommandID uuid.UUID `json:"command_id"`
}

// ControlMsg asks the gateway holding a device connection to act on it.
type ControlMsg struct {
	Type      string    `json:"type"`
	SessionID uuid.UUID `json:"session_id,omitempty"` // for kick: the session that should survive
	// For session_open: the signed SessionSpec to forward to the agent.
	Spec      []byte `json:"spec,omitempty"`
	Signature []byte `json:"signature,omitempty"`
	KeyID     string `json:"key_id,omitempty"`
}

// ControlSessionOpen forwards a signed relay-session grant to the agent.
const ControlSessionOpen = "session_open"

// RelayKillSubject terminates a relayed session wherever it is paired.
func RelayKillSubject(sessionID uuid.UUID) string { return "am.relay." + sessionID.String() + ".kill" }

// EventSessionUpdate is published when a relayed session changes state.
const EventSessionUpdate = "session.updated"

// Event is fanned out to dashboards over SSE.
type Event struct {
	Kind  string          `json:"kind"`
	OrgID uuid.UUID       `json:"org_id"`
	Data  json.RawMessage `json:"data"`
}

// Bus wraps a NATS connection.
type Bus struct {
	nc  *nats.Conn
	log *slog.Logger
}

// Connect dials NATS with reconnect enabled.
func Connect(url, name string, log *slog.Logger) (*Bus, error) {
	nc, err := nats.Connect(url,
		nats.Name(name),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.RetryOnFailedConnect(true),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Warn("nats disconnected", "err", err)
			}
		}),
		nats.ReconnectHandler(func(c *nats.Conn) { log.Info("nats reconnected", "url", c.ConnectedUrl()) }),
	)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	return &Bus{nc: nc, log: log}, nil
}

// Conn exposes the raw connection.
func (b *Bus) Conn() *nats.Conn { return b.nc }

// Connected reports connection health for readiness probes.
func (b *Bus) Connected() bool { return b.nc.IsConnected() }

// Close drains and closes the connection.
func (b *Bus) Close() { _ = b.nc.Drain() }

// PublishJSON publishes v as JSON. Failures are logged, not returned: the
// database already holds the state, and consumers reconcile from it.
func (b *Bus) PublishJSON(subject string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		b.log.Error("bus marshal", "subject", subject, "err", err)
		return
	}
	if err := b.nc.Publish(subject, data); err != nil {
		b.log.Warn("bus publish failed", "subject", subject, "err", err)
	}
}

// PublishEvent publishes a dashboard event for an org.
func (b *Bus) PublishEvent(orgID uuid.UUID, kind string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		b.log.Error("bus marshal event", "kind", kind, "err", err)
		return
	}
	b.PublishJSON(EventSubject(orgID, kind), Event{Kind: kind, OrgID: orgID, Data: raw})
}
