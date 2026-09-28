package exitproto

import (
	"bytes"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	var b bytes.Buffer
	if err := WriteRequest(&b, Request{Network: NetTCP, Addr: "example.com:443"}); err != nil {
		t.Fatal(err)
	}
	r, err := ReadRequest(&b)
	if err != nil || r.Network != NetTCP || r.Addr != "example.com:443" {
		t.Fatalf("got %+v %v", r, err)
	}
	if err := WriteRequest(&b, Request{Network: NetTCP}); err == nil {
		t.Fatal("empty address accepted")
	}
	if _, err := ReadRequest(bytes.NewReader([]byte{9, 1, 0, 1, 'x'})); err == nil {
		t.Fatal("bad version accepted")
	}
	if _, err := ReadRequest(bytes.NewReader([]byte{1, 7, 0, 1, 'x'})); err == nil {
		t.Fatal("bad network accepted")
	}
}

func TestResponseAndDatagrams(t *testing.T) {
	var b bytes.Buffer
	_ = WriteResponse(&b, StatusDenied, "private address")
	s, msg, err := ReadResponse(&b)
	if err != nil || s != StatusDenied || msg != "private address" {
		t.Fatalf("got %d %q %v", s, msg, err)
	}
	_ = WriteDatagram(&b, []byte("dns-query"))
	_ = WriteDatagram(&b, nil)
	buf := make([]byte, MaxDatagram)
	n, err := ReadDatagram(&b, buf)
	if err != nil || string(buf[:n]) != "dns-query" {
		t.Fatalf("datagram 1: %q %v", buf[:n], err)
	}
	if n, err = ReadDatagram(&b, buf); err != nil || n != 0 {
		t.Fatalf("empty datagram: %d %v", n, err)
	}
	if _, err := ReadDatagram(bytes.NewReader([]byte{0, 10, 1, 2}), make([]byte, 4)); err == nil {
		t.Fatal("oversized datagram accepted")
	}
}
