package scheduler

import (
	"crypto/sha256"
	"math/big"
	"strings"
)

// generateRSMQID creates a valid RSMQ ID from a task identifier.
// The resulting ID will be:
// - Deterministic (same input = same output)
// - 32 characters long
// - Alphanumeric (base36)
// - Composed of:
//   - First 10 chars: fixed "0000000000" (timestamp part, not used for ordering)
//   - Last 22 chars: base36 encoded SHA-256 hash of the input
//
// Note: The timestamp part is fixed because we use Redis's sorted set scores
// for timing, not the ID's timestamp. This ensures that overriding a message
// with the same ID but different delay works correctly. The scheduler package
// does not use msg.Sent for timing as it can be inaccurate for overridden
// messages.
func generateRSMQID(taskID string) string {
	// First 10 chars: fixed timestamp in base36
	// Using 0 as timestamp since we don't use it for ordering
	timestampPart := "0000000000"

	// Remaining 22 chars: hash of taskID encoded in base36
	h := sha256.New()
	h.Write([]byte(taskID))
	hash := h.Sum(nil)

	// Convert hash to a big integer
	num := new(big.Int).SetBytes(hash)

	// Convert to base36. SHA-256 will always produce enough bits
	// to generate at least 22 base36 chars
	hashPart := strings.ToUpper(num.Text(36))[:22]

	return timestampPart + hashPart
}
