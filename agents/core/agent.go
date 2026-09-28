package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/enfec/agentmesh/protocols/agentapi"
	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

const (
	backoffBase       = time.Second
	backoffMax        = 5 * time.Minute
	pendingPollMax    = 15 * time.Second // approval should take effect quickly
	inventoryInterval = time.Hour
	outboxMax         = 500
	stableSession     = time.Minute
)

// Agent is the long-running device agent.
type Agent struct {
	cfg    *Config
	id     *Identity
	log    *slog.Logger
	client *client
	exec   *executor
	verify *verifier
	bootID string

	clockOffset atomic.Int64 // nanoseconds: server - local
	cur         atomic.Pointer[conn]

	outboxMu sync.Mutex
	outbox   []*agentv1.CommandResult
}

// NewAgent loads identity and prepares an agent.
func NewAgent(cfg *Config, log *slog.Logger) (*Agent, error) {
	id, err := LoadIdentity(cfg)
	if err != nil {
		return nil, err
	}
	cl, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	a := &Agent{cfg: cfg, id: id, log: log, client: cl, bootID: uuid.NewString()}
	a.exec = newExecutor(cfg.Policy, a, log)
	a.verify = &verifier{id: id, replay: newReplayGuard(cfg.path(seenFile)), offset: func() time.Duration {
		return time.Duration(a.clockOffset.Load())
	}}
	return a, nil
}

// ---------------------------------------------------------------- emitter

func (a *Agent) live(env *agentv1.Envelope) {
	if c := a.cur.Load(); c != nil {
		c.trySend(env)
	}
}

func (a *Agent) result(r *agentv1.CommandResult) {
	a.outboxMu.Lock()
	if len(a.outbox) >= outboxMax {
		a.outbox = a.outbox[1:]
	}
	a.outbox = append(a.outbox, r)
	a.outboxMu.Unlock()
	if c := a.cur.Load(); c != nil {
		c.kickOutbox()
	}
}

func (a *Agent) sendInventory() {
	if c := a.cur.Load(); c != nil {
		c.trySend(&agentv1.Envelope{Body: &agentv1.Envelope_Inventory{Inventory: collectInventory(c.ctx)}})
	}
}

// popOutbox removes and returns the oldest queued result.
func (a *Agent) popOutbox() *agentv1.CommandResult {
	a.outboxMu.Lock()
	defer a.outboxMu.Unlock()
	if len(a.outbox) == 0 {
		return nil
	}
	r := a.outbox[0]
	a.outbox = a.outbox[1:]
	return r
}

func (a *Agent) unpopOutbox(r *agentv1.CommandResult) {
	a.outboxMu.Lock()
	a.outbox = append([]*agentv1.CommandResult{r}, a.outbox...)
	a.outboxMu.Unlock()
}

// ---------------------------------------------------------------- lifecycle

type backoff struct{ attempt int }

// next returns an exponential delay with full jitter.
func (b *backoff) next() time.Duration {
	b.attempt++
	ceil := backoffBase << min(b.attempt, 12)
	if ceil > backoffMax || ceil <= 0 {
		ceil = backoffMax
	}
	return time.Duration(rand.Int64N(int64(ceil))) + 500*time.Millisecond
}

func (b *backoff) reset() { b.attempt = 0 }

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// sessionEnd describes why a connection ended.
type sessionEnd struct {
	reason     agentv1.Disconnect_Reason
	retryAfter time.Duration
	message    string
}

// Run keeps the agent connected until ctx is canceled.
func (a *Agent) Run(ctx context.Context) error {
	a.log.Info("agent starting", "version", Version, "device_id", a.id.DeviceID, "server", a.cfg.ServerURL,
		"platform", runtime.GOOS, "arch", runtime.GOARCH, "capabilities", a.exec.capabilities())
	var bo backoff
	for ctx.Err() == nil {
		if a.id.Revoked {
			a.log.Error("this device has been revoked; the agent is halted. Re-enroll with 'enroll --force' to register again.")
			<-ctx.Done()
			return nil
		}
		tok, err := a.client.token(ctx, a.id.DeviceID, a.id.Key())
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			wait := bo.next()
			switch {
			case IsCode(err, agentapi.CodeDeviceRevoked):
				a.halt()
				continue
			case IsCode(err, agentapi.CodeDevicePending):
				wait = min(wait, pendingPollMax)
				a.log.Info("device is awaiting approval in the dashboard", "retry_in", wait.Round(time.Second))
			case IsCode(err, agentapi.CodeDeviceDisabled):
				wait = time.Minute
				a.log.Warn("device is disabled by an administrator", "retry_in", wait)
			default:
				var ae *APIError
				if errors.As(err, &ae) && ae.RetryAfter > wait {
					wait = ae.RetryAfter
				}
				a.log.Warn("authentication failed", "err", err, "retry_in", wait.Round(time.Second))
			}
			sleep(ctx, wait)
			continue
		}

		started := time.Now()
		end, err := a.connect(ctx, tok)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) > stableSession {
			bo.reset()
		}
		wait := bo.next()
		if err != nil {
			a.log.Warn("connection ended", "err", err, "retry_in", wait.Round(time.Second))
		}
		if end != nil {
			a.log.Info("server closed connection", "reason", end.reason.String(), "message", end.message)
			switch end.reason {
			case agentv1.Disconnect_REASON_REVOKED:
				a.halt()
				continue
			case agentv1.Disconnect_REASON_TOKEN_EXPIRED:
				wait = 0
			case agentv1.Disconnect_REASON_SUPERSEDED:
				// Another process presented this identity (cloned disk image?).
				a.log.Warn("another agent connected with this device identity; if this machine was cloned, re-enroll it")
				wait = max(wait, 30*time.Second)
			}
			if end.retryAfter > 0 {
				wait = end.retryAfter
			}
		}
		sleep(ctx, wait)
	}
	return nil
}

func (a *Agent) halt() {
	a.id.Revoked = true
	if err := a.id.Save(a.cfg); err != nil {
		a.log.Error("persist revoked state", "err", err)
	}
}

// ---------------------------------------------------------------- connection

type conn struct {
	a      *Agent
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	out    chan []byte
	kick   chan struct{}
	end    atomic.Pointer[sessionEnd]
}

func envelope(env *agentv1.Envelope) ([]byte, error) {
	env.V = agentapi.ProtocolVersion
	env.Id = uuid.NewString()
	env.Ts = timestamppb.Now()
	return proto.Marshal(env)
}

// trySend queues a best-effort message; it is dropped when the queue is full.
func (c *conn) trySend(env *agentv1.Envelope) bool {
	b, err := envelope(env)
	if err != nil {
		return false
	}
	select {
	case c.out <- b:
		return true
	default:
		return false
	}
}

// send queues a message, waiting for room.
func (c *conn) send(env *agentv1.Envelope) bool {
	b, err := envelope(env)
	if err != nil {
		return false
	}
	select {
	case c.out <- b:
		return true
	case <-c.ctx.Done():
		return false
	}
}

func (c *conn) kickOutbox() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

func wsURL(base string) string {
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + agentapi.PathConnect
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + agentapi.PathConnect
	}
	return base + agentapi.PathConnect
}

func (a *Agent) connect(ctx context.Context, tok *agentapi.TokenResponse) (*sessionEnd, error) {
	dctx, dcancel := context.WithTimeout(ctx, 30*time.Second)
	ws, resp, err := websocket.Dial(dctx, wsURL(a.cfg.ServerURL), &websocket.DialOptions{
		HTTPClient:      a.client.http,
		HTTPHeader:      http.Header{"Authorization": {"Bearer " + tok.AccessToken}, "User-Agent": {"agentmesh-agent/" + Version}},
		Subprotocols:    []string{agentapi.Subprotocol},
		CompressionMode: websocket.CompressionDisabled,
	})
	dcancel()
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusServiceUnavailable {
			ra := 30 * time.Second
			return &sessionEnd{reason: agentv1.Disconnect_REASON_RECONNECT, retryAfter: ra, message: "gateway unavailable"}, err
		}
		return nil, fmt.Errorf("dial: %w", err)
	}
	ws.SetReadLimit(agentapi.MaxMessageBytes)
	cctx, cancel := context.WithCancel(ctx)
	c := &conn{a: a, ws: ws, ctx: cctx, cancel: cancel, out: make(chan []byte, 256), kick: make(chan struct{}, 1)}
	defer ws.CloseNow()
	defer cancel()

	// Handshake: Hello → Welcome.
	hello := &agentv1.Envelope{Body: &agentv1.Envelope_Hello{Hello: &agentv1.Hello{
		AgentVersion: Version, ProtocolVersions: []uint32{agentapi.ProtocolVersion}, Platform: runtime.GOOS,
		Arch: runtime.GOARCH, Capabilities: a.exec.capabilities(), BootId: a.bootID, Hostname: hostname(),
	}}}
	hb, _ := envelope(hello)
	if err := ws.Write(cctx, websocket.MessageBinary, hb); err != nil {
		return nil, fmt.Errorf("send hello: %w", err)
	}
	wctx, wcancel := context.WithTimeout(cctx, 15*time.Second)
	env, err := readEnvelope(wctx, ws)
	wcancel()
	if err != nil {
		return nil, fmt.Errorf("await welcome: %w", err)
	}
	if d := env.GetDisconnect(); d != nil {
		return endFrom(d), nil
	}
	welcome := env.GetWelcome()
	if welcome == nil {
		return nil, errors.New("protocol error: expected Welcome")
	}
	if st := welcome.GetServerTime(); st != nil {
		a.clockOffset.Store(int64(time.Until(st.AsTime())))
	}
	interval := time.Duration(welcome.GetHeartbeatIntervalS()) * time.Second
	if interval < 5*time.Second || interval > 10*time.Minute {
		interval = 30 * time.Second
	}
	a.log.Info("connected", "session_id", welcome.GetSessionId(), "heartbeat", interval,
		"clock_offset", time.Duration(a.clockOffset.Load()).Round(time.Millisecond))

	a.cur.Store(c)
	defer a.cur.CompareAndSwap(c, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); c.writeLoop() }()
	go func() { defer wg.Done(); c.timers(interval, tok) }()
	c.send(&agentv1.Envelope{Body: &agentv1.Envelope_Inventory{Inventory: collectInventory(cctx)}})
	c.kickOutbox()

	var readErr error
	for {
		env, err := readEnvelope(cctx, ws)
		if err != nil {
			readErr = err
			break
		}
		if d := env.GetDisconnect(); d != nil {
			c.end.Store(endFrom(d))
			continue // the server closes the socket next
		}
		a.handle(c, env)
	}
	cancel()
	wg.Wait()
	if e := c.end.Load(); e != nil {
		return e, nil
	}
	if cs := websocket.CloseStatus(readErr); cs == websocket.StatusNormalClosure {
		return nil, nil
	}
	return nil, readErr
}

func endFrom(d *agentv1.Disconnect) *sessionEnd {
	return &sessionEnd{reason: d.GetReason(), message: d.GetMessage(), retryAfter: time.Duration(d.GetRetryAfterS()) * time.Second}
}

func readEnvelope(ctx context.Context, ws *websocket.Conn) (*agentv1.Envelope, error) {
	typ, data, err := ws.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageBinary {
		return nil, errors.New("unexpected text frame")
	}
	var env agentv1.Envelope
	if err := proto.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

func (c *conn) writeLoop() {
	write := func(b []byte) bool {
		ctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
		defer cancel()
		if err := c.ws.Write(ctx, websocket.MessageBinary, b); err != nil {
			c.cancel()
			return false
		}
		return true
	}
	for {
		select {
		case <-c.ctx.Done():
			return
		case b := <-c.out:
			if !write(b) {
				return
			}
		case <-c.kick:
			for {
				r := c.a.popOutbox()
				if r == nil {
					break
				}
				b, err := envelope(&agentv1.Envelope{Body: &agentv1.Envelope_CommandResult{CommandResult: r}})
				if err != nil {
					continue
				}
				if !write(b) {
					c.a.unpopOutbox(r)
					return
				}
			}
		}
	}
}

// timers drives heartbeats, periodic inventory and token refresh.
func (c *conn) timers(interval time.Duration, tok *agentapi.TokenResponse) {
	hb := time.NewTicker(interval)
	inv := time.NewTicker(inventoryInterval)
	defer hb.Stop()
	defer inv.Stop()
	refreshIn := func(expiresIn int) time.Duration {
		d := time.Duration(expiresIn) * time.Second * 2 / 3
		if d < 10*time.Second {
			d = 10 * time.Second
		}
		return d
	}
	refresh := time.NewTimer(refreshIn(tok.ExpiresIn))
	defer refresh.Stop()
	var seq uint64
	c.send(&agentv1.Envelope{Body: &agentv1.Envelope_Heartbeat{Heartbeat: collectHeartbeat(c.ctx, seq)}})
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-hb.C:
			seq++
			c.trySend(&agentv1.Envelope{Body: &agentv1.Envelope_Heartbeat{Heartbeat: collectHeartbeat(c.ctx, seq)}})
		case <-inv.C:
			c.trySend(&agentv1.Envelope{Body: &agentv1.Envelope_Inventory{Inventory: collectInventory(c.ctx)}})
		case <-refresh.C:
			ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
			t, err := c.a.client.token(ctx, c.a.id.DeviceID, c.a.id.Key())
			cancel()
			if err != nil {
				c.a.log.Warn("token refresh failed; will retry", "err", err)
				if IsCode(err, agentapi.CodeDeviceRevoked) {
					c.a.halt()
					c.cancel()
					return
				}
				refresh.Reset(30 * time.Second)
				continue
			}
			c.send(&agentv1.Envelope{Body: &agentv1.Envelope_TokenRefresh{TokenRefresh: &agentv1.TokenRefresh{AccessToken: t.AccessToken}}})
			refresh.Reset(refreshIn(t.ExpiresIn))
		}
	}
}

// handle processes one server message.
func (a *Agent) handle(c *conn, env *agentv1.Envelope) {
	switch b := env.Body.(type) {
	case *agentv1.Envelope_CommandRequest:
		a.onCommand(c, b.CommandRequest)
	case *agentv1.Envelope_CommandCancel:
		a.log.Info("cancel requested", "command_id", b.CommandCancel.GetCommandId())
		a.exec.cancel(b.CommandCancel.GetCommandId())
	case *agentv1.Envelope_Error:
		a.log.Warn("server error", "code", b.Error.GetCode(), "message", b.Error.GetMessage())
	case *agentv1.Envelope_ConfigUpdate, *agentv1.Envelope_Welcome:
		// Heartbeat changes take effect on the next connection.
	}
}

func (a *Agent) onCommand(c *conn, req *agentv1.CommandRequest) {
	spec, err := a.verify.verify(req)
	if errors.Is(err, errReplay) {
		a.log.Debug("duplicate command delivery ignored", "command_id", spec.GetCommandId())
		return
	}
	reject := func(reason string) {
		if spec == nil || spec.GetCommandId() == "" {
			return
		}
		a.log.Warn("command rejected", "command_id", spec.GetCommandId(), "reason", reason)
		a.result(&agentv1.CommandResult{CommandId: spec.GetCommandId(), Status: agentv1.CommandStatus_COMMAND_STATUS_REJECTED,
			Error: reason, StartedAt: timestamppb.Now(), FinishedAt: timestamppb.Now()})
	}
	if err != nil {
		reject(err.Error())
		return
	}
	if reason := a.exec.admit(spec); reason != "" {
		reject(reason)
		return
	}
	a.log.Info("executing command", "command_id", spec.GetCommandId(), "kind", spec.GetKind(), "action", spec.GetAction(),
		"issued_by", spec.GetIssuedBy())
	c.send(&agentv1.Envelope{Body: &agentv1.Envelope_CommandAck{CommandAck: &agentv1.CommandAck{CommandId: spec.GetCommandId()}}})
	go a.exec.run(spec)
}
