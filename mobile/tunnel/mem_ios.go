//go:build ios

package tunnel

import rdebug "runtime/debug" // engine.go already has a "debug" flag

// iOS kills a packet-tunnel extension that uses more than ~50 MB, so keep the
// Go heap well below that.
func init() {
	rdebug.SetMemoryLimit(32 << 20)
	rdebug.SetGCPercent(50)
}
