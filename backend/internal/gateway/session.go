package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/commands"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/platform/textutil"
	"github.com/enfec/agentmesh/protocols/agentapi"
	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

const (
	helloTimeout    = 10 * time.Second
	writeTimeout    = 10 * time.Second
	pingInterval    = 20 * time.Second
	offlineGrace    = 15 * time.Second
	outboundBuffer  = 128
	maxLiveOutput   = 32 << 10
	maxCapabilities = 64
)

var capabilityRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

var errDeviceInactive = errors.New("device is not active")

type outMsg struct {
	data        []byte
	closeCode   websocket.StatusCode
	closeReason string
}

// session is one live agent connection.
type session struct {
	id         uuid.UUID
	deviceID   uuid.UUID
	orgID      uuid.UUID
	deviceName string
	ip         string
	g          *Gateway
	conn       *websocket.Conn
	log        *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	out    chan outMsg

	tokenExp   atomic.Int64 // unix nanos
	closeOnce  sync.Once
	reasonMu   sync.Mutex
	reason     string
	limiter    *rate.Limiter
	lastSample time.Time

	ownedMu sync.Mutex
	owned   map[uuid.UUID]bool
}

func (g *Gateway) handleConnect(w http.ResponseWriter, r *http.Request) error {
	ip := httpx.ClientIP(r.Context())
	if g.registry.draining.Load() {
		metricRejected.WithLabelValues("draining").Inc()
		return &httpx.Error{Status: http.StatusServiceUnavailable, Code: "draining", Message: "gateway is draining", RetryAfter: 5}
	}
	if ok, wait := g.connectRL.Allow(ip); !ok {
		metricRejected.WithLabelValues("rate_limited").Inc()
		return httpx.RateLimited(wait)
	}
	v, err := g.tokens.Verify(bearer(r))
	if errors.Is(err, auth.ErrTokenExpired) {
		return httpx.WithCode(http.StatusUnauthorized, "token_expired", "device token expired")
	}
	if err != nil {
		metricRejected.WithLabelValues("unauthenticated").Inc()
		return httpx.Unauthorized("invalid device token")
	}
	if int(g.registry.count.Load()) >= g.opts.MaxConnections {
		metricRejected.WithLabelValues("capacity").Inc()
		return &httpx.Error{Status: http.StatusServiceUnavailable, Code: "capacity", Message: "gateway at capacity", RetryAfter: 30}
	}
	var status, name string
	err = g.pool.QueryRow(r.Context(), `SELECT status, name FROM devices WHERE id = $1 AND org_id = $2`, v.Subject, v.OrgID).Scan(&status, &name)
	if db.IsNoRows(err) {
		return httpx.Unauthorized("unknown device")
	}
	if err != nil {
		return err
	}
	switch status {
	case devices.StatusRevoked:
		return httpx.WithCode(http.StatusUnauthorized, agentapi.CodeDeviceRevoked, "device has been revoked")
	case devices.StatusDisabled:
		return httpx.WithCode(http.StatusForbidden, agentapi.CodeDeviceDisabled, "device is disabled")
	case devices.StatusPending:
		return httpx.WithCode(http.StatusForbidden, agentapi.CodeDevicePending, "device is awaiting approval")
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:    []string{agentapi.Subprotocol},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil // Accept already wrote the HTTP error
	}
	if conn.Subprotocol() != agentapi.Subprotocol {
		_ = conn.Close(websocket.StatusPolicyViolation, "subprotocol "+agentapi.Subprotocol+" required")
		return nil
	}
	conn.SetReadLimit(agentapi.MaxMessageBytes)

	ctx, cancel := context.WithCancel(context.Background())
	s := &session{
		id: uuid.Must(uuid.NewV7()), deviceID: v.Subject, orgID: v.OrgID, deviceName: name, ip: ip,
		g: g, conn: conn, ctx: ctx, cancel: cancel, out: make(chan outMsg, outboundBuffer),
		limiter: rate.NewLimiter(50, 200), owned: make(map[uuid.UUID]bool),
	}
	s.log = g.log.With("device_id", s.deviceID, "session_id", s.id)
	s.tokenExp.Store(v.ExpiresAt.UnixNano())

	g.registry.wg.Add(1)
	defer g.registry.wg.Done()
	s.run()
	return nil
}

func (s *session) setReason(r string) {
	s.reasonMu.Lock()
	if s.reason == "" {
		s.reason = r
	}
	s.reasonMu.Unlock()
}

func (s *session) run() {
	defer s.conn.CloseNow()
	defer s.cancel()

	hctx, hcancel := context.WithTimeout(s.ctx, helloTimeout)
	env, err := s.read(hctx)
	hcancel()
	if err != nil {
		s.log.Debug("no hello", "err", err)
		return
	}
	hello := env.GetHello()
	if hello == nil {
		_ = s.conn.Close(websocket.StatusPolicyViolation, "expected Hello")
		return
	}
	if !supports(hello.GetProtocolVersions(), agentapi.ProtocolVersion) {
		s.writeNow(disconnectEnv(agentv1.Disconnect_REASON_PROTOCOL_ERROR, "unsupported protocol version", 3600))
		_ = s.conn.Close(websocket.StatusPolicyViolation, "unsupported protocol version")
		return
	}

	superseded, err := s.register(hello)
	if err != nil {
		retry := uint32(5)
		if errors.Is(err, errDeviceInactive) {
			retry = 60
		} else {
			s.log.Error("register session", "err", err)
		}
		s.writeNow(disconnectEnv(agentv1.Disconnect_REASON_RECONNECT, "registration failed", retry))
		_ = s.conn.Close(websocket.StatusTryAgainLater, "registration failed")
		return
	}

	sub, err := s.g.bus.Conn().Subscribe(bus.DeviceWildcard(s.deviceID), s.onBus)
	if err != nil {
		s.log.Error("subscribe", "err", err)
		s.setReason("bus_error")
		s.finish()
		return
	}
	defer func() { _ = sub.Unsubscribe() }()

	s.g.registry.add(s)
	defer s.g.registry.remove(s)
	if superseded {
		s.g.bus.PublishJSON(bus.DeviceControlSubject(s.deviceID), bus.ControlMsg{Type: bus.ControlKick, SessionID: s.id})
	}
	now := time.Now()
	s.g.bus.PublishEvent(s.orgID, bus.EventDeviceStatus, devices.StatusEvent{
		DeviceID: s.deviceID, Status: devices.StatusActive, Connectivity: devices.Online, LastSeenAt: &now})
	s.log.Info("agent connected", "agent_version", hello.GetAgentVersion(), "platform", hello.GetPlatform(), "ip", s.ip)

	go s.writeLoop()
	go s.keepalive()
	s.send(&agentv1.Envelope{Body: &agentv1.Envelope_Welcome{Welcome: &agentv1.Welcome{
		SessionId: s.id.String(), ServerTime: timestamppb.Now(),
		HeartbeatIntervalS: uint32(s.g.opts.HeartbeatInterval / time.Second),
		NegotiatedVersion:  agentapi.ProtocolVersion, DeviceId: s.deviceID.String(),
	}}})
	go s.deliverPending()

	for {
		env, err := s.read(s.ctx)
		if err != nil {
			switch websocket.CloseStatus(err) {
			case websocket.StatusNormalClosure, websocket.StatusGoingAway:
				s.setReason("agent_closed")
			case -1:
				s.setReason("connection_lost")
			default:
				s.setReason("closed_" + websocket.CloseStatus(err).String())
			}
			break
		}
		if !s.limiter.Allow() {
			s.setReason("rate_limited")
			s.disconnect(agentv1.Disconnect_REASON_RATE_LIMITED, "message rate exceeded", 30)
			continue
		}
		s.handle(env)
	}
	s.cancel()
	s.finish()
	s.log.Info("agent disconnected", "reason", s.reason)
}

func supports(versions []uint32, v uint32) bool {
	for _, x := range versions {
		if x == v {
			return true
		}
	}
	return false
}

func (s *session) read(ctx context.Context) (*agentv1.Envelope, error) {
	typ, data, err := s.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageBinary {
		return nil, errors.New("text frames are not allowed")
	}
	var env agentv1.Envelope
	if err := proto.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

func stamp(env *agentv1.Envelope) {
	env.V = agentapi.ProtocolVersion
	if env.Id == "" {
		env.Id = uuid.Must(uuid.NewV7()).String()
	}
	env.Ts = timestamppb.Now()
}

// send queues env for the writer. A full queue means the agent is not
// draining its socket; the connection is dropped rather than buffered forever.
func (s *session) send(env *agentv1.Envelope) bool {
	stamp(env)
	data, err := proto.Marshal(env)
	if err != nil {
		s.log.Error("marshal", "err", err)
		return false
	}
	select {
	case s.out <- outMsg{data: data}:
		return true
	case <-s.ctx.Done():
		return false
	default:
		s.log.Warn("outbound queue full; dropping connection")
		s.setReason("slow_consumer")
		s.cancel()
		return false
	}
}

// writeNow writes synchronously; used before the writer loop starts.
func (s *session) writeNow(env *agentv1.Envelope) {
	stamp(env)
	data, err := proto.Marshal(env)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, writeTimeout)
	defer cancel()
	_ = s.conn.Write(ctx, websocket.MessageBinary, data)
}

func (s *session) writeLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case m := <-s.out:
			ctx, cancel := context.WithTimeout(s.ctx, writeTimeout)
			err := s.conn.Write(ctx, websocket.MessageBinary, m.data)
			cancel()
			if err != nil {
				s.setReason("write_failed")
				s.cancel()
				return
			}
			metricMessagesOut.Inc()
			if m.closeCode != 0 {
				_ = s.conn.Close(m.closeCode, m.closeReason)
				s.cancel()
				return
			}
		}
	}
}

func disconnectEnv(reason agentv1.Disconnect_Reason, msg string, retryAfter uint32) *agentv1.Envelope {
	return &agentv1.Envelope{Body: &agentv1.Envelope_Disconnect{Disconnect: &agentv1.Disconnect{
		Reason: reason, Message: msg, RetryAfterS: retryAfter}}}
}

// disconnect tells the agent why it is being dropped, then closes.
func (s *session) disconnect(reason agentv1.Disconnect_Reason, msg string, retryAfter uint32) {
	s.closeOnce.Do(func() {
		s.setReason(strings.ToLower(strings.TrimPrefix(reason.String(), "REASON_")))
		env := disconnectEnv(reason, msg, retryAfter)
		stamp(env)
		data, _ := proto.Marshal(env)
		select {
		case s.out <- outMsg{data: data, closeCode: websocket.StatusNormalClosure, closeReason: msg}:
		default:
			s.cancel()
		}
	})
}

func (s *session) keepalive() {
	ping := time.NewTicker(pingInterval)
	check := time.NewTicker(5 * time.Second)
	defer ping.Stop()
	defer check.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ping.C:
			ctx, cancel := context.WithTimeout(s.ctx, writeTimeout)
			err := s.conn.Ping(ctx)
			cancel()
			if err != nil && s.ctx.Err() == nil {
				s.setReason("ping_timeout")
				s.cancel()
				return
			}
		case <-check.C:
			if time.Now().UnixNano() > s.tokenExp.Load() {
				s.disconnect(agentv1.Disconnect_REASON_TOKEN_EXPIRED, "device token expired; re-authenticate", 0)
			}
		}
	}
}

func sanitizeCapabilities(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, c := range in {
		c = strings.ToLower(strings.TrimSpace(c))
		if capabilityRe.MatchString(c) && !seen[c] && len(out) < maxCapabilities {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// register records the new session, superseding any older live session.
func (s *session) register(h *agentv1.Hello) (superseded bool, err error) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	tx, err := s.g.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE device_sessions SET disconnected_at = now(), disconnect_reason = 'superseded'
		WHERE device_id = $1 AND disconnected_at IS NULL`, s.deviceID)
	if err != nil {
		return false, err
	}
	superseded = tag.RowsAffected() > 0
	if _, err := tx.Exec(ctx, `
		INSERT INTO device_sessions (id, device_id, gateway_id, remote_ip, agent_version, protocol_version)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		s.id, s.deviceID, s.g.opts.GatewayID, s.ip, textutil.Clean(h.GetAgentVersion(), 64), agentapi.ProtocolVersion); err != nil {
		return false, err
	}
	tag, err = tx.Exec(ctx, `
		UPDATE devices SET connectivity = 'online', agent_version = $2,
		       platform = coalesce(nullif($3, ''), platform), arch = coalesce(nullif($4, ''), arch),
		       capabilities = $5, hostname = coalesce(nullif($6, ''), hostname),
		       last_seen_at = now(), last_ip = $7, updated_at = now()
		WHERE id = $1 AND status = 'active'`,
		s.deviceID, textutil.Clean(h.GetAgentVersion(), 64), strings.ToLower(textutil.Clean(h.GetPlatform(), 32)),
		strings.ToLower(textutil.Clean(h.GetArch(), 32)), sanitizeCapabilities(h.GetCapabilities()),
		textutil.Clean(h.GetHostname(), 255), s.ip)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, errDeviceInactive
	}
	return superseded, tx.Commit(ctx)
}

// finish marks the session closed and schedules the offline transition.
func (s *session) finish() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reason := s.reason
	if reason == "" {
		reason = "closed"
	}
	if _, err := s.g.pool.Exec(ctx, `
		UPDATE device_sessions SET disconnected_at = now(), disconnect_reason = $2, last_seen_at = now()
		WHERE id = $1 AND disconnected_at IS NULL`, s.id, reason); err != nil {
		s.log.Error("close session", "err", err)
	}
	if _, err := s.g.pool.Exec(ctx, `UPDATE devices SET last_seen_at = now() WHERE id = $1`, s.deviceID); err != nil {
		s.log.Error("update last seen", "err", err)
	}
	deviceID, orgID, g := s.deviceID, s.orgID, s.g
	// Short grace period so a quick reconnect (possibly to another gateway)
	// does not flap the dashboard. The worker's reaper covers crashed gateways.
	time.AfterFunc(offlineGrace, func() { g.markOfflineIfIdle(deviceID, orgID) })
}

func (g *Gateway) markOfflineIfIdle(deviceID, orgID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var status string
	var lastSeen *time.Time
	err := g.pool.QueryRow(ctx, `
		UPDATE devices d SET connectivity = 'offline', updated_at = now()
		WHERE d.id = $1 AND d.connectivity = 'online'
		  AND NOT EXISTS (SELECT 1 FROM device_sessions s WHERE s.device_id = d.id AND s.disconnected_at IS NULL)
		RETURNING d.status, d.last_seen_at`, deviceID).Scan(&status, &lastSeen)
	if db.IsNoRows(err) {
		return
	}
	if err != nil {
		g.log.Error("mark offline", "err", err, "device_id", deviceID)
		return
	}
	g.bus.PublishEvent(orgID, bus.EventDeviceStatus, devices.StatusEvent{
		DeviceID: deviceID, Status: status, Connectivity: devices.Offline, LastSeenAt: lastSeen})
}

// ---------------------------------------------------------------- inbound

func (s *session) handle(env *agentv1.Envelope) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	switch b := env.Body.(type) {
	case *agentv1.Envelope_Heartbeat:
		metricMessagesIn.WithLabelValues("heartbeat").Inc()
		s.g.presence.touch(s.id, s.deviceID, s.ip)
		s.sampleHeartbeat(ctx, b.Heartbeat)
	case *agentv1.Envelope_Inventory:
		metricMessagesIn.WithLabelValues("inventory").Inc()
		s.g.presence.touch(s.id, s.deviceID, s.ip)
		if err := s.storeInventory(ctx, b.Inventory); err != nil {
			s.log.Error("store inventory", "err", err)
		}
	case *agentv1.Envelope_CommandAck:
		metricMessagesIn.WithLabelValues("command_ack").Inc()
		if id, err := uuid.Parse(b.CommandAck.GetCommandId()); err == nil {
			if err := s.g.commands.MarkRunning(ctx, s.orgID, s.deviceID, id); err != nil {
				s.log.Error("mark running", "err", err)
			}
		}
	case *agentv1.Envelope_CommandOutput:
		metricMessagesIn.WithLabelValues("command_output").Inc()
		s.forwardOutput(ctx, b.CommandOutput)
	case *agentv1.Envelope_CommandResult:
		metricMessagesIn.WithLabelValues("command_result").Inc()
		err := s.g.commands.StoreResult(ctx, commands.DeviceRef{ID: s.deviceID, OrgID: s.orgID, Name: s.deviceName, IP: s.ip}, b.CommandResult)
		if err != nil {
			s.log.Error("store result", "err", err)
		}
	case *agentv1.Envelope_TokenRefresh:
		metricMessagesIn.WithLabelValues("token_refresh").Inc()
		v, err := s.g.tokens.Verify(b.TokenRefresh.GetAccessToken())
		if err != nil || v.Subject != s.deviceID {
			s.log.Warn("invalid token refresh", "err", err)
			return
		}
		s.tokenExp.Store(v.ExpiresAt.UnixNano())
	default:
		metricMessagesIn.WithLabelValues("other").Inc()
	}
}

func clampInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

func (s *session) sampleHeartbeat(ctx context.Context, hb *agentv1.Heartbeat) {
	if time.Since(s.lastSample) < heartbeatSampleEvery {
		return
	}
	s.lastSample = time.Now()
	cpu := hb.GetCpuPercent()
	if math.IsNaN(cpu) || cpu < 0 || cpu > 100 {
		cpu = 0
	}
	load := hb.GetLoad1()
	if math.IsNaN(load) || math.IsInf(load, 0) || load < 0 {
		load = 0
	}
	if _, err := s.g.pool.Exec(ctx, `
		INSERT INTO device_heartbeats (device_id, ts, cpu_percent, mem_used_bytes, mem_total_bytes, load1, uptime_s)
		VALUES ($1, now(), $2, $3, $4, $5, $6)`,
		s.deviceID, cpu, clampInt64(hb.GetMemUsedBytes()), clampInt64(hb.GetMemTotalBytes()), load, clampInt64(hb.GetUptimeS())); err != nil {
		s.log.Warn("heartbeat sample", "err", err)
	}
}

func (s *session) storeInventory(ctx context.Context, in *agentv1.Inventory) error {
	var inv devices.Inventory
	if c := in.GetCpu(); c != nil {
		inv.CPU.Model = textutil.Clean(c.GetModel(), 200)
		inv.CPU.Cores = c.GetCores()
		inv.CPU.Threads = c.GetThreads()
	}
	if m := in.GetMemory(); m != nil {
		inv.Memory.TotalBytes = m.GetTotalBytes()
		inv.Memory.UsedBytes = m.GetUsedBytes()
	}
	inv.Disks = []devices.Disk{}
	for i, d := range in.GetDisks() {
		if i >= 64 {
			break
		}
		inv.Disks = append(inv.Disks, devices.Disk{Mount: textutil.Clean(d.GetMount(), 255), FSType: textutil.Clean(d.GetFstype(), 32),
			TotalBytes: d.GetTotalBytes(), UsedBytes: d.GetUsedBytes()})
	}
	inv.Network = []devices.NetInterface{}
	for i, n := range in.GetNetwork() {
		if i >= 64 {
			break
		}
		ni := devices.NetInterface{Name: textutil.Clean(n.GetName(), 100), MAC: textutil.Clean(n.GetMac(), 64), Addrs: []string{}}
		for j, a := range n.GetAddrs() {
			if j >= 32 {
				break
			}
			ni.Addrs = append(ni.Addrs, textutil.Clean(a, 64))
		}
		inv.Network = append(inv.Network, ni)
	}
	inv.UptimeS = in.GetUptimeS()
	if bt := in.GetBootTime(); bt != nil && bt.IsValid() && bt.AsTime().Year() > 1990 {
		t := bt.AsTime()
		inv.BootTime = &t
	}
	inv.CollectedAt = time.Now().UTC()
	raw, err := json.Marshal(inv)
	if err != nil {
		return err
	}
	if _, err := s.g.pool.Exec(ctx, `
		UPDATE devices SET hostname = coalesce(nullif($2, ''), hostname), os_name = $3, os_version = $4, os_build = $5,
		       kernel_version = $6, inventory = $7, machine_id_hash = coalesce(nullif($8, ''), machine_id_hash), updated_at = now()
		WHERE id = $1`,
		s.deviceID, textutil.Clean(in.GetHostname(), 255), textutil.Clean(in.GetOsName(), 100), textutil.Clean(in.GetOsVersion(), 100),
		textutil.Clean(in.GetOsBuild(), 100), textutil.Clean(in.GetKernelVersion(), 100), raw, textutil.Clean(in.GetMachineIdHash(), 64)); err != nil {
		return err
	}
	s.g.bus.PublishEvent(s.orgID, bus.EventDeviceUpdated, devices.UpdatedEvent{DeviceID: s.deviceID})
	return nil
}

// ownsCommand verifies a command belongs to this device before forwarding
// its output, so one device cannot inject output into another's command.
func (s *session) ownsCommand(ctx context.Context, id uuid.UUID) bool {
	s.ownedMu.Lock()
	ok, known := s.owned[id]
	s.ownedMu.Unlock()
	if known {
		return ok
	}
	var exists bool
	err := s.g.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM device_commands WHERE id = $1 AND device_id = $2)`, id, s.deviceID).Scan(&exists)
	if err != nil {
		return false
	}
	s.ownedMu.Lock()
	if len(s.owned) > 1000 {
		s.owned = make(map[uuid.UUID]bool)
	}
	s.owned[id] = exists
	s.ownedMu.Unlock()
	return exists
}

func (s *session) forwardOutput(ctx context.Context, o *agentv1.CommandOutput) {
	id, err := uuid.Parse(o.GetCommandId())
	if err != nil || !s.ownsCommand(ctx, id) {
		return
	}
	data := o.GetData()
	if len(data) > maxLiveOutput {
		data = data[:maxLiveOutput]
	}
	stream := "stdout"
	if o.GetStream() == agentv1.Stream_STREAM_STDERR {
		stream = "stderr"
	}
	s.g.bus.PublishEvent(s.orgID, bus.EventCommandOutput, map[string]any{
		"command_id": id, "device_id": s.deviceID, "stream": stream, "seq": o.GetSeq(),
		"data": strings.ToValidUTF8(string(data), "�"),
	})
}

// ---------------------------------------------------------------- bus → agent

func (s *session) onBus(m *nats.Msg) {
	switch {
	case strings.HasSuffix(m.Subject, ".cmd"):
		var msg bus.CommandMsg
		if json.Unmarshal(m.Data, &msg) == nil {
			s.deliver(msg.CommandID)
		}
	case strings.HasSuffix(m.Subject, ".cancel"):
		var msg bus.CommandMsg
		if json.Unmarshal(m.Data, &msg) == nil {
			s.send(&agentv1.Envelope{Body: &agentv1.Envelope_CommandCancel{CommandCancel: &agentv1.CommandCancel{CommandId: msg.CommandID.String()}}})
		}
	case strings.HasSuffix(m.Subject, ".ctl"):
		var msg bus.ControlMsg
		if json.Unmarshal(m.Data, &msg) != nil {
			return
		}
		switch msg.Type {
		case bus.ControlKick:
			if msg.SessionID != s.id {
				s.disconnect(agentv1.Disconnect_REASON_SUPERSEDED, "a newer connection for this device exists", 0)
			}
		case bus.ControlRevoke:
			s.disconnect(agentv1.Disconnect_REASON_REVOKED, "device revoked by administrator", 0)
		case bus.ControlDisable:
			s.disconnect(agentv1.Disconnect_REASON_DISABLED, "device disabled by administrator", 60)
		case bus.ControlSessionOpen:
			// Signed by control-api; the agent verifies it before joining.
			s.send(&agentv1.Envelope{Body: &agentv1.Envelope_SessionOpen{SessionOpen: &agentv1.SessionOpen{
				Spec: msg.Spec, Signature: msg.Signature, KeyId: msg.KeyID}}})
		}
	}
}

func (s *session) deliver(id uuid.UUID) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	d, err := s.g.commands.Claim(ctx, s.deviceID, id)
	if err != nil {
		s.log.Error("claim command", "err", err, "command_id", id)
		return
	}
	if d == nil {
		return
	}
	s.ownedMu.Lock()
	s.owned[id] = true
	s.ownedMu.Unlock()
	s.send(&agentv1.Envelope{Body: &agentv1.Envelope_CommandRequest{CommandRequest: &agentv1.CommandRequest{
		Spec: d.Spec, Signature: d.Signature, KeyId: d.KeyID}}})
}

func (s *session) deliverPending() {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	ids, err := s.g.commands.PendingIDs(ctx, s.deviceID)
	cancel()
	if err != nil {
		s.log.Error("pending commands", "err", err)
		return
	}
	for _, id := range ids {
		s.deliver(id)
	}
}
