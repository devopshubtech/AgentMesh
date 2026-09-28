// Package exitproto is the exit-node stream protocol.
//
// A relayed session carries one yamux session end to end between the
// operator client (Android app) and the agent. Every yamux stream is one
// flow:
//
//	client → agent   request   : version(1) | network(1) | addrLen(2) | addr ("host:port")
//	agent  → client  response  : status(1)  | msgLen(2)  | msg
//	then TCP: raw bytes both ways
//	     UDP: datagrams framed as len(2) | payload, both ways
//
// The agent performs the real dial, so the flow leaves the network with the
// agent's IP address.
package exitproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/yamux"
)

const (
	Version = 1

	NetTCP byte = 1
	NetUDP byte = 2

	StatusOK         byte = 0
	StatusDenied     byte = 1 // destination blocked by agent policy
	StatusDialFailed byte = 2
	StatusBadRequest byte = 3

	MaxAddrLen     = 512
	MaxDatagram    = 65535
	HandshakeLimit = 15 * time.Second
)

// YamuxConfig is shared by both ends so keepalives and windows match.
func YamuxConfig() *yamux.Config {
	c := yamux.DefaultConfig()
	c.EnableKeepAlive = true
	// Phones may stall a backgrounded app for tens of seconds (battery
	// savers); tolerate ~60s without a pong before declaring the link dead.
	c.KeepAliveInterval = 30 * time.Second
	c.ConnectionWriteTimeout = 30 * time.Second
	c.MaxStreamWindowSize = 1 << 20 // 1 MiB per stream: good throughput on high-latency links
	c.StreamOpenTimeout = HandshakeLimit
	c.LogOutput = io.Discard
	return c
}

// Request is the per-stream flow request.
type Request struct {
	Network byte
	Addr    string
}

// WriteRequest sends a flow request.
func WriteRequest(w io.Writer, r Request) error {
	if len(r.Addr) == 0 || len(r.Addr) > MaxAddrLen {
		return errors.New("invalid address")
	}
	b := make([]byte, 4+len(r.Addr))
	b[0], b[1] = Version, r.Network
	binary.BigEndian.PutUint16(b[2:4], uint16(len(r.Addr)))
	copy(b[4:], r.Addr)
	_, err := w.Write(b)
	return err
}

// ReadRequest parses a flow request.
func ReadRequest(r io.Reader) (Request, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Request{}, err
	}
	if h[0] != Version {
		return Request{}, fmt.Errorf("unsupported version %d", h[0])
	}
	if h[1] != NetTCP && h[1] != NetUDP {
		return Request{}, fmt.Errorf("unsupported network %d", h[1])
	}
	n := int(binary.BigEndian.Uint16(h[2:4]))
	if n == 0 || n > MaxAddrLen {
		return Request{}, errors.New("invalid address length")
	}
	addr := make([]byte, n)
	if _, err := io.ReadFull(r, addr); err != nil {
		return Request{}, err
	}
	return Request{Network: h[1], Addr: string(addr)}, nil
}

// WriteResponse sends the flow status.
func WriteResponse(w io.Writer, status byte, msg string) error {
	if len(msg) > 1024 {
		msg = msg[:1024]
	}
	b := make([]byte, 3+len(msg))
	b[0] = status
	binary.BigEndian.PutUint16(b[1:3], uint16(len(msg)))
	copy(b[3:], msg)
	_, err := w.Write(b)
	return err
}

// ReadResponse reads the flow status.
func ReadResponse(r io.Reader) (byte, string, error) {
	var h [3]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, "", err
	}
	n := int(binary.BigEndian.Uint16(h[1:3]))
	if n > 1024 {
		return 0, "", errors.New("invalid response")
	}
	msg := make([]byte, n)
	if _, err := io.ReadFull(r, msg); err != nil {
		return 0, "", err
	}
	return h[0], string(msg), nil
}

// WriteDatagram writes one length-framed UDP datagram.
func WriteDatagram(w io.Writer, p []byte) error {
	if len(p) > MaxDatagram {
		return errors.New("datagram too large")
	}
	b := make([]byte, 2+len(p))
	binary.BigEndian.PutUint16(b, uint16(len(p)))
	copy(b[2:], p)
	_, err := w.Write(b)
	return err
}

// ReadDatagram reads one length-framed UDP datagram into buf.
func ReadDatagram(r io.Reader, buf []byte) (int, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, err
	}
	n := int(binary.BigEndian.Uint16(h[:]))
	if n > len(buf) {
		return 0, errors.New("datagram exceeds buffer")
	}
	return io.ReadFull(r, buf[:n])
}
