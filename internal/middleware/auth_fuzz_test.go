package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func FuzzParseBearerToken(f *testing.F) {
	f.Add("Bearer token")
	f.Add("bearer value")
	f.Add("Basic value")
	f.Add("")
	f.Add("Bearer")

	f.Fuzz(func(t *testing.T, authorizationHeader string) {
		token, err := parseBearerToken(authorizationHeader)
		parts := strings.Fields(authorizationHeader)
		expectOK := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != ""

		if expectOK {
			if err != nil {
				t.Fatalf("parseBearerToken(%q) error = %v, want nil", authorizationHeader, err)
			}
			if token != parts[1] {
				t.Fatalf("parseBearerToken(%q) token = %q, want %q", authorizationHeader, token, parts[1])
			}
			return
		}

		if err == nil {
			t.Fatalf("parseBearerToken(%q) error = nil, want non-nil", authorizationHeader)
		}
	})
}

func FuzzAPIKeyMatchesHash(f *testing.F) {
	validHash, err := HashAPIKey("seed-secret")
	if err != nil {
		f.Fatalf("HashAPIKey(seed-secret) error = %v", err)
	}

	legacySum := sha256.Sum256([]byte("legacy-secret"))
	legacyHash := hex.EncodeToString(legacySum[:])

	f.Add(validHash, "seed-secret")
	f.Add(validHash, "wrong-secret")
	f.Add(legacyHash, "legacy-secret")
	f.Add("not-hex", "secret")

	f.Fuzz(func(t *testing.T, expectedHash, apiKey string) {
		_ = APIKeyMatchesHash(expectedHash, apiKey)

		if expectedHash == validHash && apiKey == "seed-secret" && !APIKeyMatchesHash(expectedHash, apiKey) {
			t.Fatalf("expected bcrypt hash to match seed secret")
		}
		if expectedHash == legacyHash && apiKey == "legacy-secret" && !APIKeyMatchesHash(expectedHash, apiKey) {
			t.Fatalf("expected legacy hash to match seed secret")
		}
	})
}
