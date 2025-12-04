package cursor

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// cursorVersion is the current cursor format version.
// Increment this when making breaking changes to cursor format.
const cursorVersion = "v1"

// Cursor represents a pagination cursor with embedded sort parameters.
// This ensures cursors are only valid for queries with matching sort configuration.
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
// Format: v1:{sortBy}:{sortOrder}:{position} -> base62 encoded
func Encode(c Cursor) string {
	raw := fmt.Sprintf("%s:%s:%s:%s", cursorVersion, c.SortBy, c.SortOrder, c.Position)
	num := new(big.Int)
	num.SetBytes([]byte(raw))
	return num.Text(62)
}

// Decode converts a base62 encoded cursor string back to a Cursor.
// Returns driver.ErrInvalidCursor if the cursor is malformed or has an unsupported version.
func Decode(encoded string) (Cursor, error) {
	if encoded == "" {
		return Cursor{}, nil
	}

	num := new(big.Int)
	num, ok := num.SetString(encoded, 62)
	if !ok {
		return Cursor{}, driver.ErrInvalidCursor
	}

	raw := string(num.Bytes())
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) != 4 {
		return Cursor{}, driver.ErrInvalidCursor
	}

	version := parts[0]
	sortBy := parts[1]
	sortOrder := parts[2]
	position := parts[3]

	// Validate version
	if version != cursorVersion {
		return Cursor{}, fmt.Errorf("%w: unsupported cursor version %q", driver.ErrInvalidCursor, version)
	}

	// Validate sortBy
	if sortBy != "event_time" && sortBy != "delivery_time" {
		return Cursor{}, driver.ErrInvalidCursor
	}

	// Validate sortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		return Cursor{}, driver.ErrInvalidCursor
	}

	return Cursor{
		SortBy:    sortBy,
		SortOrder: sortOrder,
		Position:  position,
	}, nil
}

// Validate checks if the cursor matches the expected sort parameters.
// Returns driver.ErrInvalidCursor if there's a mismatch.
func Validate(c Cursor, expectedSortBy, expectedSortOrder string) error {
	if c.IsEmpty() {
		// Empty cursor is always valid
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
// Returns the decoded cursors or an error if either cursor is invalid or mismatched.
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
