//go:build tools

package tunnel

// Keeps golang.org/x/mobile in go.mod: `gomobile bind` needs its bind
// package resolvable from this module.
import _ "golang.org/x/mobile/bind"
