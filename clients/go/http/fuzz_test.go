// Fuzz / property-based tests for the SSE parser and HTTP wire mapping.
// Uses the white-box package (package http) to reach unexported symbols.
package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	flagz "github.com/matt-riley/flagz/clients/go"
)

// runParseSSE runs the SSE parser on b and collects all emitted events.
// Draining the channel prevents goroutine leaks in corpus-mode runs.
func runParseSSE(b []byte) []flagz.FlagEvent {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan flagz.FlagEvent, 256)
	go func() {
		defer close(ch)
		br := bufio.NewReaderSize(bytes.NewReader(b), 1<<20)
		parseSSE(ctx, br, ch)
	}()
	var evs []flagz.FlagEvent
	for e := range ch {
		evs = append(evs, e)
	}
	return evs
}

// FuzzParseSSE ensures the SSE parser never panics on arbitrary input and
// produces no more events than blank lines in the input (upper bound).
func FuzzParseSSE(f *testing.F) {
	f.Add([]byte("id:1\nevent:update\ndata:{\"key\":\"x\",\"enabled\":true}\n\n"))
	f.Add([]byte("id:2\nevent:delete\ndata:{\"key\":\"x\"}\n\n"))
	f.Add([]byte("event:update\ndata:first\ndata:second\n\n"))
	f.Add([]byte(":comment\ndata:hello\n\n"))
	f.Add([]byte("\n\n"))
	f.Add([]byte(""))
	f.Add([]byte("id:9999999999\nevent:update\ndata:{}\n\n"))
	f.Add([]byte(strings.Repeat("data:x\n", 1000) + "\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		evs := runParseSSE(data)
		// Upper-bound sanity: events ≤ number of blank lines in input.
		blankLines := bytes.Count(data, []byte("\n\n"))
		if len(evs) > blankLines+1 {
			t.Errorf("got %d events from input with %d blank lines", len(evs), blankLines)
		}
	})
}

// FuzzDecodeFlag ensures decodeFlag never panics on arbitrary JSON input.
func FuzzDecodeFlag(f *testing.F) {
	mustMarshalWire := func(wf wireFlag) []byte {
		b, _ := json.Marshal(wf)
		return b
	}
	f.Add(mustMarshalWire(wireFlag{Key: "x", Enabled: true}))
	f.Add(mustMarshalWire(wireFlag{
		Key:       "y",
		Enabled:   false,
		Variants:  json.RawMessage(`{"beta":true}`),
		Rules:     json.RawMessage(`[{"attribute":"env","operator":"equals","value":"prod"}]`),
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "not-a-date",
	}))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"key":"","variants":"broken","rules":42}`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		var wf wireFlag
		if err := json.Unmarshal(raw, &wf); err != nil {
			return // skip non-JSON
		}
		f, err := decodeFlag(wf)
		if err != nil {
			return // decode errors are fine; panics are not
		}
		// Invariant: decoded key always equals wire key.
		if f.Key != wf.Key {
			t.Errorf("key mismatch: got %q, want %q", f.Key, wf.Key)
		}
		// Invariant: if CreatedAt parses, it must be non-zero.
		if wf.CreatedAt != "" {
			if _, parseErr := time.Parse(time.RFC3339, wf.CreatedAt); parseErr == nil {
				if f.CreatedAt.IsZero() {
					t.Errorf("expected non-zero CreatedAt for input %q", wf.CreatedAt)
				}
			}
		}
	})
}

// FuzzEncodeDecodeFlag verifies encodeFlag/decodeFlag roundtrip: key and
// enabled are preserved for any valid string key and boolean.
func FuzzEncodeDecodeFlag(f *testing.F) {
	f.Add("my-flag", true)
	f.Add("", false)
	f.Add("flag/with/slashes", true)
	f.Add(strings.Repeat("a", 512), false)

	f.Fuzz(func(t *testing.T, key string, enabled bool) {
		orig := flagz.Flag{Key: key, Enabled: enabled}
		wire, err := encodeFlag(orig)
		if err != nil {
			t.Fatalf("encodeFlag(%q, %v) failed: %v", key, enabled, err)
		}
		decoded, err := decodeFlag(wire)
		if err != nil {
			t.Fatalf("decodeFlag after encodeFlag failed: %v", err)
		}
		if decoded.Key != key {
			t.Errorf("key: got %q, want %q", decoded.Key, key)
		}
		if decoded.Enabled != enabled {
			t.Errorf("enabled: got %v, want %v", decoded.Enabled, enabled)
		}
	})
}
