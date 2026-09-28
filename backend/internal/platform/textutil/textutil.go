// Package textutil sanitizes untrusted, device-reported strings.
package textutil

import (
	"strings"
	"unicode/utf8"
)

// Clean trims s, drops invalid UTF-8 and control characters, and truncates
// it to at most max bytes on a rune boundary.
func Clean(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	for len(s) > max {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}
