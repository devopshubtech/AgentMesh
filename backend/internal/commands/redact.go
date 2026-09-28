package commands

import (
	"regexp"
	"strings"
)

var (
	secretFlag  = regexp.MustCompile(`(?i)^--?(password|passwd|pass|pwd|secret|token|api[-_]?key|access[-_]?key|private[-_]?key)$`)
	secretKV    = regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[-_]?key|access[-_]?key)\s*[=:]\s*)(\S+)`)
	bearerToken = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)
)

// RedactArgv masks likely secrets in argv before it is written to the audit
// log. It is a best-effort guard; callers should still avoid passing secrets
// on command lines.
func RedactArgv(argv []string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if i > 0 && secretFlag.MatchString(argv[i-1]) {
			out[i] = "***"
			continue
		}
		if k, _, ok := strings.Cut(a, "="); ok && secretFlag.MatchString(k) {
			out[i] = k + "=***"
			continue
		}
		a = secretKV.ReplaceAllString(a, "${1}***")
		out[i] = bearerToken.ReplaceAllString(a, "${1}***")
	}
	return out
}
