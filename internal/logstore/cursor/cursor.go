package cursor

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// Cursor represents a pagination cursor with embedded sort parameters.
// This ensures cursors are only valid for queries with matching sort configuration.
// Implementations use this type directly - versioning is handled internally by Encode/Decode.
type Cursor struct {
	SortBy    string // "event_time" or "delivery_time"
	SortOrder string // "asc" or "desc"
	Position  string // implementation-specific position value
}

// IsEmpty returns true if this cursor has no position (i.e., no cursor was provided).
func (c Cursor) IsEmpty() bool {
	return c.Position == ""
}

// Encode converts a Cursor to a URL-safe base62 string.
// Always encodes using the current version format.
func Encode(c Cursor) string {
	return encodeV1(c)
}

// Decode converts a base62 encoded cursor string back to a Cursor.
// Automatically detects and handles different cursor versions.
// Returns driver.ErrInvalidCursor if the cursor is malformed.
func Decode(encoded string) (Cursor, error) {
	if encoded == "" {
		return Cursor{}, nil
	}

	raw, err := decodeBase62(encoded)
	if err != nil {
		return Cursor{}, err
	}

	// Detect version and decode accordingly
	if strings.HasPrefix(raw, v1Prefix) {
		return decodeV1(raw)
	}

	// Fall back to v0 format (legacy)
	return decodeV0(raw)
}

// Validate checks if the cursor matches the expected sort parameters.
// Returns driver.ErrInvalidCursor if there's a mismatch.
func Validate(c Cursor, expectedSortBy, expectedSortOrder string) error {
	if c.IsEmpty() {
		return nil
	}

	if c.SortBy != expectedSortBy {
		return fmt.Errorf("%w: cursor sortBy %q does not match request sortBy %q",
			driver.ErrInvalidCursor, c.SortBy, expectedSortBy)
	}

	if c.SortOrder != expectedSortOrder {
		return fmt.Errorf("%w: cursor sortOrder %q does not match request sortOrder %q",
			driver.ErrInvalidCursor, c.SortOrder, expectedSortOrder)
	}

	return nil
}

// DecodeAndValidate is a helper that decodes and validates both Next and Prev cursors.
// This is the common pattern used by all LogStore implementations.
func DecodeAndValidate(next, prev, sortBy, sortOrder string) (nextCursor, prevCursor Cursor, err error) {
	if next != "" {
		nextCursor, err = Decode(next)
		if err != nil {
			return Cursor{}, Cursor{}, err
		}
		if err := Validate(nextCursor, sortBy, sortOrder); err != nil {
			return Cursor{}, Cursor{}, err
		}
	}
	if prev != "" {
		prevCursor, err = Decode(prev)
		if err != nil {
			return Cursor{}, Cursor{}, err
		}
		if err := Validate(prevCursor, sortBy, sortOrder); err != nil {
			return Cursor{}, Cursor{}, err
		}
	}
	return nextCursor, prevCursor, nil
}

// =============================================================================
// Internal: Base62 encoding/decoding
// =============================================================================

func encodeBase62(raw string) string {
	num := new(big.Int)
	num.SetBytes([]byte(raw))
	return num.Text(62)
}

func decodeBase62(encoded string) (string, error) {
	num := new(big.Int)
	num, ok := num.SetString(encoded, 62)
	if !ok {
		return "", driver.ErrInvalidCursor
	}
	return string(num.Bytes()), nil
}

// =============================================================================
// Internal: v1 cursor format
// Format: v1:{sortBy}:{sortOrder}:{position}
// =============================================================================

const v1Prefix = "v1:"

func encodeV1(c Cursor) string {
	raw := fmt.Sprintf("v1:%s:%s:%s", c.SortBy, c.SortOrder, c.Position)
	return encodeBase62(raw)
}

func decodeV1(raw string) (Cursor, error) {
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) != 4 {
		return Cursor{}, driver.ErrInvalidCursor
	}

	sortBy := parts[1]
	sortOrder := parts[2]
	position := parts[3]

	if sortBy != "event_time" && sortBy != "delivery_time" {
		return Cursor{}, driver.ErrInvalidCursor
	}

	if sortOrder != "asc" && sortOrder != "desc" {
		return Cursor{}, driver.ErrInvalidCursor
	}

	if position == "" {
		return Cursor{}, driver.ErrInvalidCursor
	}

	return Cursor{
		SortBy:    sortBy,
		SortOrder: sortOrder,
		Position:  position,
	}, nil
}

// =============================================================================
// Internal: v0 cursor format (legacy, backward compatibility)
// Format: {position} (no version prefix, no sort params)
// Defaults: sortBy=event_time, sortOrder=desc
// =============================================================================

const (
	v0DefaultSortBy    = "event_time"
	v0DefaultSortOrder = "desc"
)

func decodeV0(raw string) (Cursor, error) {
	// v0 cursors are just the position, no validation needed
	// If position is invalid, the DB query will simply not find it
	return Cursor{
		SortBy:    v0DefaultSortBy,
		SortOrder: v0DefaultSortOrder,
		Position:  raw,
	}, nil
}
