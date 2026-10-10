package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskURLSecrets(t *testing.T) {
	tests := map[string]string{
		"":                                    "",
		"http://proxy:3128":                   "http://proxy:3128",
		"https://mcp.example.com/mcp":         "https://mcp.example.com/mcp",
		"http://user:pass@proxy:3128":         "http://***@proxy:3128",
		"http://token@proxy:3128":             "http://***@proxy:3128",
		"socks5h://u:p@[::1]:1080":            "socks5h://***@[::1]:1080",
		"https://mcp.example.com/mcp?key=x":   "https://mcp.example.com/mcp?***",
		"https://mcp.example.com/mcp?":        "https://mcp.example.com/mcp?***",
		"https://mcp.example.com/a%2Fb#frag":  "https://mcp.example.com/a%2Fb",
		"https://u:p@h/p?q=1#f":               "https://***@h/p?***",
		"user:pass@proxy:3128":                "<invalid URL>",
		"proxy:3128":                          "<invalid URL>",
		"//user:pass@proxy:3128":              "<invalid URL>",
		"http://user:pass@":                   "<invalid URL>",
		"http://u:p@a:1 http://u:p@b:2":       "<invalid URL>",
		"http://user:pa%zzss@proxy":           "<invalid URL>",
		"https://[::1/mcp":                    "<invalid URL>",
		"/relative/path?token=secret":         "<invalid URL>",
		"http://user:pass@proxy:3128/x?y=z#w": "http://***@proxy:3128/x?***",
	}
	for in, want := range tests {
		assert.Equal(t, want, maskURLSecrets(in), in)
	}
}
