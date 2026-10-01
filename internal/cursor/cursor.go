// Package cursor provides a unified cursor encoding/decoding utility for pagination.
// Cursors are versioned and resource-scoped, allowing different parts of the system
// to use cursors without collision.
package cursor

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidCursor indicates the cursor is malformed or cannot be decoded.
	ErrInvalidCursor = errors.New("invalid cursor")

	// ErrVersionMismatch indicates the cursor version doesn't match the expected version.
	ErrVersionMismatch = errors.New("cursor version mismatch")
)

// A cursor timestamp has to be a time the API can express: an RFC 3339 year
// (0000-9999), with a day of margin for UTC offsets.
var (
	minTimeMs = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC).Add(-24 * time.Hour).UnixMilli()
	maxTimeMs = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC).Add(24 * time.Hour).UnixMilli()
)

// ParseTimeMs reads the unix milliseconds timestamp of a cursor position.
// The error wraps ErrInvalidCursor.
func ParseTimeMs(s string) (int64, error) {
	timeMs, err := strconv.ParseInt(s, 10, 64)
	if err != nil || timeMs < minTimeMs || timeMs > maxTimeMs {
		return 0, fmt.Errorf("%w: invalid timestamp", ErrInvalidCursor)
	}
	return timeMs, nil
}

// Base62Encode encodes a string to base62.
func Base62Encode(s string) string {
	if s == "" {
		return ""
	}
	num := new(big.Int)
	num.SetBytes([]byte(s))
	return num.Text(62)
}

// Base62Decode decodes a base62 string.
func Base62Decode(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	num := new(big.Int)
	num, ok := num.SetString(s, 62)
	if !ok {
		return "", ErrInvalidCursor
	}
	return string(num.Bytes()), nil
}

// Encode creates a versioned cursor string.
// Format: {resource}v{version:02d}:{data}, then base62 encoded.
// Example: "evtv01:position_data" -> base62
func Encode(resource string, version int, data string) string {
	raw := fmt.Sprintf("%sv%02d:%s", resource, version, data)
	return Base62Encode(raw)
}

// Decode decodes and validates a cursor string.
// Returns the data portion if the cursor matches the expected resource and version.
// Returns ErrInvalidCursor if the cursor is malformed.
// Returns an error wrapping both ErrInvalidCursor and ErrVersionMismatch if the version doesn't match.
func Decode(encoded string, resource string, version int) (string, error) {
	if encoded == "" {
		return "", nil
	}

	raw, err := Base62Decode(encoded)
	if err != nil {
		return "", err
	}

	// Expected prefix: {resource}v{version:02d}:
	expectedPrefix := fmt.Sprintf("%sv%02d:", resource, version)

	if !strings.HasPrefix(raw, expectedPrefix) {
		// Check if it's a version mismatch vs completely invalid
		resourcePrefix := resource + "v"
		if strings.HasPrefix(raw, resourcePrefix) {
			// Has correct resource but wrong version
			return "", fmt.Errorf("%w: %w: expected version %02d", ErrInvalidCursor, ErrVersionMismatch, version)
		}
		return "", ErrInvalidCursor
	}

	return raw[len(expectedPrefix):], nil
}
