//go:build !darwin

package tunnel

import (
	"strconv"

	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
)

// openTUN opens the Android VpnService TUN file descriptor (plain IP packets).
func openTUN(fd, mtu int) (device.Device, error) {
	return fdbased.Open(strconv.Itoa(fd), uint32(mtu), 0)
}
