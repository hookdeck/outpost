package scheduler

import (
	"crypto/sha256"
	"math/big"
	"strings"
)

// generateRSMQID creates a valid RSMQ ID from a task identifier.
// The resulting ID will be deterministic (same input = same output)
// and follow RSMQ's format requirements:
// - 10 chars base36 timestamp prefix
// - 22 chars base36 encoded hash suffix
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
