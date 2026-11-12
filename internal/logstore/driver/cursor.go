package driver

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
)

// CursorFormat is how a log list cursor writes the position of the record it
// points at. The value is the version number the cursor carries.
//
// The time and id formats write "{unix time}::{id}", the order of the lists,
// and differ in the precision of the time. A log store issues the one that
// matches the precision it orders by: a coarser one skips or repeats records
// that are closer together.
type CursorFormat int

const (
	// CursorFormatTimeIDMillis writes "{unix milliseconds}::{id}".
	CursorFormatTimeIDMillis CursorFormat = 1
	// CursorFormatTimeIDMicros writes "{unix microseconds}::{id}".
	CursorFormatTimeIDMicros CursorFormat = 2
)

var cursorFormats = []CursorFormat{CursorFormatTimeIDMillis, CursorFormatTimeIDMicros}

func (f CursorFormat) formatTime(t time.Time) int64 {
	if f == CursorFormatTimeIDMillis {
		return t.UnixMilli()
	}
	return t.UnixMicro()
}

func (f CursorFormat) parseTime(s string) (time.Time, error) {
	if f == CursorFormatTimeIDMillis {
		timeMs, err := cursor.ParseTimeMs(s)
		return time.UnixMilli(timeMs), err
	}
	timeMicro, err := cursor.ParseTimeMicro(s)
	return time.UnixMicro(timeMicro), err
}

// Cursor encodes and decodes the cursors of one log list.
type Cursor struct {
	resource string
	format   CursorFormat
}

// NewCursor returns the cursors of a list: resource names the list and format
// is the one the store issues.
func NewCursor(resource string, format CursorFormat) Cursor {
	return Cursor{resource: resource, format: format}
}

// Encode returns the cursor that points at a record, in the store's format.
func (c Cursor) Encode(t time.Time, id string) string {
	position := fmt.Sprintf("%d::%s", c.format.formatTime(t), id)
	return cursor.Encode(c.resource, int(c.format), position)
}

// Decode returns the position a cursor points at, as CursorPosition writes it.
// It reads a cursor of any format, whichever one the store issues.
// The error wraps cursor.ErrInvalidCursor when the cursor cannot be read.
func (c Cursor) Decode(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	// The store's own format first, so the error for an unknown one names it.
	formats := append([]CursorFormat{c.format}, cursorFormats...)
	var mismatch error
	for _, format := range formats {
		data, err := cursor.Decode(encoded, c.resource, int(format))
		if errors.Is(err, cursor.ErrVersionMismatch) {
			if mismatch == nil {
				mismatch = err
			}
			continue
		}
		if err != nil {
			return "", err
		}
		ts, id, err := cutCursorPosition(data)
		if err != nil {
			return "", err
		}
		t, err := format.parseTime(ts)
		if err != nil {
			return "", err
		}
		return CursorPosition(t, id), nil
	}
	return "", mismatch
}

// CursorPosition returns the position of a record in a list ordered by time
// and id. It is the same for every cursor format: "{unix microseconds}::{id}".
func CursorPosition(t time.Time, id string) string {
	return fmt.Sprintf("%d::%s", t.UnixMicro(), id)
}

// ParseCursorPosition reads a position written by CursorPosition.
// The error wraps cursor.ErrInvalidCursor when the position cannot be read.
func ParseCursorPosition(position string) (t time.Time, id string, err error) {
	ts, id, err := cutCursorPosition(position)
	if err != nil {
		return time.Time{}, "", err
	}
	t, err = CursorFormatTimeIDMicros.parseTime(ts)
	if err != nil {
		return time.Time{}, "", err
	}
	return t, id, nil
}

func cutCursorPosition(position string) (ts, id string, err error) {
	ts, id, found := strings.Cut(position, "::")
	if !found || ts == "" || id == "" {
		return "", "", cursor.ErrInvalidCursor
	}
	return ts, id, nil
}
