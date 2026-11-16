package driver_test

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCursor(t *testing.T) {
	at := time.Date(2024, 1, 15, 9, 30, 0, 123456000, time.UTC)

	t.Run("a cursor carries its format as the version", func(t *testing.T) {
		millis := driver.NewCursor("evt", driver.CursorFormatTimeIDMillis)
		micros := driver.NewCursor("evt", driver.CursorFormatTimeIDMicros)
		assert.Equal(t, cursor.Encode("evt", 1, "1705311000123::evt_1"), millis.Encode(at, "evt_1"))
		assert.Equal(t, cursor.Encode("evt", 2, "1705311000123456::evt_1"), micros.Encode(at, "evt_1"))
	})

	t.Run("a cursor decodes to the position of its record", func(t *testing.T) {
		micros := driver.NewCursor("evt", driver.CursorFormatTimeIDMicros)
		position, err := micros.Decode(micros.Encode(at, "evt_1"))
		require.NoError(t, err)
		assert.Equal(t, driver.CursorPosition(at, "evt_1"), position)

		// Milliseconds are all a millisecond cursor keeps of the time.
		millis := driver.NewCursor("evt", driver.CursorFormatTimeIDMillis)
		position, err = millis.Decode(millis.Encode(at, "evt_1"))
		require.NoError(t, err)
		assert.Equal(t, driver.CursorPosition(at.Truncate(time.Millisecond), "evt_1"), position)
	})

	t.Run("a store reads the cursors of every format", func(t *testing.T) {
		for _, issued := range []driver.CursorFormat{driver.CursorFormatTimeIDMillis, driver.CursorFormatTimeIDMicros} {
			store := driver.NewCursor("evt", issued)

			position, err := store.Decode(cursor.Encode("evt", 1, "1700000000000::a::b"))
			require.NoError(t, err)
			assert.Equal(t, "1700000000000000::a::b", position)

			position, err = store.Decode(cursor.Encode("evt", 2, "1700000000000123::a::b"))
			require.NoError(t, err)
			assert.Equal(t, "1700000000000123::a::b", position)
		}
	})

	t.Run("empty cursor is an empty position", func(t *testing.T) {
		position, err := driver.NewCursor("evt", driver.CursorFormatTimeIDMicros).Decode("")
		require.NoError(t, err)
		assert.Empty(t, position)
	})

	t.Run("unreadable cursor is an invalid cursor", func(t *testing.T) {
		const (
			invalidCursor    = "invalid cursor"
			invalidTimestamp = "invalid cursor: invalid timestamp"
		)
		cases := []struct {
			name    string
			encoded string
			message string
		}{
			// The message names the format the store issues.
			{"unknown format", cursor.Encode("evt", 3, "1700000000000123::evt_1"), "invalid cursor: cursor version mismatch: expected version 02"},
			{"another resource", cursor.Encode("att", 2, "1700000000000123::att_1"), invalidCursor},
			{"empty position", cursor.Encode("evt", 2, ""), invalidCursor},
			{"only a timestamp", cursor.Encode("evt", 2, "1700000000000123"), invalidCursor},
			{"no id", cursor.Encode("evt", 2, "1700000000000123::"), invalidCursor},
			{"timestamp that is not a number", cursor.Encode("evt", 2, "yesterday::evt_1"), invalidTimestamp},
			{"timestamp after the year 9999", cursor.Encode("evt", 2, "9223372036854775807::evt_1"), invalidTimestamp},
			{"milliseconds, empty position", cursor.Encode("evt", 1, ""), invalidCursor},
			{"milliseconds, timestamp that is not a number", cursor.Encode("evt", 1, "yesterday::evt_1"), invalidTimestamp},
			// In range as microseconds, after the year 9999 as milliseconds.
			{"milliseconds, timestamp after the year 9999", cursor.Encode("evt", 1, "1700000000000123::evt_1"), invalidTimestamp},
		}
		store := driver.NewCursor("evt", driver.CursorFormatTimeIDMicros)
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := store.Decode(tc.encoded)
				require.ErrorIs(t, err, cursor.ErrInvalidCursor)
				assert.EqualError(t, err, tc.message)
			})
		}
	})
}

func TestParseCursorPosition(t *testing.T) {
	t.Run("readable position", func(t *testing.T) {
		cases := []struct {
			name      string
			position  string
			timeMicro int64
			id        string
		}{
			{"timestamp and id", "1700000000000123::evt_1", 1700000000000123, "evt_1"},
			{"id that contains the separator", "1700000000000123::a::b", 1700000000000123, "a::b"},
			{"timestamp before 1970", "-1::evt_1", -1, "evt_1"},
			{"timestamp of zero", "0::evt_1", 0, "evt_1"},
			{"year 0000", "-62167219200000000::evt_1", -62167219200000000, "evt_1"},
			{"year 9999", "253402300799999999::evt_1", 253402300799999999, "evt_1"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, id, err := driver.ParseCursorPosition(tc.position)
				require.NoError(t, err)
				assert.Equal(t, tc.timeMicro, got.UnixMicro())
				assert.Equal(t, tc.id, id)
			})
		}
	})

	t.Run("unreadable position is an invalid cursor", func(t *testing.T) {
		const (
			invalidCursor    = "invalid cursor"
			invalidTimestamp = "invalid cursor: invalid timestamp"
		)
		cases := []struct {
			name     string
			position string
			message  string
		}{
			{"empty", "", invalidCursor},
			{"one word", "yesterday", invalidCursor},
			{"only a timestamp", "1700000000000123", invalidCursor},
			{"another separator", "1700000000000123_evt_1", invalidCursor},
			{"no timestamp", "::evt_1", invalidCursor},
			{"no id", "1700000000000123::", invalidCursor},
			{"timestamp that is not a number", "yesterday::evt_1", invalidTimestamp},
			{"timestamp with a decimal", "1700000000000123.5::evt_1", invalidTimestamp},
			{"timestamp larger than an integer", "99999999999999999999::evt_1", invalidTimestamp},
			{"timestamp after the year 9999", "9223372036854775807::evt_1", invalidTimestamp},
			{"timestamp before the year 0000", "-9223372036854775808::evt_1", invalidTimestamp},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, _, err := driver.ParseCursorPosition(tc.position)
				require.ErrorIs(t, err, cursor.ErrInvalidCursor)
				assert.EqualError(t, err, tc.message)
			})
		}
	})
}
