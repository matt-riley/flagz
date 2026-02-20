package service

import (
	"encoding/json"
	"errors"
	"testing"
)

func FuzzParseRulesJSON(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte(`[]`))
	f.Add([]byte(`[{"attribute":"country","operator":"equals","value":"US"}]`))
	f.Add([]byte(`{"invalid":true}`))

	f.Fuzz(func(t *testing.T, payload []byte) {
		rules, err := parseRulesJSON(json.RawMessage(payload))
		if len(payload) == 0 {
			if err != nil || len(rules) != 0 {
				t.Fatalf("parseRulesJSON(empty) = (%v, %v), want (empty, nil)", rules, err)
			}
			return
		}

		if err != nil && !errors.Is(err, ErrInvalidRules) {
			t.Fatalf("parseRulesJSON(%q) error = %v, want ErrInvalidRules-wrapped error", payload, err)
		}
	})
}

func FuzzParseVariantsJSON(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte(`{"default":true}`))
	f.Add([]byte(`{"default":"bad"}`))
	f.Add([]byte(`{"default"`))

	f.Fuzz(func(t *testing.T, payload []byte) {
		err := parseVariantsJSON(json.RawMessage(payload))
		if len(payload) == 0 {
			if err != nil {
				t.Fatalf("parseVariantsJSON(empty) error = %v, want nil", err)
			}
			return
		}

		if err != nil && !errors.Is(err, ErrInvalidVariants) {
			t.Fatalf("parseVariantsJSON(%q) error = %v, want ErrInvalidVariants-wrapped error", payload, err)
		}
	})
}

func FuzzParseBooleanDefaultFromVariants(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte(`{"default":true}`))
	f.Add([]byte(`{"default":false}`))
	f.Add([]byte(`{"default":"nope"}`))
	f.Add([]byte(`{"default"`))

	f.Fuzz(func(t *testing.T, payload []byte) {
		result := parseBooleanDefaultFromVariants(json.RawMessage(payload))
		if result == nil {
			return
		}

		var variants map[string]any
		if err := json.Unmarshal(payload, &variants); err != nil {
			t.Fatalf("parseBooleanDefaultFromVariants returned non-nil for invalid json: %q", payload)
		}

		defaultValue, ok := variants["default"].(bool)
		if !ok {
			t.Fatalf("parseBooleanDefaultFromVariants returned non-nil for non-bool default: %#v", variants["default"])
		}
		if *result != defaultValue {
			t.Fatalf("parseBooleanDefaultFromVariants value = %v, want %v", *result, defaultValue)
		}
	})
}
