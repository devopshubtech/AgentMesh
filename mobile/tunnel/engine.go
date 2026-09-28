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

// Engine is one exit-node tunnel. Create with NewEngine, then Start/Stop.
type Engine struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	dev     device.Device
	stk     *stack.Stack
	sess    *yamux.Session
	ws      *websocket.Conn
	running atomic.Bool
	lastErr atomic.Value

	up, down, flows, failed, quicBlocked atomic.Int64
}

// Version is the engine version string (set by the app build).
func Version() string { return "0.5.0" }

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

	hc, err := httpClient(caPEM)
	if err != nil {
		return err
	}
	u := relayURL
	switch {
	case len(u) > 8 && u[:8] == "https://":
		u = "wss://" + u[8:]
	case len(u) > 7 && u[:7] == "http://":
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
		return fmt.Errorf("connect relay: %w", err)
	}
	ws.SetReadLimit(4 << 20)
	sess, err := yamux.Client(websocket.NetConn(ctx, ws, websocket.MessageBinary), exitproto.YamuxConfig())
	if err != nil {
		cancel()
		ws.CloseNow()
		return fmt.Errorf("start multiplexer: %w", err)
	}
	dev, err := fdbased.Open(strconv.Itoa(fd), uint32(mtu), 0)
	if err != nil {
		cancel()
		_ = sess.Close()
		return fmt.Errorf("open tun fd: %w", err)
	}
	stk, err := core.CreateStack(&core.Config{LinkEndpoint: dev, TransportHandler: &handler{e: e}})
	if err != nil {
		cancel()
		dev.Close()
		_ = sess.Close()
		return fmt.Errorf("create stack: %w", err)
	}
	e.cancel, e.dev, e.stk, e.sess, e.ws = cancel, dev, stk, sess, ws
	e.running.Store(true)
	go func() {
		select {
		case <-sess.CloseChan():
			e.fail("relay connection closed")
		case <-ctx.Done():
		}
	}()
	return nil
}

func (e *Engine) fail(msg string) {
	if e.running.Load() {
		e.lastErr.Store(msg)
	}
	e.Stop()
}

// Stop tears the tunnel down. It is safe to call more than once.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running.Swap(false) {
		return
	}
	if e.cancel != nil {
		e.cancel()
	}
	if e.sess != nil {
		_ = e.sess.Close()
	}
	if e.ws != nil {
		_ = e.ws.Close(websocket.StatusNormalClosure, "client disconnected")
	}
	if e.stk != nil {
		e.stk.Close()
	}
	if e.dev != nil {
		e.dev.Close()
	}
	e.stk, e.dev, e.sess, e.ws = nil, nil, nil, nil
}

// IsRunning reports whether the tunnel is up.
func (e *Engine) IsRunning() bool { return e.running.Load() }

// LastError is the reason the tunnel stopped unexpectedly ("" if none).
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
	sess := func() *yamux.Session { e.mu.Lock(); defer e.mu.Unlock(); return e.sess }()
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
	// QUIC (UDP/443) inside a reliable relay stream suffers from stacked
	// congestion control and head-of-line blocking; refusing it makes
	// browsers fall back to TCP immediately, which is much faster here.
	if c.ID().LocalPort == 443 {
		h.e.quicBlocked.Add(1)
		return
	}
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
