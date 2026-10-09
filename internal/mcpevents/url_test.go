package mcpevents

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// urlReason returns the invalid_params reason of a NormalizeCallbackURL error.
func urlReason(t *testing.T, err error) string {
	t.Helper()
	var mcpErr *Error
	require.True(t, errors.As(err, &mcpErr), "want *Error, got %v", err)
	require.Equal(t, KindInvalidParams, mcpErr.Kind)
	require.Equal(t, FieldDeliveryURL, mcpErr.Data["field"])
	return mcpErr.Data["reason"].(string)
}

// Intentional differences from WHATWG `new URL(input).href` (the guide derives
// subscription IDs from href). Each entry is our result: the normalized URL,
// or the invalid_params reason. Everything else in testdata/whatwg_urls.json
// must match WHATWG exactly (both rejecting counts as a match).
//
//   - A trailing dot is removed (WHATWG keeps it), so "a.com." and "a.com"
//     share an ID, a verification and rate-limit buckets; "a.com.." keeps an
//     empty label and is rejected.
//   - Port 0 is rejected (WHATWG keeps ":0", which can't be dialed).
//   - Hosts follow the IDNA lookup profile with STD3 rules: "_", leading or
//     trailing "-" and "--" in label positions 3-4 (outside xn--) are
//     rejected; WHATWG allows them. Publicly trusted TLS certificates can't
//     name underscore hosts anyway.
//   - Hosts ending in a number must be canonical dotted-decimal IPv4: the
//     WHATWG shorthand forms (127.1, 0x7f.0.0.1, 2130706433, octal
//     127.000.0.1) are rejected rather than rewritten, so no parser in the
//     chain (getaddrinfo included) gets to interpret them differently.
//   - IPv4-mapped IPv6 literals are unmapped to IPv4 (WHATWG keeps
//     [::ffff:7f00:1]), so the address guard and the rate-limit buckets see
//     one spelling per address.
//   - Userinfo (even an empty "@") and fragments (even an empty "#") are
//     rejected; WHATWG keeps or drops them.
//   - Whitespace, control characters and backslashes are rejected anywhere:
//     WHATWG strips leading/trailing spaces, drops tabs and newlines inside,
//     encodes inner spaces and treats "\" as "/". These are classic
//     parser-differential vectors (example.com\@evil.com).
//   - "|" is percent-encoded in paths (WHATWG leaves it), so net/url
//     serializes the same path it was given.
//   - Invalid percent-escapes (%zz) are rejected; WHATWG keeps them.
//   - Percent-encoded hosts (ex%61mple.com) are rejected; WHATWG decodes them.
//   - Scheme-relative spellings of special URLs (https:example.com,
//     https:/example.com, https:///path) are rejected; WHATWG repairs them.
//   - Schemes other than http and https are rejected with https_required.
var whatwgDifferences = map[string]string{
	"https://Example.COM./a":                  "https://example.com/a",
	"https://example.com../a":                 ReasonInvalidURL,
	"https://example.com:0/":                  ReasonInvalidURL,
	"https://my_host.example.com/":            ReasonInvalidURL,
	"https://-lead.example.com/":              ReasonInvalidURL,
	"https://r3---sn-abc.example.com/":        ReasonInvalidURL,
	"https://127.1/":                          ReasonInvalidURL,
	"https://0x7f.0.0.1/":                     ReasonInvalidURL,
	"https://2130706433/":                     ReasonInvalidURL,
	"https://127.000.0.1/":                    ReasonInvalidURL,
	"https://[::ffff:7f00:1]/":                "https://127.0.0.1/",
	"https://[::ffff:127.0.0.1]/":             "https://127.0.0.1/",
	"https://[0:0:0:0:0:ffff:127.0.0.1]/":     "https://127.0.0.1/",
	"https://user:pass@example.com/":          ReasonInvalidURL,
	"https://user@example.com/":               ReasonInvalidURL,
	"https://@example.com/":                   ReasonInvalidURL,
	"https://example.com/#frag":               ReasonInvalidURL,
	"https://example.com/#":                   ReasonInvalidURL,
	"https://example.com/a b":                 ReasonInvalidURL,
	"https://example.com/a<b>\"c\"`d{e}f^g|h": "https://example.com/a%3Cb%3E%22c%22%60d%7Be%7Df%5Eg%7Ch",
	"https://example.com/%7e%7E%zz":           ReasonInvalidURL,
	"https://example.com/a\\b":                ReasonInvalidURL,
	"https://example.com\\@evil.com/":         ReasonInvalidURL,
	" https://example.com/ ":                  ReasonInvalidURL,
	"https://exa\tmple.com/":                  ReasonInvalidURL,
	"https:example.com/":                      ReasonInvalidURL,
	"https:/example.com/":                     ReasonInvalidURL,
	"https:///path":                           ReasonInvalidURL,
	"https://ex%61mple.com/":                  ReasonInvalidURL,
	"ftp://example.com/":                      ReasonHTTPSRequired,
	"javascript:alert(1)":                     ReasonHTTPSRequired,
	"file:///etc/passwd":                      ReasonHTTPSRequired,
	"wss://example.com/":                      ReasonHTTPSRequired,
}

func TestNormalizeCallbackURL_WHATWGVectors(t *testing.T) {
	t.Parallel()
	type vector struct {
		Input string `json:"input"`
		Href  string `json:"href"`
		Error string `json:"error"`
	}
	seen := map[string]bool{}
	for _, v := range loadVectors[vector](t, "whatwg_urls.json") {
		seen[v.Input] = true
		u, got, err := NormalizeCallbackURL(v.Input)
		if want, ok := whatwgDifferences[v.Input]; ok {
			if strings.Contains(want, "://") {
				require.NoError(t, err, "%q", v.Input)
				assert.Equal(t, want, got, "%q", v.Input)
				assert.NotEqual(t, v.Href, got, "%q is listed as a difference but matches WHATWG", v.Input)
			} else {
				require.Error(t, err, "%q", v.Input)
				assert.Equal(t, want, urlReason(t, err), "%q", v.Input)
				assert.Empty(t, v.Error, "%q is listed as a difference but WHATWG rejects it too", v.Input)
			}
			continue
		}
		if v.Error != "" {
			assert.Error(t, err, "%q: WHATWG rejects it", v.Input)
			continue
		}
		require.NoError(t, err, "%q", v.Input)
		assert.Equal(t, v.Href, got, "%q", v.Input)
		assert.Equal(t, got, u.String())
	}
	for input := range whatwgDifferences {
		assert.True(t, seen[input], "difference %q has no vector", input)
	}
}

func TestNormalizeCallbackURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string // normalized, or an invalid_params reason
	}{
		// Ports.
		{"https://a.example:443/x", "https://a.example/x"},
		{"https://a.example:0443/x", "https://a.example/x"},
		{"https://a.example:00443/x", "https://a.example/x"},
		{"https://a.example:000000000000443/x", "https://a.example/x"},
		{"https://a.example:65535/", "https://a.example:65535/"},
		{"https://a.example:65536/", ReasonInvalidURL},
		{"https://a.example:99999999999999999999/", ReasonInvalidURL},
		{"https://a.example:0/", ReasonInvalidURL},
		{"https://a.example:00000/", ReasonInvalidURL},
		{"https://a.example:-1/", ReasonInvalidURL},
		{"https://a.example:+443/", ReasonInvalidURL},
		{"https://a.example:/", "https://a.example/"},
		{"http://a.example:80/", "http://a.example/"},
		{"http://a.example:0080/", "http://a.example/"},
		{"http://a.example:443/", "http://a.example:443/"},
		{"https://a.example:80/", "https://a.example:80/"},
		// Hosts.
		{"HTTPS://A.EXAMPLE/Path", "https://a.example/Path"},
		{"https://a.example./", "https://a.example/"},
		{"https://a.example．/", "https://a.example/"}, // full-width trailing dot
		{"https://ＥＸＡＭＰＬＥ．ｃｏｍ/", "https://example.com/"},
		{"https://a.example../", ReasonInvalidURL},
		{"https://a..example/", ReasonInvalidURL},
		{"https://./", ReasonInvalidURL},
		{"https://BÜCHER.de/", "https://xn--bcher-kva.de/"},
		{"https://ⓐ.example/", "https://a.example/"},
		{"https://a­.example/", "https://a.example/"},
		{"https://xn--zz.example/", ReasonInvalidURL},
		{"https://a*b.example/", ReasonInvalidURL},
		{"https://a!b.example/", ReasonInvalidURL},
		{"https://" + strings.Repeat("a", 63) + ".example/", "https://" + strings.Repeat("a", 63) + ".example/"},
		{"https://" + strings.Repeat("a", 64) + ".example/", ReasonInvalidURL},
		{"https://" + strings.Repeat("a.", 127) + "b/", ReasonInvalidURL}, // 255 bytes
		{"https://１２７.０.０.１/", "https://127.0.0.1/"},                      // fullwidth digits map to ASCII first
		{"https://127.0.0.1./", "https://127.0.0.1/"},
		{"https://1.2.3.4:8443/", "https://1.2.3.4:8443/"},
		{"https://1.2.3/", ReasonInvalidURL},
		{"https://0x7f000001/", ReasonInvalidURL},
		{"https://a.0x10/", ReasonInvalidURL},
		{"https://a.0x/", ReasonInvalidURL},
		{"https://a.1b/", "https://a.1b/"},
		{"https://[1.2.3.4]/", ReasonInvalidURL},
		// IPv6 literals.
		{"https://[::1]/", "https://[::1]/"},
		{"https://[::1]:443/", "https://[::1]/"},
		{"https://[0:0:0:0:0:0:0:1]:8443/", "https://[::1]:8443/"},
		{"https://[0000:0000:0000:0000:0000:0000:0000:0001]/", "https://[::1]/"},
		{"https://[2001:DB8::1]/", "https://[2001:db8::1]/"},
		{"https://[2001:4860:4860:0:0:0:0:8888]/", "https://[2001:4860:4860::8888]/"},
		{"https://[::ffff:7f00:1]/", "https://127.0.0.1/"},
		{"https://[::FFFF:127.0.0.1]:8443/", "https://127.0.0.1:8443/"},
		{"https://[::ffff:a9fe:a9fe]/", "https://169.254.169.254/"},
		{"https://[64:ff9b::7f00:1]/", "https://[64:ff9b::7f00:1]/"},
		{"https://[fe80::1%25en0]/", ReasonInvalidURL},
		{"https://[::1/", ReasonInvalidURL},
		{"https://[::1]x/", ReasonInvalidURL},
		{"https://[zz::1]/", ReasonInvalidURL},
		// Paths and queries.
		{"https://a.example", "https://a.example/"},
		{"https://a.example?x=1", "https://a.example/?x=1"},
		{"https://a.example/?", "https://a.example/?"},
		{"https://a.example/a/./b/../c/", "https://a.example/a/c/"},
		{"https://a.example/a/%2E%2E/%2e/b", "https://a.example/b"},
		{"https://a.example/a/b/..", "https://a.example/a/"},
		{"https://a.example/../../..", "https://a.example/"},
		{"https://a.example/..a/a..", "https://a.example/..a/a.."},
		{"https://a.example/a%2Fb|c", "https://a.example/a%2Fb%7Cc"},
		{"https://a.example/%41%c3%a9", "https://a.example/%41%c3%a9"},
		{"https://a.example/é?é", "https://a.example/%C3%A9?%C3%A9"},
		{"https://a.example/p?a='b'&c=<d>&e=%zz", "https://a.example/p?a=%27b%27&c=%3Cd%3E&e=%zz"},
		{"https://a.example/%", ReasonInvalidURL},
		// Rejected outright.
		{"", ReasonInvalidURL},
		{"/relative", ReasonInvalidURL},
		{"//a.example/", ReasonInvalidURL},
		{"a.example", ReasonInvalidURL},
		{"https://", ReasonInvalidURL},
		{"https://:443/", ReasonInvalidURL},
		{"https://u:p@a.example/", ReasonInvalidURL},
		{"https://u@a.example/", ReasonInvalidURL},
		{"https://a.example@b.example/", ReasonInvalidURL},
		{"https://a.example/#x", ReasonInvalidURL},
		{"https://a.example/x\r\nHost: evil", ReasonInvalidURL},
		{"https://a.example/\x00", ReasonInvalidURL},
		{"https://a.example/\x7f", ReasonInvalidURL},
		{"ftp://a.example/", ReasonHTTPSRequired},
		{"HTTPX://a.example/", ReasonHTTPSRequired},
		{"data:text/plain,hi", ReasonHTTPSRequired},
		{"gopher://127.0.0.1:6379/_FLUSHALL", ReasonHTTPSRequired},
	}
	for _, tt := range tests {
		u, got, err := NormalizeCallbackURL(tt.in)
		if !strings.Contains(tt.want, "://") {
			require.Error(t, err, "%q -> %q", tt.in, got)
			assert.Equal(t, tt.want, urlReason(t, err), "%q", tt.in)
			continue
		}
		require.NoError(t, err, "%q", tt.in)
		assert.Equal(t, tt.want, got, "%q", tt.in)
		assert.Equal(t, got, u.String(), "%q", tt.in)
	}
}

func TestNormalizeCallbackURL_Length(t *testing.T) {
	t.Parallel()
	base := "https://a.example/"
	ok := base + strings.Repeat("a", MaxCallbackURLBytes-len(base))
	_, got, err := NormalizeCallbackURL(ok)
	require.NoError(t, err)
	assert.Len(t, got, MaxCallbackURLBytes)

	_, _, err = NormalizeCallbackURL(ok + "a")
	assert.Equal(t, ReasonInvalidURL, urlReason(t, err))

	// Within the input cap but over it once percent-encoded.
	_, _, err = NormalizeCallbackURL(base + strings.Repeat("é", (MaxCallbackURLBytes-len(base))/2))
	assert.Equal(t, ReasonInvalidURL, urlReason(t, err))
}

func TestNormalizeCallbackURL_Idempotent(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"https://BÜCHER.de:0443/a/../b/%2e/c?q=<x>",
		"https://[::FFFF:127.0.0.1]:08443/x|y",
		"http://a.example./é",
	} {
		_, once, err := NormalizeCallbackURL(in)
		require.NoError(t, err)
		_, twice, err := NormalizeCallbackURL(once)
		require.NoError(t, err)
		assert.Equal(t, once, twice)
	}
}

func TestHostPort(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"https://a.example/":       "a.example:443",
		"http://a.example/":        "a.example:80",
		"https://a.example:8443/":  "a.example:8443",
		"https://[::1]/":           "[::1]:443",
		"https://[2001:db8::1]:8/": "[2001:db8::1]:8",
		"https://127.0.0.1:1/":     "127.0.0.1:1",
	} {
		u, _, err := NormalizeCallbackURL(in)
		require.NoError(t, err)
		assert.Equal(t, want, HostPort(u))
	}
	assert.Equal(t, "a.example:443", HostPort(&url.URL{Scheme: "https", Host: "a.example"}))
}

func FuzzNormalizeCallbackURL(f *testing.F) {
	for _, seed := range []string{
		"https://a.example/", "https://BÜCHER.de:0443/a/../b?c=<d>", "http://[::ffff:7f00:1]:80/x|y",
		"https://a.example/%2e%2E/x", "https://127.0.0.1./", "https://a.example/?",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		u, s, err := NormalizeCallbackURL(in)
		if err != nil {
			var mcpErr *Error
			if !errors.As(err, &mcpErr) {
				t.Fatalf("non-MCP error %v", err)
			}
			return
		}
		if u.String() != s || len(s) > MaxCallbackURLBytes {
			t.Fatalf("%q: %q vs %q", in, u.String(), s)
		}
		if u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.Fragment != "" {
			t.Fatalf("%q: bad result %q", in, s)
		}
		_, again, err := NormalizeCallbackURL(s)
		if err != nil || again != s {
			t.Fatalf("not idempotent: %q -> %q -> %q (%v)", in, s, again, err)
		}
	})
}
