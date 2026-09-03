// Package identifier creates opaque application identifiers.
package identifier

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// New returns a prefix followed by 128 bits of cryptographically random data.
func New(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate %s identifier: %w", prefix, err)
	}
	return prefix + hex.EncodeToString(bytes), nil
}

// Valid reports whether value is a New-generated identifier for prefix.
func Valid(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+32 {
		return false
	}
	_, err := hex.DecodeString(value[len(prefix):])
	return err == nil
}
