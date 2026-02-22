package logging

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseLevel(tt.input); got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewWithWriter(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter("info", &buf)
	log.Info("hello", "key", "value")

	if buf.Len() == 0 {
		t.Fatal("expected log output, got nothing")
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"msg":"hello"`)) {
		t.Errorf("expected JSON msg field, got: %s", buf.String())
	}
}
