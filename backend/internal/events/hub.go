// Package events fans bus events out to connected dashboard clients (SSE).
package events

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/enfec/agentmesh/backend/internal/platform/bus"
)

// Client is one SSE subscriber.
type Client struct {
	OrgID uuid.UUID
	C     chan bus.Event
	// Closed is closed when the hub drops the client (slow consumer).
	Closed chan struct{}
	once   sync.Once
}

func (c *Client) close() { c.once.Do(func() { close(c.Closed) }) }

// Hub holds subscribers of this API replica.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
	sub     *nats.Subscription
	log     *slog.Logger
}

// NewHub subscribes to all org events on the bus.
func NewHub(b *bus.Bus, log *slog.Logger) (*Hub, error) {
	h := &Hub{clients: make(map[*Client]struct{}), log: log}
	sub, err := b.Conn().Subscribe(bus.EventWildcard, h.onMessage)
	if err != nil {
		return nil, err
	}
	h.sub = sub
	return h, nil
}

func (h *Hub) onMessage(m *nats.Msg) {
	var ev bus.Event
	if err := json.Unmarshal(m.Data, &ev); err != nil {
		h.log.Warn("bad event on bus", "subject", m.Subject, "err", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.OrgID != ev.OrgID {
			continue
		}
		select {
		case c.C <- ev:
		default:
			// Slow consumer: drop it rather than block the bus. The client
			// reconnects and refetches state.
			c.close()
		}
	}
}

// Subscribe registers a client for an org.
func (h *Hub) Subscribe(orgID uuid.UUID) *Client {
	c := &Client{OrgID: orgID, C: make(chan bus.Event, 256), Closed: make(chan struct{})}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

// Unsubscribe removes a client.
func (h *Hub) Unsubscribe(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.close()
}

// Count returns the number of connected clients.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close stops receiving events.
func (h *Hub) Close() {
	if h.sub != nil {
		_ = h.sub.Unsubscribe()
	}
}
