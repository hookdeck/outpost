package cursor

import (
	"errors"
	"math/big"
	"testing"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCursor_IsEmpty(t *testing.T) {
	t.Run("empty cursor", func(t *testing.T) {
		c := Cursor{}
		assert.True(t, c.IsEmpty())
	})

	t.Run("cursor with position", func(t *testing.T) {
		c := Cursor{Position: "abc123"}
		assert.False(t, c.IsEmpty())
	})

	t.Run("cursor with only sort params", func(t *testing.T) {
		c := Cursor{SortBy: "event_time", SortOrder: "desc"}
		assert.True(t, c.IsEmpty(), "cursor without position is empty")
	})
}

func TestEncode(t *testing.T) {
	t.Run("encodes cursor to base62", func(t *testing.T) {
		c := Cursor{
			SortBy:    "delivery_time",
			SortOrder: "desc",
			Position:  "1234567890_del_abc",
		}
		encoded := Encode(c)
		assert.NotEmpty(t, encoded)
		assert.NotContains(t, encoded, ":", "encoded cursor should not contain raw separators")
	})

	t.Run("different cursors produce different encodings", func(t *testing.T) {
		c1 := Cursor{SortBy: "delivery_time", SortOrder: "desc", Position: "pos1"}
		c2 := Cursor{SortBy: "delivery_time", SortOrder: "desc", Position: "pos2"}
		assert.NotEqual(t, Encode(c1), Encode(c2))
	})

	t.Run("same cursor produces same encoding", func(t *testing.T) {
		c := Cursor{SortBy: "event_time", SortOrder: "asc", Position: "pos"}
		assert.Equal(t, Encode(c), Encode(c))
	})
}

func TestDecode(t *testing.T) {
	t.Run("empty string returns empty cursor", func(t *testing.T) {
		c, err := Decode("")
		require.NoError(t, err)
		assert.True(t, c.IsEmpty())
	})

	t.Run("decodes v1 cursor", func(t *testing.T) {
		original := Cursor{
			SortBy:    "delivery_time",
			SortOrder: "desc",
			Position:  "1234567890_del_abc",
		}
		encoded := Encode(original)

		decoded, err := Decode(encoded)
		require.NoError(t, err)
		assert.Equal(t, original.SortBy, decoded.SortBy)
		assert.Equal(t, original.SortOrder, decoded.SortOrder)
		assert.Equal(t, original.Position, decoded.Position)
	})

	t.Run("decodes cursor with colons in position", func(t *testing.T) {
		original := Cursor{
			SortBy:    "event_time",
			SortOrder: "asc",
			Position:  "time:with:colons:in:it",
		}
		encoded := Encode(original)

		decoded, err := Decode(encoded)
		require.NoError(t, err)
		assert.Equal(t, original.Position, decoded.Position)
	})

	t.Run("invalid base62 returns error", func(t *testing.T) {
		_, err := Decode("!!!invalid!!!")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})

	t.Run("v1 invalid sortBy returns error", func(t *testing.T) {
		raw := "v1:invalid_sort:desc:position"
		encoded := encodeRaw(raw)

		_, err := Decode(encoded)
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})

	t.Run("v1 invalid sortOrder returns error", func(t *testing.T) {
		raw := "v1:event_time:invalid_order:position"
		encoded := encodeRaw(raw)

		_, err := Decode(encoded)
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})

	t.Run("v1 empty position returns error", func(t *testing.T) {
		raw := "v1:event_time:desc:"
		encoded := encodeRaw(raw)

		_, err := Decode(encoded)
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})

	t.Run("v1 missing parts returns error", func(t *testing.T) {
		raw := "v1:event_time:desc" // missing position
		encoded := encodeRaw(raw)

		_, err := Decode(encoded)
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})
}

func TestDecodeV0BackwardCompatibility(t *testing.T) {
	t.Run("decodes v0 cursor with defaults", func(t *testing.T) {
		// v0 format: just position, no version prefix
		position := "1704067200_evt_abc"
		encoded := encodeRaw(position)

		decoded, err := Decode(encoded)
		require.NoError(t, err)
		assert.Equal(t, position, decoded.Position)
		assert.Equal(t, "event_time", decoded.SortBy, "v0 defaults to event_time")
		assert.Equal(t, "desc", decoded.SortOrder, "v0 defaults to desc")
	})

	t.Run("decodes v0 composite cursor", func(t *testing.T) {
		// v0 composite cursor for event_time sort
		position := "1704067200_evt_abc_1704067500_del_xyz"
		encoded := encodeRaw(position)

		decoded, err := Decode(encoded)
		require.NoError(t, err)
		assert.Equal(t, position, decoded.Position)
		assert.Equal(t, "event_time", decoded.SortBy)
		assert.Equal(t, "desc", decoded.SortOrder)
	})

	t.Run("v0 cursor validates with matching defaults", func(t *testing.T) {
		position := "1704067200_evt_abc"
		encoded := encodeRaw(position)

		// Should work with default sort params
		next, _, err := DecodeAndValidate(encoded, "", "event_time", "desc")
		require.NoError(t, err)
		assert.Equal(t, position, next.Position)
	})

	t.Run("v0 cursor fails validation with non-default sort params", func(t *testing.T) {
		position := "1704067200_del_xyz"
		encoded := encodeRaw(position)

		// Should fail because v0 defaults to event_time, not delivery_time
		_, _, err := DecodeAndValidate(encoded, "", "delivery_time", "desc")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
		assert.Contains(t, err.Error(), "sortBy")
	})

	t.Run("v0 cursor fails validation with different sort order", func(t *testing.T) {
		position := "1704067200_evt_abc"
		encoded := encodeRaw(position)

		// Should fail because v0 defaults to desc, not asc
		_, _, err := DecodeAndValidate(encoded, "", "event_time", "asc")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
		assert.Contains(t, err.Error(), "sortOrder")
	})

	t.Run("random string treated as v0 position", func(t *testing.T) {
		// Any valid base62 that doesn't start with "v1:" is treated as v0
		position := "some_random_position_string"
		encoded := encodeRaw(position)

		decoded, err := Decode(encoded)
		require.NoError(t, err)
		assert.Equal(t, position, decoded.Position)
		assert.Equal(t, "event_time", decoded.SortBy)
		assert.Equal(t, "desc", decoded.SortOrder)
	})
}

func TestValidate(t *testing.T) {
	t.Run("empty cursor is always valid", func(t *testing.T) {
		c := Cursor{}
		err := Validate(c, "event_time", "desc")
		assert.NoError(t, err)
	})

	t.Run("matching params is valid", func(t *testing.T) {
		c := Cursor{SortBy: "event_time", SortOrder: "desc", Position: "pos"}
		err := Validate(c, "event_time", "desc")
		assert.NoError(t, err)
	})

	t.Run("mismatched sortBy returns error", func(t *testing.T) {
		c := Cursor{SortBy: "event_time", SortOrder: "desc", Position: "pos"}
		err := Validate(c, "delivery_time", "desc")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
		assert.Contains(t, err.Error(), "sortBy")
	})

	t.Run("mismatched sortOrder returns error", func(t *testing.T) {
		c := Cursor{SortBy: "event_time", SortOrder: "desc", Position: "pos"}
		err := Validate(c, "event_time", "asc")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
		assert.Contains(t, err.Error(), "sortOrder")
	})
}

func TestDecodeAndValidate(t *testing.T) {
	t.Run("empty cursors return empty results", func(t *testing.T) {
		next, prev, err := DecodeAndValidate("", "", "delivery_time", "desc")
		require.NoError(t, err)
		assert.True(t, next.IsEmpty())
		assert.True(t, prev.IsEmpty())
	})

	t.Run("valid next cursor", func(t *testing.T) {
		original := Cursor{SortBy: "delivery_time", SortOrder: "desc", Position: "pos"}
		encoded := Encode(original)

		next, prev, err := DecodeAndValidate(encoded, "", "delivery_time", "desc")
		require.NoError(t, err)
		assert.Equal(t, "pos", next.Position)
		assert.True(t, prev.IsEmpty())
	})

	t.Run("valid prev cursor", func(t *testing.T) {
		original := Cursor{SortBy: "event_time", SortOrder: "asc", Position: "pos"}
		encoded := Encode(original)

		next, prev, err := DecodeAndValidate("", encoded, "event_time", "asc")
		require.NoError(t, err)
		assert.True(t, next.IsEmpty())
		assert.Equal(t, "pos", prev.Position)
	})

	t.Run("invalid next cursor returns error", func(t *testing.T) {
		_, _, err := DecodeAndValidate("!!!invalid!!!", "", "delivery_time", "desc")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})

	t.Run("invalid prev cursor returns error", func(t *testing.T) {
		_, _, err := DecodeAndValidate("", "!!!invalid!!!", "delivery_time", "desc")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})

	t.Run("mismatched next cursor returns error", func(t *testing.T) {
		original := Cursor{SortBy: "delivery_time", SortOrder: "desc", Position: "pos"}
		encoded := Encode(original)

		_, _, err := DecodeAndValidate(encoded, "", "event_time", "desc")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})

	t.Run("mismatched prev cursor returns error", func(t *testing.T) {
		original := Cursor{SortBy: "delivery_time", SortOrder: "desc", Position: "pos"}
		encoded := Encode(original)

		_, _, err := DecodeAndValidate("", encoded, "delivery_time", "asc")
		require.Error(t, err)
		assert.True(t, errors.Is(err, driver.ErrInvalidCursor))
	})
}

func TestRoundTrip(t *testing.T) {
	testCases := []Cursor{
		{SortBy: "delivery_time", SortOrder: "desc", Position: "simple"},
		{SortBy: "delivery_time", SortOrder: "asc", Position: "1234567890_del_abc123"},
		{SortBy: "event_time", SortOrder: "desc", Position: "1234567890_evt_abc_1234567891_del_xyz"},
		{SortBy: "event_time", SortOrder: "asc", Position: "with:colons:and_underscores"},
		{SortBy: "delivery_time", SortOrder: "desc", Position: "unicode-émoji-🎉"},
	}

	for _, tc := range testCases {
		t.Run(tc.Position, func(t *testing.T) {
			encoded := Encode(tc)
			decoded, err := Decode(encoded)
			require.NoError(t, err)

			assert.Equal(t, tc.SortBy, decoded.SortBy)
			assert.Equal(t, tc.SortOrder, decoded.SortOrder)
			assert.Equal(t, tc.Position, decoded.Position)
		})
	}
}

// encodeRaw is a helper to encode raw strings for testing
func encodeRaw(raw string) string {
	num := new(big.Int)
	num.SetBytes([]byte(raw))
	return num.Text(62)
}
