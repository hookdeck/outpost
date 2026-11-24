package rediskey_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/rediskey"
	"github.com/stretchr/testify/assert"
)

func TestEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want string
	}{
		{"plain ID is unchanged", "des_01HZX-abc.def_9", "des_01HZX-abc.def_9"},
		{"empty", "", ""},
		{"colon", "org:123", "org%3A123"},
		{"braces", "a{b}c", "a%7Bb%7Dc"},
		{"percent", "100%", "100%25"},
		{"already escaped text stays distinct from the raw form", "org%3A123", "org%253A123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, rediskey.Escape(tt.id))
		})
	}
}

func TestTag(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "{tenant_1}", rediskey.Tag("tenant_1"))
	assert.Equal(t, "{x%7D%3Adestination%3Ay}", rediskey.Tag("x}:destination:y"))
}

func TestIDs(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "tenant_1:dest_1", rediskey.IDs("tenant_1", "dest_1"))
	assert.NotEqual(t,
		rediskey.IDs("org", "123:webhook"),
		rediskey.IDs("org:123", "webhook"),
		"pairs that differ only in where the colon falls must not share a result")
	assert.NotEqual(t,
		rediskey.IDs("org:123", "webhook"),
		rediskey.IDs("org%3A123", "webhook"))
}

func FuzzIDsDistinct(f *testing.F) {
	f.Add("org", "123:webhook", "org:123", "webhook")
	f.Add("a%3Ab", "c", "a:b", "c")
	f.Add("", ":", ":", "")
	f.Fuzz(func(t *testing.T, a1, a2, b1, b2 string) {
		if a1 == b1 && a2 == b2 {
			return
		}
		if rediskey.IDs(a1, a2) == rediskey.IDs(b1, b2) {
			t.Fatalf("(%q, %q) and (%q, %q) both produce %q", a1, a2, b1, b2, rediskey.IDs(a1, a2))
		}
	})
}
