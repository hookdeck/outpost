package config

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckOpenAPIRedirect(t *testing.T) {
	req := func(u string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	via := func(urls ...string) []*http.Request {
		out := make([]*http.Request, len(urls))
		for i, u := range urls {
			out[i] = req(u)
		}
		return out
	}

	assert.NoError(t, checkOpenAPIRedirect(req("http://b/x"), via("http://a/x")))
	assert.NoError(t, checkOpenAPIRedirect(req("https://b/x"), via("http://a/x")), "http to https is an upgrade")
	assert.NoError(t, checkOpenAPIRedirect(req("https://b/x"), via("https://a/x")))
	assert.NoError(t, checkOpenAPIRedirect(req("https://d/x"), via("https://a/x", "https://b/x", "https://c/x")), "third redirect")

	assert.EqualError(t, checkOpenAPIRedirect(req("http://b/x"), via("https://a/x")), "refused a redirect from https to http")
	assert.EqualError(t, checkOpenAPIRedirect(req("http://c/x"), via("http://a/x", "https://b/x")), "refused a redirect from https to http")
	assert.EqualError(t, checkOpenAPIRedirect(req("https://e/x"), via("https://a/x", "https://b/x", "https://c/x", "https://d/x")), "stopped after 3 redirects")
	assert.Error(t, checkOpenAPIRedirect(req("file:///etc/passwd"), via("http://a/x")))
}

func TestMaskOpenAPISource(t *testing.T) {
	tests := map[string]string{
		"":                                 "",
		"config/openapi.yaml":              "config/openapi.yaml",
		"/etc/outpost/openapi%zz.yaml":     "/etc/outpost/openapi%zz.yaml",
		"https://example.com/openapi.yaml": "https://example.com/openapi.yaml",
		"https://user:pass@example.com/openapi.yaml":                "https://***@example.com/openapi.yaml",
		"https://token@example.com:8443/a/openapi.yaml?sig=abc#top": "https://***@example.com:8443/a/openapi.yaml?***",
		"http://example.com/openapi.yaml?":                          "http://example.com/openapi.yaml?***",
		"ftp://user:pass@example.com/openapi.yaml":                  "ftp://***@example.com/openapi.yaml",
		"https://[::1/openapi.yaml":                                 "<invalid URL>",
	}
	for in, want := range tests {
		assert.Equal(t, want, maskOpenAPISource(in), in)
	}
}
