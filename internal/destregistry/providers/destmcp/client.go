package destmcp

import (
	"errors"
	"net/http"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/netguard"
)

const maxResponseHeaderBytes = 64 << 10

// NewHTTPClient returns a client that connects only to addresses guard allows,
// ignores proxy environment variables and never follows redirects.
func NewHTTPClient(guard *netguard.Guard, cfg destregistry.HTTPClientConfig) (*http.Client, error) {
	if guard == nil {
		return nil, errors.New("destmcp: guard is required")
	}
	cfg.Guard = guard
	// Callback responses are small and mostly discarded: headers are capped
	// and gzip is never requested, so a callback can't make a response
	// expensive to read.
	cfg.MaxResponseHeaderBytes = maxResponseHeaderBytes
	cfg.DisableCompression = true
	client, err := destregistry.NewHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client, nil
}
