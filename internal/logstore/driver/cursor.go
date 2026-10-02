package driver

import (
	"strings"

	"github.com/hookdeck/outpost/internal/cursor"
)

// ParseCursorPosition reads a "{unix milliseconds}::{id}" position.
// The error wraps cursor.ErrInvalidCursor when the position cannot be read.
func ParseCursorPosition(position string) (timeMs int64, id string, err error) {
	ts, id, found := strings.Cut(position, "::")
	if !found || ts == "" || id == "" {
		return 0, "", cursor.ErrInvalidCursor
	}
	timeMs, err = cursor.ParseTimeMs(ts)
	if err != nil {
		return 0, "", err
	}
	return timeMs, id, nil
}
