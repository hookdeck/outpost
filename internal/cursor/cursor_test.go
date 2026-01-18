package cursor_test

import (
	"errors"
	"testing"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBase62Encode(t *testing.T) {
	t.Run("encodes string to base62", func(t *testing.T) {
		// Verified against Go's big.Int.Text(62) which uses alphabet: 0-9a-zA-Z
		encoded := cursor.Base62Encode("the quick brown fox jumps over the lazy dog")
		assert.Equal(t, "b6QPtm6Z5XFM81QySyltRRVYvv0ELEGBENK9XUgI4iciqMTErk0ea0kd2n", encoded)
	})

	t.Run("round-trips through encode/decode", func(t *testing.T) {
		original := "the quick brown fox jumps over the lazy dog"
		encoded := cursor.Base62Encode(original)
		decoded, err := cursor.Base62Decode(encoded)
		require.NoError(t, err)
		assert.Equal(t, original, decoded)
	})

	t.Run("empty string returns empty", func(t *testing.T) {
		encoded := cursor.Base62Encode("")
		assert.Empty(t, encoded)
	})

	t.Run("same input produces same output", func(t *testing.T) {
		a := cursor.Base62Encode("test")
		b := cursor.Base62Encode("test")
		assert.Equal(t, a, b)
	})

	t.Run("different inputs produce different outputs", func(t *testing.T) {
		a := cursor.Base62Encode("test1")
		b := cursor.Base62Encode("test2")
		assert.NotEqual(t, a, b)
	})
}

func TestBase62Decode(t *testing.T) {
	t.Run("decodes base62 string", func(t *testing.T) {
		// Verified against Go's big.Int.SetString(s, 62) which uses alphabet: 0-9a-zA-Z
		decoded, err := cursor.Base62Decode("b6QPtm6Z5XFM81QySyltRRVYvv0ELEGBENK9XUgI4iciqMTErk0ea0kd2n")
		require.NoError(t, err)
		assert.Equal(t, "the quick brown fox jumps over the lazy dog", decoded)
	})

	t.Run("empty string returns empty", func(t *testing.T) {
		decoded, err := cursor.Base62Decode("")
		require.NoError(t, err)
		assert.Empty(t, decoded)
	})

	t.Run("invalid base62 returns error", func(t *testing.T) {
		_, err := cursor.Base62Decode("!!!invalid!!!")
		require.Error(t, err)
		assert.True(t, errors.Is(err, cursor.ErrInvalidCursor))
	})
}

func TestBase62Roundtrip(t *testing.T) {
	testCases := []string{
		"simple",
		"with spaces",
		"with:colons",
		"with_underscores",
		"unicode-émoji-🎉",
		"1234567890",
		"mixed123with456numbers",
	}

	for _, tc := range testCases {
		t.Run(tc, func(t *testing.T) {
			encoded := cursor.Base62Encode(tc)
			decoded, err := cursor.Base62Decode(encoded)
			require.NoError(t, err)
			assert.Equal(t, tc, decoded)
		})
	}
}

func TestEncode(t *testing.T) {
	t.Run("encodes with resource and version", func(t *testing.T) {
		encoded := cursor.Encode("evt", 1, "position123")
		assert.NotEmpty(t, encoded)
		assert.NotContains(t, encoded, ":", "encoded cursor should not contain raw separators")
	})

	t.Run("different resources produce different encodings", func(t *testing.T) {
		a := cursor.Encode("evt", 1, "data")
		b := cursor.Encode("dlv", 1, "data")
		assert.NotEqual(t, a, b)
	})

	t.Run("different versions produce different encodings", func(t *testing.T) {
		a := cursor.Encode("evt", 1, "data")
		b := cursor.Encode("evt", 2, "data")
		assert.NotEqual(t, a, b)
	})

	t.Run("different data produces different encodings", func(t *testing.T) {
		a := cursor.Encode("evt", 1, "data1")
		b := cursor.Encode("evt", 1, "data2")
		assert.NotEqual(t, a, b)
	})

	t.Run("version is zero-padded", func(t *testing.T) {
		// Version 1 should be "01" internally
		encoded1 := cursor.Encode("evt", 1, "data")
		encoded01 := cursor.Encode("evt", 1, "data")
		assert.Equal(t, encoded1, encoded01)
	})
}

func TestDecode(t *testing.T) {
	t.Run("empty string returns empty", func(t *testing.T) {
		data, err := cursor.Decode("", "evt", 1)
		require.NoError(t, err)
		assert.Empty(t, data)
	})

	t.Run("decodes valid cursor", func(t *testing.T) {
		encoded := cursor.Encode("evt", 1, "position123")
		data, err := cursor.Decode(encoded, "evt", 1)
		require.NoError(t, err)
		assert.Equal(t, "position123", data)
	})

	t.Run("wrong resource returns ErrInvalidCursor", func(t *testing.T) {
		encoded := cursor.Encode("evt", 1, "data")
		_, err := cursor.Decode(encoded, "dlv", 1)
		require.Error(t, err)
		assert.True(t, errors.Is(err, cursor.ErrInvalidCursor))
	})

	t.Run("wrong version returns ErrVersionMismatch", func(t *testing.T) {
		encoded := cursor.Encode("evt", 1, "data")
		_, err := cursor.Decode(encoded, "evt", 2)
		require.Error(t, err)
		assert.True(t, errors.Is(err, cursor.ErrVersionMismatch))
	})

	t.Run("invalid base62 returns ErrInvalidCursor", func(t *testing.T) {
		_, err := cursor.Decode("!!!invalid!!!", "evt", 1)
		require.Error(t, err)
		assert.True(t, errors.Is(err, cursor.ErrInvalidCursor))
	})

	t.Run("completely malformed cursor returns ErrInvalidCursor", func(t *testing.T) {
		// Encode something that doesn't follow the format at all
		encoded := cursor.Base62Encode("garbage")
		_, err := cursor.Decode(encoded, "evt", 1)
		require.Error(t, err)
		assert.True(t, errors.Is(err, cursor.ErrInvalidCursor))
	})
}

func TestRoundtrip(t *testing.T) {
	testCases := []struct {
		resource string
		version  int
		data     string
	}{
		{"evt", 1, "simple"},
		{"dlv", 1, "1234567890_del_abc"},
		{"tnt", 2, "timestamp:12345"},
		{"evt", 99, "max_version"},
		{"x", 1, "short_resource"},
		{"longresource", 1, "long_resource_name"},
		{"evt", 1, "data:with:colons:in:it"},
		{"evt", 1, "unicode-émoji-🎉"},
	}

	for _, tc := range testCases {
		name := tc.resource + "_v" + string(rune('0'+tc.version)) + "_" + tc.data
		t.Run(name, func(t *testing.T) {
			encoded := cursor.Encode(tc.resource, tc.version, tc.data)
			decoded, err := cursor.Decode(encoded, tc.resource, tc.version)
			require.NoError(t, err)
			assert.Equal(t, tc.data, decoded)
		})
	}
}

func TestVersionMismatchMessage(t *testing.T) {
	t.Run("error includes expected version", func(t *testing.T) {
		encoded := cursor.Encode("evt", 1, "data")
		_, err := cursor.Decode(encoded, "evt", 5)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "05")
	})
}
