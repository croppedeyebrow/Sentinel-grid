// Package ids generates the identifiers the service assigns to new rows.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewUUID returns a random version 4 UUID in canonical form. The service
// assigns identifiers itself rather than letting the database do it, because
// the aggregate is built and validated before anything is written.
func NewUUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	bytes[6] = (bytes[6] & 0x0f) | 0x40 // version 4
	bytes[8] = (bytes[8] & 0x3f) | 0x80 // RFC 4122 variant

	encoded := make([]byte, 32)
	hex.Encode(encoded, bytes)

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]), nil
}
