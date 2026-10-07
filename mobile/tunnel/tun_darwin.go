//go:build darwin

package tunnel

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/iobased"
)

// openTUN wraps the utun file descriptor of an iOS/macOS packet tunnel
// (NEPacketTunnelProvider). tun2socks' generic unix device writes zeros where
// utun expects the address family, which the kernel drops, so packets go
// through utunRW (utun.go) instead.
//
// The fd belongs to the system extension, so the engine works on a duplicate:
// closing it on Stop unblocks the reader without closing the system's fd.
func openTUN(fd, mtu int) (device.Device, error) {
	dup, err := unix.Dup(fd)
	if err != nil {
		return nil, fmt.Errorf("dup utun fd: %w", err)
	}
	if err := unix.SetNonblock(dup, true); err != nil { // pollable, so Close interrupts Read
		unix.Close(dup)
		return nil, fmt.Errorf("utun nonblock: %w", err)
	}
	f := os.NewFile(uintptr(dup), "utun")
	ep, err := iobased.New(newUtunRW(f, mtu), uint32(mtu), 0)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &utunDevice{Endpoint: ep, f: f}, nil
}

type utunDevice struct {
	*iobased.Endpoint
	f *os.File
}

func (d *utunDevice) Name() string { return d.f.Name() }
func (d *utunDevice) Type() string { return "utun" }

func (d *utunDevice) Close() {
	_ = d.f.Close()
	d.Endpoint.Close()
}
