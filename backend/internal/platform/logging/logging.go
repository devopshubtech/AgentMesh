// Package logging configures structured JSON logging.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON slog logger tagged with the service name.
func New(service, level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	l := slog.New(h).With("service", service)
	slog.SetDefault(l)
	return l
}
