package driver

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
)

// A cursor timestamp has to be a time the API can express: an RFC 3339 year
// (0000-9999), with a day of margin for UTC offsets.
var (
	minCursorTimeMs = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC).Add(-24 * time.Hour).UnixMilli()
	maxCursorTimeMs = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC).Add(24 * time.Hour).UnixMilli()
)

// ParseCursorPosition reads a "{unix milliseconds}::{id}" position.
// The error wraps cursor.ErrInvalidCursor when the position cannot be read.
func ParseCursorPosition(position string) (timeMs int64, id string, err error) {
	ts, id, found := strings.Cut(position, "::")
	if !found || ts == "" || id == "" {
		return 0, "", cursor.ErrInvalidCursor
	}
	timeMs, err = strconv.ParseInt(ts, 10, 64)
	if err != nil || timeMs < minCursorTimeMs || timeMs > maxCursorTimeMs {
		return 0, "", fmt.Errorf("%w: invalid timestamp", cursor.ErrInvalidCursor)
	}
	return timeMs, id, nil
}
