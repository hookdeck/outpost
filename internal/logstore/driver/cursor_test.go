package driver_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCursorPosition(t *testing.T) {
	t.Run("readable position", func(t *testing.T) {
		cases := []struct {
			name     string
			position string
			timeMs   int64
			id       string
		}{
			{"timestamp and id", "1700000000000::evt_1", 1700000000000, "evt_1"},
			{"id that contains the separator", "1700000000000::a::b", 1700000000000, "a::b"},
			{"timestamp before 1970", "-1::evt_1", -1, "evt_1"},
			{"timestamp of zero", "0::evt_1", 0, "evt_1"},
			{"year 0000", "-62167219200000::evt_1", -62167219200000, "evt_1"},
			{"year 9999", "253402300799999::evt_1", 253402300799999, "evt_1"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				timeMs, id, err := driver.ParseCursorPosition(tc.position)
				require.NoError(t, err)
				assert.Equal(t, tc.timeMs, timeMs)
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
			{"only a timestamp", "1700000000000", invalidCursor},
			{"another separator", "1700000000000_evt_1", invalidCursor},
			{"no timestamp", "::evt_1", invalidCursor},
			{"no id", "1700000000000::", invalidCursor},
			{"timestamp that is not a number", "yesterday::evt_1", invalidTimestamp},
			{"timestamp with a decimal", "1700000000000.5::evt_1", invalidTimestamp},
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

func TestDecodeCursor(t *testing.T) {
	t.Run("returns the position of a readable cursor", func(t *testing.T) {
		position, err := driver.DecodeCursor(cursor.Encode("evt", 1, "1700000000000::evt_1"), "evt", 1)
		require.NoError(t, err)
		assert.Equal(t, "1700000000000::evt_1", position)
	})

	cases := []struct {
		name    string
		cursor  string
		message string
	}{
		{"not a cursor", "not-a-cursor", "invalid cursor"},
		{"cursor of another resource", cursor.Encode("att", 1, "1700000000000::att_1"), "invalid cursor"},
		{"cursor of another version", cursor.Encode("evt", 2, "1700000000000::evt_1"), "invalid cursor: cursor version mismatch: expected version 01"},
		{"empty position", cursor.Encode("evt", 1, ""), "invalid cursor"},
		{"position without an id", cursor.Encode("evt", 1, "1700000000000"), "invalid cursor"},
		{"position with a timestamp that is not a number", cursor.Encode("evt", 1, "yesterday::evt_1"), "invalid cursor: invalid timestamp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := driver.DecodeCursor(tc.cursor, "evt", 1)
			require.ErrorIs(t, err, cursor.ErrInvalidCursor)
			assert.EqualError(t, err, tc.message)
		})
	}
}
