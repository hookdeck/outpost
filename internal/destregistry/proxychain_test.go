package destregistry_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProxyURL(t *testing.T) {
	t.Parallel()

	t.Run("empty and whitespace-only yield no proxy", func(t *testing.T) {
		t.Parallel()
		for _, in := range []string{"", "   ", "\t\n"} {
			hops, err := destregistry.ParseProxyURL(in)
			require.NoError(t, err)
			assert.Nil(t, hops)
		}
	})

	t.Run("single URL parses unchanged, including comma in password", func(t *testing.T) {
		t.Parallel()
		hops, err := destregistry.ParseProxyURL("http://user:p,ss@proxy.example.com:3128")
		require.NoError(t, err)
		require.Len(t, hops, 1)
		assert.Equal(t, "proxy.example.com:3128", hops[0].Host)
		pw, _ := hops[0].User.Password()
		assert.Equal(t, "p,ss", pw)
	})

	t.Run("multiple hops split on any whitespace, order preserved", func(t *testing.T) {
		t.Parallel()
		hops, err := destregistry.ParseProxyURL("http://a:10000 \t https://b:8443\nhttp://c")
		require.NoError(t, err)
		require.Len(t, hops, 3)
		assert.Equal(t, "a:10000", hops[0].Host)
		assert.Equal(t, "https", hops[1].Scheme)
		assert.Equal(t, "b:8443", hops[1].Host)
		assert.Equal(t, "c", hops[2].Host)
	})

	t.Run("rejects invalid hops by index without echoing credentials", func(t *testing.T) {
		t.Parallel()
		cases := map[string]string{
			"relative":     "http://a:10000 proxy.example.com:3128",
			"socks5":       "http://a:10000 socks5://b:1080",
			"missing host": "http://a:10000 http://",
			"garbage":      "http://a:10000 ://bad",
			"userinfo":     "http://a:10000 socks5://secretuser:secretpass@b:1080",
		}
		for name, in := range cases {
			t.Run(name, func(t *testing.T) {
				hops, err := destregistry.ParseProxyURL(in)
				require.Error(t, err)
				assert.Nil(t, hops)
				assert.Contains(t, err.Error(), "proxy hop 1")
				assert.NotContains(t, err.Error(), "secret")
			})
		}
	})
}

func TestRedactedProxyURL(t *testing.T) {
	t.Parallel()
	hops, err := destregistry.ParseProxyURL("https://user:pass@proxy.example.com:8443/ignored?x=1")
	require.NoError(t, err)
	assert.Equal(t, "https://proxy.example.com:8443", destregistry.RedactedProxyURL(hops[0]))
	assert.Equal(t, "", destregistry.RedactedProxyURL(nil))
}
