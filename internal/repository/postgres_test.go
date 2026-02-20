package repository

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNormalizeNotifyChannel(t *testing.T) {
	t.Run("defaults when empty", func(t *testing.T) {
		if got := normalizeNotifyChannel(""); got != defaultNotifyChannel {
			t.Fatalf("normalizeNotifyChannel() = %q, want %q", got, defaultNotifyChannel)
		}
	})

	t.Run("trims non-empty values", func(t *testing.T) {
		if got := normalizeNotifyChannel("  custom_events  "); got != "custom_events" {
			t.Fatalf("normalizeNotifyChannel() = %q, want %q", got, "custom_events")
		}
	})
}

func TestEnsureJSON(t *testing.T) {
	if got := string(ensureJSON(nil, "{}")); got != "{}" {
		t.Fatalf("ensureJSON(nil) = %q, want %q", got, "{}")
	}

	if got := string(ensureJSON(json.RawMessage(`{"a":1}`), "{}")); got != `{"a":1}` {
		t.Fatalf("ensureJSON(non-empty) = %q, want %q", got, `{"a":1}`)
	}
}

func TestMarshalNotifyPayload(t *testing.T) {
	t.Run("marshals compact event payload", func(t *testing.T) {
		payload, err := marshalNotifyPayload(FlagEvent{
			EventID:   7,
			FlagKey:   "new-ui",
			EventType: "updated",
			Payload:   json.RawMessage(`{"enabled":true}`),
		})
		if err != nil {
			t.Fatalf("marshalNotifyPayload() error = %v", err)
		}

		var message struct {
			FlagKey   string `json:"flag_key"`
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal([]byte(payload), &message); err != nil {
			t.Fatalf("unmarshal notify payload: %v", err)
		}

		if message.FlagKey != "new-ui" || message.EventType != "updated" {
			t.Fatalf("unexpected notify payload envelope: %+v", message)
		}
	})
}

func TestListenStatement(t *testing.T) {
	if got := listenStatement("flag_events"); got != `LISTEN "flag_events"` {
		t.Fatalf("listenStatement() = %q, want %q", got, `LISTEN "flag_events"`)
	}
}

func TestDeleteFlagNoRows(t *testing.T) {
	if err := deleteFlagNoRows(pgconn.NewCommandTag("DELETE 1")); err != nil {
		t.Fatalf("deleteFlagNoRows(delete 1) error = %v, want nil", err)
	}

	if err := deleteFlagNoRows(pgconn.NewCommandTag("DELETE 0")); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleteFlagNoRows(delete 0) error = %v, want %v", err, pgx.ErrNoRows)
	}
}
