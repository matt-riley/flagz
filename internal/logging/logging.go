// Package logging provides a structured logger factory for the flagz server.
//
// It configures [log/slog] with a JSON handler and a configurable minimum
// level, suitable for production deployments.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New creates a [slog.Logger] that writes JSON to stderr at the given level.
// Accepted level strings (case-insensitive): "debug", "info", "warn", "error".
// An empty string defaults to "info".
func New(level string) *slog.Logger {
	return NewWithWriter(level, os.Stderr)
}

// NewWithWriter creates a [slog.Logger] writing JSON to w at the given level.
func NewWithWriter(level string, w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: ParseLevel(level),
	}))
}

// ParseLevel converts a level string to a [slog.Level].
// Returns [slog.LevelInfo] for unrecognised values.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
