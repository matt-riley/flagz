// Fuzz / property-based tests for the gRPC wire mapper.
// Uses the white-box package (package grpc) to reach unexported symbols.
package grpc

import (
	"encoding/json"
	"testing"

	flagz "github.com/matt-riley/flagz/clients/go"
	flagspb "github.com/matt-riley/flagz/api/proto/v1"
)

// FuzzProtoToFlag ensures protoToFlag never panics on arbitrary JSON bytes
// in the variants_json and rules_json fields.
func FuzzProtoToFlag(f *testing.F) {
	f.Add([]byte(`{"beta":true}`), []byte(`[{"attribute":"env","operator":"equals","value":"prod"}]`))
	f.Add([]byte(`{}`), []byte(`[]`))
	f.Add([]byte(nil), []byte(nil))
	f.Add([]byte(`{invalid`), []byte(`{invalid`))
	f.Add([]byte(`null`), []byte(`null`))
	f.Add([]byte(`{"a":true,"b":false,"c":true}`), []byte(`[{"attribute":"x","operator":"in","value":["y","z"]}]`))

	f.Fuzz(func(t *testing.T, variantsJSON []byte, rulesJSON []byte) {
		p := &flagspb.Flag{
			Key:          "fuzz-flag",
			Description:  "fuzz",
			Enabled:      true,
			VariantsJson: variantsJSON,
			RulesJson:    rulesJSON,
		}
		// Must never panic.
		f, err := protoToFlag(p)
		if err != nil {
			return // decode errors are acceptable; panics are not
		}
		// Invariant: key is always preserved.
		if f.Key != p.Key {
			t.Errorf("key mismatch: got %q, want %q", f.Key, p.Key)
		}
	})
}

// FuzzFlagToProtoRoundTrip verifies the encode→decode roundtrip preserves
// key, enabled, variants, and rules for arbitrary inputs.
func FuzzFlagToProtoRoundTrip(f *testing.F) {
	variantsA, _ := json.Marshal(map[string]bool{"beta": true, "alpha": false})
	rulesA, _ := json.Marshal([]map[string]any{{"attribute": "env", "operator": "equals", "value": "prod"}})
	f.Add("flag-a", true, variantsA, rulesA)
	f.Add("", false, []byte(nil), []byte(nil))
	f.Add("x", true, []byte(`{}`), []byte(`[]`))

	f.Fuzz(func(t *testing.T, key string, enabled bool, variantsJSON []byte, rulesJSON []byte) {
		// Build a domain Flag from fuzz inputs, using the variants/rules from the proto path.
		srcProto := &flagspb.Flag{
			Key:          key,
			Enabled:      enabled,
			VariantsJson: variantsJSON,
			RulesJson:    rulesJSON,
		}
		src, err := protoToFlag(srcProto)
		if err != nil {
			return // bad inputs that fail to decode; skip
		}

		// Encode back to proto and decode again.
		encoded, err := flagToProto(src)
		if err != nil {
			t.Fatalf("flagToProto failed unexpectedly: %v", err)
		}
		decoded, err := protoToFlag(encoded)
		if err != nil {
			t.Fatalf("protoToFlag(flagToProto(...)) failed: %v", err)
		}

		// Invariants after roundtrip.
		if decoded.Key != key {
			t.Errorf("key: got %q, want %q", decoded.Key, key)
		}
		if decoded.Enabled != enabled {
			t.Errorf("enabled: got %v, want %v", decoded.Enabled, enabled)
		}
		if len(src.Variants) > 0 {
			for k, v := range src.Variants {
				if decoded.Variants[k] != v {
					t.Errorf("variant[%q]: got %v, want %v", k, decoded.Variants[k], v)
				}
			}
		}
		if len(src.Rules) > 0 {
			if len(decoded.Rules) != len(src.Rules) {
				t.Errorf("rules length: got %d, want %d", len(decoded.Rules), len(src.Rules))
			}
		}
	})
}

// FuzzFlagToProtoNoPanic ensures flagToProto never panics on arbitrary Flag inputs.
func FuzzFlagToProtoNoPanic(f *testing.F) {
	f.Add("key", true)
	f.Add("", false)

	f.Fuzz(func(t *testing.T, key string, enabled bool) {
		flag := flagz.Flag{
			Key:     key,
			Enabled: enabled,
			Variants: map[string]bool{
				key: enabled,
			},
			Rules: []flagz.Rule{
				{Attribute: key, Operator: "equals", Value: key},
			},
		}
		// Must not panic.
		_, _ = flagToProto(flag)
	})
}
