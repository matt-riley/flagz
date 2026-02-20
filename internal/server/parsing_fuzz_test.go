package server

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func FuzzParseLastEventID(f *testing.F) {
	f.Add("")
	f.Add("0")
	f.Add("42")
	f.Add("-1")
	f.Add("not-a-number")
	f.Add("  7  ")

	f.Fuzz(func(t *testing.T, value string) {
		got, err := parseLastEventID(value)
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			if err != nil || got != 0 {
				t.Fatalf("parseLastEventID(%q) = (%d, %v), want (0, nil)", value, got, err)
			}
			return
		}

		want, parseErr := strconv.ParseInt(trimmed, 10, 64)
		expectErr := parseErr != nil || want < 0
		if expectErr {
			if err == nil {
				t.Fatalf("parseLastEventID(%q) error = nil, want non-nil", value)
			}
			return
		}

		if err != nil || got != want {
			t.Fatalf("parseLastEventID(%q) = (%d, %v), want (%d, nil)", value, got, err, want)
		}
	})
}

func FuzzParseListPageToken(f *testing.F) {
	f.Add("", 0)
	f.Add("0", 0)
	f.Add("10", 100)
	f.Add("-1", 100)
	f.Add("not-a-number", 100)

	f.Fuzz(func(t *testing.T, pageToken string, rawMaxOffset int) {
		maxOffset := int(uint(rawMaxOffset) % 100000)
		got, err := parseListPageToken(pageToken, maxOffset)

		trimmed := strings.TrimSpace(pageToken)
		if trimmed == "" {
			if err != nil || got != 0 {
				t.Fatalf("parseListPageToken(%q, %d) = (%d, %v), want (0, nil)", pageToken, maxOffset, got, err)
			}
			return
		}

		want, parseErr := strconv.Atoi(trimmed)
		expectErr := parseErr != nil || want < 0 || want > maxOffset
		if expectErr {
			if err == nil {
				t.Fatalf("parseListPageToken(%q, %d) error = nil, want non-nil", pageToken, maxOffset)
			}
			return
		}

		if err != nil || got != want {
			t.Fatalf("parseListPageToken(%q, %d) = (%d, %v), want (%d, nil)", pageToken, maxOffset, got, err, want)
		}
	})
}

func FuzzCompactSSEPayload(f *testing.F) {
	f.Add([]byte(`{"key":"new-ui","enabled":true}`))
	f.Add([]byte("{\n  \"key\": \"new-ui\",\n  \"enabled\": true\n}"))
	f.Add([]byte("line1\nline2"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, payload []byte) {
		lines := compactSSEPayload(payload)
		if len(lines) == 0 {
			t.Fatal("compactSSEPayload returned no lines")
		}

		var builder strings.Builder
		if err := writeSSEEvent(&builder, 1, "update", payload); err != nil {
			t.Fatalf("writeSSEEvent() error = %v", err)
		}
		body := builder.String()
		if !strings.HasPrefix(body, "id: 1\nevent: update\n") {
			t.Fatalf("unexpected SSE prefix: %q", body)
		}

		var compact bytes.Buffer
		if err := json.Compact(&compact, payload); err == nil {
			if len(lines) != 1 || lines[0] != compact.String() {
				t.Fatalf("compactSSEPayload valid json mismatch: got %#v want %q", lines, compact.String())
			}
		}
	})
}
