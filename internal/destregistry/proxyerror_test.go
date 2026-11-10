package destregistry_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyHopPolicy_Apply(t *testing.T) {
	t.Parallel()
	hops := func(n int) []*url.URL {
		out := make([]*url.URL, n)
		for i := range out {
			out[i] = &url.URL{Scheme: "http", User: url.UserPassword("u", "topsecret"), Host: "hop" + string(rune('0'+i)) + ":3128"}
		}
		return out
	}
	infraAt := func(i int) error {
		return &destregistry.ErrProxyInfra{Underlying: errors.New("boom"), DestHost: "dest", HopIndex: i, Hop: "http://hop:3128"}
	}

	tests := []struct {
		name      string
		hops      int
		external  int
		errHop    int
		wantAtt   bool
		wantInfra bool
	}{
		{"no external hops", 2, 0, 1, false, true},
		{"last hop external, failure at last", 2, 1, 1, true, false},
		{"last hop external, failure before", 2, 1, 0, false, true},
		{"all external", 2, 2, 0, true, false},
		{"more external than hops is clamped", 2, 5, 0, true, false},
		{"negative is ignored", 2, -1, 1, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := destregistry.NewProxyHopPolicy(hops(tt.hops), tt.external).Apply(infraAt(tt.errHop))
			var destErr *destregistry.ErrProxyDestination
			var infra *destregistry.ErrProxyInfra
			assert.Equal(t, tt.wantAtt, errors.As(got, &destErr))
			assert.Equal(t, tt.wantInfra, errors.As(got, &infra))
			if tt.wantAtt {
				assert.Equal(t, destregistry.ProxyErrorCode, destErr.Code)
				assert.Equal(t, "dest", destErr.DestHost)
				assert.NotContains(t, destErr.Error(), "topsecret")
				assert.False(t, destregistry.IsProxyInfraError(got), "must not read as a nack")
			}
		})
	}

	t.Run("other errors are untouched", func(t *testing.T) {
		t.Parallel()
		plain := errors.New("plain")
		assert.Equal(t, plain, destregistry.NewProxyHopPolicy(hops(1), 1).Apply(plain))
		dest := &destregistry.ErrProxyDestination{Code: "timeout"}
		assert.Equal(t, dest, destregistry.NewProxyHopPolicy(hops(1), 1).Apply(dest))
	})

	t.Run("zero value is a no-op", func(t *testing.T) {
		t.Parallel()
		err := infraAt(0)
		assert.Equal(t, err, destregistry.ProxyHopPolicy{}.Apply(err))
	})

	t.Run("customer-visible text carries no proxy address", func(t *testing.T) {
		t.Parallel()
		got := destregistry.NewProxyHopPolicy(hops(1), 1).Apply(&destregistry.ErrProxyInfra{
			Underlying: errors.New("dial tcp 10.0.0.9:3128: connect: connection refused"),
			DestHost:   "dest", Hop: "http://hop0:3128",
		})
		delivery, _ := destregistry.ProxyPublishResult(got, "rabbitmq")
		require.NotNil(t, delivery)
		assert.Equal(t, map[string]interface{}{"error": "proxy_error connecting to dest"}, delivery.Response)
	})
}
