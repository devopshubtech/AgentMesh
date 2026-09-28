package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"google.golang.org/protobuf/proto"

	"github.com/enfec/agentmesh/protocols/agentapi"
	"github.com/enfec/agentmesh/protocols/exitproto"
	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

// Exit node: the agent joins a relayed session and dials flows on behalf of
// the operator's client, so that client's traffic leaves with this device's
// IP. It is disabled unless the device owner sets "allow_exit_node": true.

const (
	maxExitStreams = 512
	udpIdleTimeout = 60 * time.Second
	dialTimeout    = 10 * time.Second
)

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// errDestinationDenied is returned by the dial control hook.
var errDestinationDenied = errors.New("destination not allowed by exit-node policy")

// destinationAllowed rejects addresses that would let a remote user reach
// this machine or its local network.
func destinationAllowed(ip netip.Addr, allowLAN bool) bool {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid(), ip.IsUnspecified(), ip.IsLoopback(), ip.IsMulticast(), ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(), ip.IsInterfaceLocalMulticast():
		return false
	case ip.Is4() && (ip.As4()[0] == 0 || ip == netip.AddrFrom4([4]byte{255, 255, 255, 255})):
		return false
	case !allowLAN && (ip.IsPrivate() || cgnat.Contains(ip)):
		return false
	}
	return true
}

func (p Policy) exitNodeAllowed() bool { return p.AllowExitNode != nil && *p.AllowExitNode }

// exitDialer checks the *connected* address (after DNS resolution), which
// also defeats DNS-rebinding tricks.
func exitDialer(allowLAN bool) *net.Dialer {
	return &net.Dialer{
		Timeout: dialTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !destinationAllowed(ip, allowLAN) {
				return errDestinationDenied
			}
			return nil
		},
	}
}

func (a *Agent) onSessionOpen(req *agentv1.SessionOpen) {
	spec, err := a.verifySession(req)
	if err != nil {
		a.log.Warn("session request rejected", "err", err)
		return
	}
	if !a.cfg.Policy.exitNodeAllowed() {
		a.log.Warn("exit-node session refused by local policy", "session_id", spec.GetSessionId())
		return
	}
	a.log.Info("exit-node session starting", "session_id", spec.GetSessionId(), "issued_by", spec.GetIssuedBy())
	go a.runExitSession(spec)
}

func (a *Agent) verifySession(req *agentv1.SessionOpen) (*agentv1.SessionSpec, error) {
	pub := a.id.commandKey(req.GetKeyId())
	if pub == nil {
		return nil, fmt.Errorf("unknown signing key %q", req.GetKeyId())
	}
	if !agentapi.VerifySession(pub, req.GetSpec(), req.GetSignature()) {
		return nil, errors.New("invalid session signature")
	}
	var spec agentv1.SessionSpec
	if err := proto.Unmarshal(req.GetSpec(), &spec); err != nil {
		return nil, errors.New("malformed session spec")
	}
	if spec.GetDeviceId() != a.id.DeviceID || spec.GetOrgId() != a.id.OrgID {
		return nil, errors.New("session addressed to a different device")
	}
	if spec.GetKind() != "exit_node" {
		return nil, fmt.Errorf("unsupported session kind %q", spec.GetKind())
	}
	now := time.Now().Add(time.Duration(a.clockOffset.Load()))
	exp := spec.GetExpiresAt().AsTime()
	if spec.GetExpiresAt() == nil || now.After(exp.Add(clockSkewTolerance)) {
		return nil, errors.New("session grant expired")
	}
	if err := a.verify.replay.accept("session:"+spec.GetSessionId(), exp); err != nil {
		return nil, err
	}
	return &spec, nil
}

func relayURL(base string) string {
	return strings.TrimSuffix(wsURL(base), agentapi.PathConnect) + agentapi.PathRelay
}

func (a *Agent) runExitSession(spec *agentv1.SessionSpec) {
	maxDur := time.Duration(spec.GetMaxDurationS()) * time.Second
	if maxDur <= 0 || maxDur > 24*time.Hour {
		maxDur = 12 * time.Hour
	}
	ctx, cancel := context.WithTimeout(context.Background(), maxDur)
	defer cancel()
	log := a.log.With("session_id", spec.GetSessionId())

	dctx, dcancel := context.WithTimeout(ctx, 30*time.Second)
	ws, _, err := websocket.Dial(dctx, relayURL(a.cfg.ServerURL), &websocket.DialOptions{
		HTTPClient:      a.client.http,
		HTTPHeader:      http.Header{agentapi.RelayTicketHeader: {spec.GetRelayTicket()}, "User-Agent": {"agentmesh-agent/" + Version}},
		Subprotocols:    []string{agentapi.RelaySubprotocol},
		CompressionMode: websocket.CompressionDisabled,
	})
	dcancel()
	if err != nil {
		log.Error("join relay failed", "err", err)
		return
	}
	ws.SetReadLimit(4 << 20)
	defer ws.CloseNow()
	nc := websocket.NetConn(ctx, ws, websocket.MessageBinary)
	sess, err := yamux.Server(nc, exitproto.YamuxConfig())
	if err != nil {
		log.Error("start multiplexer", "err", err)
		return
	}
	defer sess.Close()

	var flows, active atomic.Int64
	var up, down atomic.Int64
	allowLAN := a.cfg.Policy.ExitNodeAllowLAN
	dialer := exitDialer(allowLAN)
	start := time.Now()
	for {
		st, err := sess.AcceptStream()
		if err != nil {
			break
		}
		if active.Load() >= maxExitStreams {
			_ = exitproto.WriteResponse(st, exitproto.StatusDialFailed, "too many concurrent flows")
			_ = st.Close()
			continue
		}
		flows.Add(1)
		active.Add(1)
		go func() {
			defer active.Add(-1)
			serveFlow(ctx, st, dialer, &up, &down)
		}()
	}
	log.Info("exit-node session ended", "flows", flows.Load(), "bytes_to_internet", up.Load(),
		"bytes_from_internet", down.Load(), "duration", time.Since(start).Round(time.Second))
}

type countWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

func serveFlow(ctx context.Context, st net.Conn, dialer *net.Dialer, up, down *atomic.Int64) {
	defer st.Close()
	_ = st.SetDeadline(time.Now().Add(exitproto.HandshakeLimit))
	req, err := exitproto.ReadRequest(st)
	if err != nil {
		_ = exitproto.WriteResponse(st, exitproto.StatusBadRequest, err.Error())
		return
	}
	network := "tcp"
	if req.Network == exitproto.NetUDP {
		network = "udp"
	}
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	out, err := dialer.DialContext(dctx, network, req.Addr)
	cancel()
	if err != nil {
		status := exitproto.StatusDialFailed
		if errors.Is(err, errDestinationDenied) {
			status = exitproto.StatusDenied
		}
		_ = exitproto.WriteResponse(st, status, err.Error())
		return
	}
	defer out.Close()
	if err := exitproto.WriteResponse(st, exitproto.StatusOK, ""); err != nil {
		return
	}
	_ = st.SetDeadline(time.Time{})
	if network == "tcp" {
		pipeTCP(st, out, up, down)
		return
	}
	pipeUDP(st, out.(*net.UDPConn), up, down)
}

func pipeTCP(st net.Conn, out net.Conn, up, down *atomic.Int64) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(countWriter{out, up}, st)
		if tc, ok := out.(*net.TCPConn); ok {
			_ = tc.CloseWrite() // propagate half-close to the destination
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(countWriter{st, down}, out)
		_ = st.Close()
	}()
	wg.Wait()
}

func pipeUDP(st net.Conn, out *net.UDPConn, up, down *atomic.Int64) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, exitproto.MaxDatagram)
		for {
			_ = out.SetReadDeadline(time.Now().Add(udpIdleTimeout))
			n, err := out.Read(buf)
			if err != nil {
				_ = st.Close()
				return
			}
			if exitproto.WriteDatagram(st, buf[:n]) != nil {
				return
			}
			down.Add(int64(n))
		}
	}()
	buf := make([]byte, exitproto.MaxDatagram)
	for {
		_ = st.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		n, err := exitproto.ReadDatagram(st, buf)
		if err != nil {
			break
		}
		if _, err := out.Write(buf[:n]); err != nil {
			break
		}
		up.Add(int64(n))
	}
	_ = out.Close()
	<-done
}
