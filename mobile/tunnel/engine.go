// Package tunnel is the exit-node client engine used by the Android control
// app (bound with gomobile). It reads IP packets from the VpnService TUN file
// descriptor, terminates TCP/UDP in a userspace stack (gVisor via tun2socks
// core) and carries every flow over the AgentMesh relay to the chosen agent,
// which dials the real destination. Traffic therefore exits from the agent's
// network and IP address.
//
// Only gomobile-compatible types are exported.
package tunnel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/enfec/agentmesh/protocols/agentapi"
	"github.com/enfec/agentmesh/protocols/exitproto"
)

const udpIdle = 60 * time.Second

var debug = os.Getenv("AGENTMESH_TUNNEL_DEBUG") != ""

func debugf(format string, args ...any) {
	if debug {
		log.Printf("tunnel: "+format, args...)
	}
}

// lastLog keeps the most recent yamux log line, e.g. "keepalive failed: ...".
type lastLog struct {
	mu sync.Mutex
	s  string
}

func (l *lastLog) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if i := strings.Index(line, "yamux: "); i >= 0 {
		line = line[i+len("yamux: "):]
	}
	debugf("yamux: %s", line)
	l.mu.Lock()
	l.s = line
	l.mu.Unlock()
	return len(p), nil
}

func (l *lastLog) get() string { l.mu.Lock(); defer l.mu.Unlock(); return l.s }

// Engine is one exit-node tunnel. Create with NewEngine, then Start/Stop.
//
// The TUN device and the userspace stack live as long as the engine. The
// relay connection under them can be replaced with Reconnect when it drops,
// so the phone's VPN interface (and apps' view of the network) stays up.
type Engine struct {
	mu      sync.Mutex
	dev     device.Device
	stk     *stack.Stack
	relay   *relayConn
	running atomic.Bool
	relayUp atomic.Bool
	lastErr atomic.Value

	up, down, flows, failed atomic.Int64
}

// relayConn is one WebSocket to the relay with its multiplexer.
type relayConn struct {
	cancel context.CancelFunc
	ws     *websocket.Conn
	sess   *yamux.Session
	ylog   *lastLog
}

func (r *relayConn) close() {
	r.cancel()
	_ = r.sess.Close()
	_ = r.ws.Close(websocket.StatusNormalClosure, "client disconnected")
}

// Version is the engine version string.
func Version() string { return "0.6.2" }

// NewEngine returns an idle engine.
func NewEngine() *Engine { return &Engine{} }

func httpClient(caPEM string) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if caPEM != "" && !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, errors.New("custom CA certificate is not valid PEM")
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			TLSClientConfig:     &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}, nil
}

func dialRelay(relayURL, ticket, caPEM string) (*relayConn, error) {
	hc, err := httpClient(caPEM)
	if err != nil {
		return nil, err
	}
	u := relayURL
	switch {
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + u[8:]
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + u[7:]
	}
	ctx, cancel := context.WithCancel(context.Background())
	dctx, dcancel := context.WithTimeout(ctx, 20*time.Second)
	ws, _, err := websocket.Dial(dctx, u, &websocket.DialOptions{
		HTTPClient:      hc,
		HTTPHeader:      http.Header{agentapi.RelayTicketHeader: {ticket}, "User-Agent": {"agentmesh-android"}},
		Subprotocols:    []string{agentapi.RelaySubprotocol},
		CompressionMode: websocket.CompressionDisabled,
	})
	dcancel()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect relay: %w", err)
	}
	ws.SetReadLimit(4 << 20)
	ycfg := exitproto.YamuxConfig()
	// Our receive window bounds how much the agent may queue per flow. Exit
	// devices often have little upload bandwidth (every byte the phone gets
	// is upload for them); with big windows a video queues megabytes ahead of
	// DNS, page loads and keepalives, so everything else times out and the
	// agent drops the relay. 256 KiB still allows ~10 Mbit/s per flow.
	ycfg.MaxStreamWindowSize = 256 << 10
	ylog := &lastLog{}
	ycfg.LogOutput = ylog // yamux only reports why it closed a session via its logger
	sess, err := yamux.Client(websocket.NetConn(ctx, ws, websocket.MessageBinary), ycfg)
	if err != nil {
		cancel()
		ws.CloseNow()
		return nil, fmt.Errorf("start multiplexer: %w", err)
	}
	return &relayConn{cancel: cancel, ws: ws, sess: sess, ylog: ylog}, nil
}

// Start joins the relay with ticket and begins forwarding packets from the
// TUN file descriptor fd. It returns once the tunnel is established.
func (e *Engine) Start(fd int, relayURL, ticket, caPEM string, mtu int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running.Load() {
		return errors.New("tunnel already running")
	}
	if mtu <= 0 {
		mtu = 1500
	}
	e.up.Store(0)
	e.down.Store(0)
	e.flows.Store(0)
	e.failed.Store(0)
	e.lastErr.Store("")

	rc, err := dialRelay(relayURL, ticket, caPEM)
	if err != nil {
		return err
	}
	dev, err := fdbased.Open(strconv.Itoa(fd), uint32(mtu), 0)
	if err != nil {
		rc.close()
		return fmt.Errorf("open tun fd: %w", err)
	}
	stk, err := core.CreateStack(&core.Config{LinkEndpoint: dev, TransportHandler: &handler{e: e}})
	if err != nil {
		dev.Close()
		rc.close()
		return fmt.Errorf("create stack: %w", err)
	}
	e.dev, e.stk, e.relay = dev, stk, rc
	e.running.Store(true)
	e.relayUp.Store(true)
	go e.watch(rc)
	return nil
}

// Reconnect replaces the relay connection with a new one for a fresh
// session ticket. The TUN device and stack are kept; flows that were open on
// the old connection end and apps simply open new ones.
func (e *Engine) Reconnect(relayURL, ticket, caPEM string) error {
	if !e.running.Load() {
		return errors.New("tunnel is not running")
	}
	rc, err := dialRelay(relayURL, ticket, caPEM)
	if err != nil {
		return err
	}
	e.mu.Lock()
	if !e.running.Load() {
		e.mu.Unlock()
		rc.close()
		return errors.New("tunnel is not running")
	}
	old := e.relay
	e.relay = rc
	e.relayUp.Store(true)
	e.lastErr.Store("")
	e.mu.Unlock()
	if old != nil {
		old.close()
	}
	go e.watch(rc)
	return nil
}

// watch marks the relay down when rc's multiplexer closes (unless it has
// already been replaced or the engine stopped).
func (e *Engine) watch(rc *relayConn) {
	<-rc.sess.CloseChan()
	msg := "relay connection closed"
	if why := rc.ylog.get(); why != "" {
		msg += ": " + why
	}
	e.mu.Lock()
	current := e.relay == rc && e.running.Load()
	if current {
		e.relayUp.Store(false)
		e.lastErr.Store(msg)
	}
	e.mu.Unlock()
	if current {
		debugf("%s", msg)
		rc.close()
	}
}

// Stop tears the tunnel down. It is safe to call more than once.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running.Swap(false) {
		return
	}
	e.relayUp.Store(false)
	if e.relay != nil {
		e.relay.close()
	}
	if e.stk != nil {
		e.stk.Close()
	}
	if e.dev != nil {
		e.dev.Close()
	}
	e.stk, e.dev, e.relay = nil, nil, nil
}

// IsRunning reports whether the tunnel (VPN side) is up.
func (e *Engine) IsRunning() bool { return e.running.Load() }

// RelayUp reports whether traffic can currently reach the agent. When it is
// false while IsRunning is true, call Reconnect with a new session ticket.
func (e *Engine) RelayUp() bool { return e.relayUp.Load() }

// LastError is why the relay last dropped ("" while it is up).
func (e *Engine) LastError() string { s, _ := e.lastErr.Load().(string); return s }

// BytesUp is traffic sent to the internet through the agent.
func (e *Engine) BytesUp() int64 { return e.up.Load() }

// BytesDown is traffic received from the internet through the agent.
func (e *Engine) BytesDown() int64 { return e.down.Load() }

// Flows is the number of TCP/UDP flows opened.
func (e *Engine) Flows() int64 { return e.flows.Load() }

// FailedFlows is the number of flows the agent refused or could not dial.
func (e *Engine) FailedFlows() int64 { return e.failed.Load() }

// PublicIP returns the address the internet sees for this tunnel, fetched
// from Cloudflare's trace endpoint through the agent.
func (e *Engine) PublicIP() (string, error) {
	if !e.running.Load() {
		return "", errors.New("tunnel is not running")
	}
	h := &handler{e: e}
	pool, _ := x509.SystemCertPool()
	c := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			return h.open(exitproto.NetTCP, addr)
		},
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	resp, err := c.Get("https://1.1.1.1/cdn-cgi/trace")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if ip, ok := strings.CutPrefix(line, "ip="); ok {
			return strings.TrimSpace(ip), nil
		}
	}
	return "", errors.New("exit IP not found in response")
}

// ---------------------------------------------------------------- flows

type handler struct{ e *Engine }

type counter struct {
	w io.Writer
	n *atomic.Int64
}

func (c counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

func (h *handler) open(network byte, addr string) (net.Conn, error) {
	e := h.e
	sess := func() *yamux.Session {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.relay == nil {
			return nil
		}
		return e.relay.sess
	}()
	if sess == nil {
		return nil, errors.New("tunnel closed")
	}
	st, err := sess.OpenStream()
	if err != nil {
		return nil, err
	}
	_ = st.SetDeadline(time.Now().Add(exitproto.HandshakeLimit))
	if err := exitproto.WriteRequest(st, exitproto.Request{Network: network, Addr: addr}); err != nil {
		st.Close()
		return nil, err
	}
	status, msg, err := exitproto.ReadResponse(st)
	if err != nil {
		st.Close()
		return nil, err
	}
	if status != exitproto.StatusOK {
		st.Close()
		return nil, fmt.Errorf("agent refused %s: %s", addr, msg)
	}
	_ = st.SetDeadline(time.Time{})
	e.flows.Add(1)
	return st, nil
}

func dst(id stack.TransportEndpointID) string {
	// In the forwarder, the "local" side of the endpoint is the original destination.
	return net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort)))
}

// The stack invokes these on its packet-processing path (synchronously for
// UDP); flows must never block it, so each one runs in its own goroutine.
func (h *handler) HandleTCP(c adapter.TCPConn) { go h.tcp(c) }
func (h *handler) HandleUDP(c adapter.UDPConn) { go h.udp(c) }

func (h *handler) tcp(c adapter.TCPConn) {
	defer c.Close()
	st, err := h.open(exitproto.NetTCP, dst(c.ID()))
	if err != nil {
		h.e.failed.Add(1)
		return
	}
	defer st.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(counter{st, &h.e.up}, c)
		_ = st.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(counter{c, &h.e.down}, st)
		_ = c.Close()
	}()
	wg.Wait()
}

func (h *handler) udp(c adapter.UDPConn) {
	defer c.Close()
	// All UDP is relayed, including QUIC (UDP/443). Blocking QUIC is not
	// safe: Chrome often uses QUIC-only for hosts it knows support HTTP/3 and
	// then fails with ERR_QUIC_PROTOCOL_ERROR instead of falling back to TCP.
	st, err := h.open(exitproto.NetUDP, dst(c.ID()))
	debugf("udp flow %s open err=%v", dst(c.ID()), err)
	if err != nil {
		h.e.failed.Add(1)
		return
	}
	defer st.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, exitproto.MaxDatagram)
		for {
			_ = st.SetReadDeadline(time.Now().Add(udpIdle))
			n, err := exitproto.ReadDatagram(st, buf)
			if err != nil {
				_ = c.Close()
				return
			}
			if _, err := c.Write(buf[:n]); err != nil {
				return
			}
			h.e.down.Add(int64(n))
		}
	}()
	buf := make([]byte, exitproto.MaxDatagram)
	for {
		_ = c.SetReadDeadline(time.Now().Add(udpIdle))
		n, err := c.Read(buf)
		debugf("udp read from app n=%d err=%v", n, err)
		if err != nil {
			break
		}
		if exitproto.WriteDatagram(st, buf[:n]) != nil {
			break
		}
		h.e.up.Add(int64(n))
	}
	_ = st.Close()
	<-done
}
