package tunnel

import (
	"encoding/binary"
	"io"
)

// Address families in the 4-byte header that Darwin's utun puts in front of
// every packet (values from Darwin's sys/socket.h, so this file builds and is
// tested on every platform; only tun_darwin.go uses it at run time).
const (
	darwinAFInet  = 2
	darwinAFInet6 = 30
)

// utunRW turns a Darwin utun device into a plain IP packet ReadWriter: it
// strips the address-family header on read and adds the right one on write.
// iobased uses one reader and one writer goroutine, so each direction keeps
// its own buffer.
type utunRW struct {
	rw         io.ReadWriter
	rbuf, wbuf []byte
}

func newUtunRW(rw io.ReadWriter, mtu int) *utunRW {
	return &utunRW{rw: rw, rbuf: make([]byte, mtu+4), wbuf: make([]byte, mtu+4)}
}

func (u *utunRW) Read(p []byte) (int, error) {
	if len(u.rbuf) < len(p)+4 {
		u.rbuf = make([]byte, len(p)+4)
	}
	n, err := u.rw.Read(u.rbuf[:len(p)+4])
	if err != nil {
		return 0, err
	}
	if n <= 4 {
		return 0, nil // header only: iobased skips empty reads
	}
	// IPv6 is routed into the tunnel only so that it cannot leak around it.
	// Exit devices are often IPv4-only, so it is dropped here: apps fall back
	// to IPv4 within Happy Eyeballs' ~250 ms (Android blocks IPv6 instead).
	if binary.BigEndian.Uint32(u.rbuf[:4]) == darwinAFInet6 {
		return 0, nil
	}
	return copy(p, u.rbuf[4:n]), nil
}

func (u *utunRW) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	af := uint32(darwinAFInet)
	if p[0]>>4 == 6 {
		af = darwinAFInet6
	}
	if len(u.wbuf) < len(p)+4 {
		u.wbuf = make([]byte, len(p)+4)
	}
	buf := u.wbuf[:len(p)+4]
	binary.BigEndian.PutUint32(buf, af)
	copy(buf[4:], p)
	n, err := u.rw.Write(buf)
	if n >= 4 {
		n -= 4
	}
	return n, err
}
