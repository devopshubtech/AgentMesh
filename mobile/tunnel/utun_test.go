package tunnel

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// fakeUtun returns queued frames on Read and records frames on Write.
type fakeUtun struct {
	in      [][]byte
	written [][]byte
}

func (f *fakeUtun) Read(p []byte) (int, error) {
	frame := f.in[0]
	f.in = f.in[1:]
	return copy(p, frame), nil
}

func (f *fakeUtun) Write(p []byte) (int, error) {
	f.written = append(f.written, append([]byte(nil), p...))
	return len(p), nil
}

func frame(af uint32, pkt []byte) []byte {
	b := make([]byte, 4+len(pkt))
	binary.BigEndian.PutUint32(b, af)
	copy(b[4:], pkt)
	return b
}

func TestUtunRead(t *testing.T) {
	v4 := []byte{0x45, 0, 0, 20, 1, 2, 3, 4}
	v6 := []byte{0x60, 0, 0, 0, 9, 9}
	f := &fakeUtun{in: [][]byte{frame(darwinAFInet, v4), frame(darwinAFInet6, v6), {0, 0, 0, 2}}}
	u := newUtunRW(f, 1500)
	p := make([]byte, 1500)

	if n, err := u.Read(p); err != nil || !bytes.Equal(p[:n], v4) {
		t.Fatalf("IPv4: got %x, %v; want %x (header stripped)", p[:n], err, v4)
	}
	if n, err := u.Read(p); err != nil || n != 0 {
		t.Fatalf("IPv6: got %d bytes, %v; want dropped (0)", n, err)
	}
	if n, err := u.Read(p); err != nil || n != 0 {
		t.Fatalf("header-only frame: got %d bytes, %v; want 0", n, err)
	}
}

func TestUtunWrite(t *testing.T) {
	f := &fakeUtun{}
	u := newUtunRW(f, 4)                     // smaller than the packets: buffer must grow
	v4 := []byte{0x45, 0, 0, 20, 1, 2, 3, 4} // version nibble 4
	v6 := []byte{0x60, 0, 0, 0, 9, 9}        // version nibble 6

	if n, err := u.Write(v4); err != nil || n != len(v4) {
		t.Fatalf("IPv4 write: n=%d err=%v", n, err)
	}
	if n, err := u.Write(v6); err != nil || n != len(v6) {
		t.Fatalf("IPv6 write: n=%d err=%v", n, err)
	}
	if !bytes.Equal(f.written[0], frame(darwinAFInet, v4)) {
		t.Errorf("IPv4 frame = %x, want AF_INET header + packet", f.written[0])
	}
	if !bytes.Equal(f.written[1], frame(darwinAFInet6, v6)) {
		t.Errorf("IPv6 frame = %x, want AF_INET6 header + packet", f.written[1])
	}
	if n, _ := u.Write(nil); n != 0 || len(f.written) != 2 {
		t.Errorf("empty write must be a no-op")
	}
}
