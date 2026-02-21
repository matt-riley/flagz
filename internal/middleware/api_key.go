// Package middleware provides authentication middleware for the flagz HTTP and
// gRPC transports, including bearer-token validation, bcrypt-based API key
// hashing, and legacy SHA-256 hash support for backward compatibility.
package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const apiKeyHashCost = bcrypt.DefaultCost

// HashAPIKey returns a salted bcrypt hash for an API key.
func HashAPIKey(apiKey string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(apiKey), apiKeyHashCost)
	if err != nil {
		return "", fmt.Errorf("hash api key: %w", err)
	}
	return string(hash), nil
}

// APIKeyMatchesHash compares an API key against a stored hash.
// Legacy SHA-256 hashes remain supported for backward compatibility.
func APIKeyMatchesHash(expectedHash, apiKey string) bool {
	if err := bcrypt.CompareHashAndPassword([]byte(expectedHash), []byte(apiKey)); err == nil {
		return true
	}

	return legacyAPIKeyMatchesHash(expectedHash, apiKey)
}

func legacyAPIKeyMatchesHash(expectedHash, apiKey string) bool {
	expectedBytes, err := hex.DecodeString(expectedHash)
	if err != nil {
		return false
	}

	actual := sha256.Sum256([]byte(apiKey))
	if len(expectedBytes) != len(actual) {
		return false
	}

	return subtle.ConstantTimeCompare(expectedBytes, actual[:]) == 1
}
