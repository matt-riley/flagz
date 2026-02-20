package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzNormalizeNotifyChannel(f *testing.F) {
	f.Add("")
	f.Add("flag_events")
	f.Add("  custom_events  ")

	f.Fuzz(func(t *testing.T, channel string) {
		got := normalizeNotifyChannel(channel)
		trimmed := strings.TrimSpace(channel)
		if trimmed == "" {
			if got != defaultNotifyChannel {
				t.Fatalf("normalizeNotifyChannel(%q) = %q, want %q", channel, got, defaultNotifyChannel)
			}
			return
		}

		if got != trimmed {
			t.Fatalf("normalizeNotifyChannel(%q) = %q, want %q", channel, got, trimmed)
		}
	})
}

func FuzzEnsureJSON(f *testing.F) {
	f.Add([]byte{}, "{}")
	f.Add([]byte(`{"a":1}`), "{}")

	f.Fuzz(func(t *testing.T, input []byte, fallback string) {
		got := ensureJSON(json.RawMessage(input), fallback)
		if len(input) == 0 {
			if string(got) != fallback {
				t.Fatalf("ensureJSON(empty,%q) = %q, want %q", fallback, got, fallback)
			}
			return
		}

		if string(got) != string(input) {
			t.Fatalf("ensureJSON(non-empty) = %q, want %q", got, input)
		}
	})
}

func FuzzListenStatement(f *testing.F) {
	f.Add("flag_events")
	f.Add("custom-events")
	f.Add(`";DROP TABLE flags;--`)

	f.Fuzz(func(t *testing.T, channel string) {
		statement := listenStatement(channel)
		if !strings.HasPrefix(statement, "LISTEN ") {
			t.Fatalf("listenStatement(%q) = %q, want LISTEN prefix", channel, statement)
		}
	})
}

func FuzzMarshalNotifyPayload(f *testing.F) {
	f.Add("new-ui", "updated")
	f.Add("old-ui", "deleted")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, flagKey, eventType string) {
		payload, err := marshalNotifyPayload(FlagEvent{
			FlagKey:   flagKey,
			EventType: eventType,
		})
		if err != nil {
			t.Fatalf("marshalNotifyPayload() error = %v", err)
		}

		var decoded struct {
			FlagKey   string `json:"flag_key"`
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			t.Fatalf("notify payload should be valid JSON: %v", err)
		}
		if utf8.ValidString(flagKey) && decoded.FlagKey != flagKey {
			t.Fatalf("decoded payload flag key mismatch: got %q, want %q", decoded.FlagKey, flagKey)
		}
		if utf8.ValidString(eventType) && decoded.EventType != eventType {
			t.Fatalf("decoded payload event type mismatch: got %q, want %q", decoded.EventType, eventType)
		}
	})
}
