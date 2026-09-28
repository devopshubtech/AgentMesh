package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/sessions"
	"github.com/enfec/agentmesh/protocols/agentapi"
)

// The relay pairs the two sides of a session (operator client and agent)
// and pipes bytes between them. It does not interpret the stream: the
// multiplexing (yamux + exitproto) runs end to end between the peers.
//
// MVP limitation: both sides must land on the same gateway replica. That
// holds for a single gateway; with several replicas the load balancer must
// route /v1/relay by session (or a dedicated session-relay tier is used, as
// planned in architecture §10).

const relayReadLimit = 4 << 20

var (
	metricRelaySessions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "agentmesh_relay_active_sessions", Help: "Active relayed sessions."})
	metricRelayBytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentmesh_relay_bytes_total", Help: "Bytes relayed."}, []string{"direction"})
)

type relayMeta struct {
	sessionID, orgID, deviceID, userID uuid.UUID
	joinDeadline, maxEndsAt            time.Time
}

type relayPair struct {
	meta  relayMeta
	sides map[string]*websocket.Conn
	ready chan struct{} // closed when both sides joined
	done  chan struct{} // closed when piping ended
}

type relayHub struct {
	mu    sync.Mutex
	pairs map[uuid.UUID]*relayPair
}

func newRelayHub() *relayHub { return &relayHub{pairs: map[uuid.UUID]*relayPair{}} }

// join registers one side; complete is true when this call paired the session.
func (h *relayHub) join(m relayMeta, side string, c *websocket.Conn) (p *relayPair, complete bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p = h.pairs[m.sessionID]
	if p == nil {
		p = &relayPair{meta: m, sides: map[string]*websocket.Conn{}, ready: make(chan struct{}), done: make(chan struct{})}
		h.pairs[m.sessionID] = p
	}
	if _, dup := p.sides[side]; dup {
		return nil, false, errors.New("side already joined")
	}
	p.sides[side] = c
	if len(p.sides) == 2 {
		close(p.ready)
		return p, true, nil
	}
	return p, false, nil
}

func (h *relayHub) remove(id uuid.UUID) {
	h.mu.Lock()
	delete(h.pairs, id)
	h.mu.Unlock()
}

func (g *Gateway) handleRelay(w http.ResponseWriter, r *http.Request) error {
	ip := httpx.ClientIP(r.Context())
	if ok, wait := g.connectRL.Allow(ip); !ok {
		return httpx.RateLimited(wait)
	}
	ticket := r.Header.Get(agentapi.RelayTicketHeader)
	if len(ticket) < 16 || len(ticket) > 128 {
		return httpx.Unauthorized("missing relay ticket")
	}
	var (
		m    relayMeta
		side string
	)
	err := g.pool.QueryRow(r.Context(), `
		WITH t AS (DELETE FROM relay_tickets WHERE ticket_hash = $1 AND expires_at > now() RETURNING session_id, side)
		SELECT s.id, s.org_id, s.device_id, s.user_id, s.join_deadline, s.max_ends_at, t.side
		FROM t JOIN remote_sessions s ON s.id = t.session_id
		JOIN devices d ON d.id = s.device_id
		WHERE s.status = 'pending' AND d.status = 'active'`, sessions.HashTicket(ticket)).
		Scan(&m.sessionID, &m.orgID, &m.deviceID, &m.userID, &m.joinDeadline, &m.maxEndsAt, &side)
	if db.IsNoRows(err) {
		return httpx.Unauthorized("relay ticket is invalid, used or expired")
	}
	if err != nil {
		return err
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{agentapi.RelaySubprotocol}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil
	}
	if conn.Subprotocol() != agentapi.RelaySubprotocol {
		_ = conn.Close(websocket.StatusPolicyViolation, "relay subprotocol required")
		return nil
	}
	conn.SetReadLimit(relayReadLimit)
	log := g.log.With("relay_session", m.sessionID, "side", side, "ip", ip)

	p, complete, err := g.relays.join(m, side, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return nil
	}
	if !complete {
		log.Info("relay side waiting for peer")
		t := time.NewTimer(time.Until(m.joinDeadline) + 5*time.Second)
		defer t.Stop()
		select {
		case <-p.ready:
			<-p.done // the pairing side pipes; keep this handler alive until it ends
		case <-t.C:
			g.relays.remove(m.sessionID)
			_ = conn.Close(websocket.StatusTryAgainLater, "peer did not join")
			g.endSession(m, "join_timeout", 0, 0, time.Time{})
		}
		return nil
	}
	log.Info("relay paired")
	g.pipe(p)
	return nil
}

type countingWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

func (g *Gateway) pipe(p *relayPair) {
	defer close(p.done)
	defer g.relays.remove(p.meta.sessionID)
	m := p.meta
	client, agent := p.sides["client"], p.sides["agent"]

	tag, err := g.pool.Exec(context.Background(), `
		UPDATE remote_sessions SET status = 'active', started_at = now(), relay_id = $2 WHERE id = $1 AND status = 'pending'`,
		m.sessionID, g.opts.GatewayID)
	if err != nil || tag.RowsAffected() == 0 {
		_ = client.Close(websocket.StatusPolicyViolation, "session no longer valid")
		_ = agent.Close(websocket.StatusPolicyViolation, "session no longer valid")
		return
	}
	started := time.Now()
	metricRelaySessions.Inc()
	defer metricRelaySessions.Dec()
	g.bus.PublishEvent(m.orgID, bus.EventSessionUpdate, map[string]any{"session_id": m.sessionID, "device_id": m.deviceID, "status": sessions.StatusActive})

	ctx, cancel := context.WithDeadline(context.Background(), m.maxEndsAt)
	defer cancel()
	var reason atomic.Value
	stop := func(r string) {
		reason.CompareAndSwap(nil, r)
		cancel()
	}
	kill, _ := g.bus.Conn().Subscribe(bus.RelayKillSubject(m.sessionID), func(*nats.Msg) { stop("terminated") })
	ctl, _ := g.bus.Conn().Subscribe(bus.DeviceControlSubject(m.deviceID), func(msg *nats.Msg) {
		var c bus.ControlMsg
		if json.Unmarshal(msg.Data, &c) == nil && (c.Type == bus.ControlRevoke || c.Type == bus.ControlDisable) {
			stop("device_" + c.Type)
		}
	})
	defer func() {
		if kill != nil {
			_ = kill.Unsubscribe()
		}
		if ctl != nil {
			_ = ctl.Unsubscribe()
		}
	}()

	cn := websocket.NetConn(ctx, client, websocket.MessageBinary)
	an := websocket.NetConn(ctx, agent, websocket.MessageBinary)
	var up, down atomic.Int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(countingWriter{an, &up}, cn)
		stop("client_closed")
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(countingWriter{cn, &down}, an)
		stop("agent_closed")
	}()
	<-ctx.Done()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		reason.CompareAndSwap(nil, "max_duration")
	}
	closeConn(cn)
	closeConn(an)
	wg.Wait()
	metricRelayBytes.WithLabelValues("up").Add(float64(up.Load()))
	metricRelayBytes.WithLabelValues("down").Add(float64(down.Load()))
	r, _ := reason.Load().(string)
	g.log.Info("relay ended", "relay_session", m.sessionID, "reason", r, "bytes_up", up.Load(), "bytes_down", down.Load(),
		"duration", time.Since(started).Round(time.Second))
	g.endSession(m, r, up.Load(), down.Load(), started)
}

func closeConn(c net.Conn) { _ = c.Close() }

// endSession finalizes the session row and audits it exactly once.
func (g *Gateway) endSession(m relayMeta, reason string, up, down int64, started time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := db.InTx(ctx, g.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE remote_sessions SET status = 'ended', ended_at = now(), end_reason = $2, bytes_up = $3, bytes_down = $4
			WHERE id = $1 AND status <> 'ended'`, m.sessionID, reason, up, down)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		details := map[string]any{"session_id": m.sessionID, "reason": reason, "bytes_up": up, "bytes_down": down}
		if !started.IsZero() {
			details["duration_s"] = int(time.Since(started).Seconds())
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: m.orgID, ActorType: audit.ActorSystem, ActorLabel: "relay/" + g.opts.GatewayID, Action: "session.end",
			TargetType: "device", TargetID: &m.deviceID, Details: details,
		})
	})
	if err != nil {
		g.log.Error("end relay session", "err", err, "relay_session", m.sessionID)
	}
	g.bus.PublishEvent(m.orgID, bus.EventSessionUpdate, map[string]any{"session_id": m.sessionID, "device_id": m.deviceID, "status": sessions.StatusEnded})
}
