package destmcp_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_Valid(t *testing.T) {
	t.Parallel()
	verifier := &fakeVerifier{}
	resolver := newResolver()
	p := newProvider(t, func(c *destmcp.Config) {
		c.Verifier = verifier
		c.Guard = &netguard.Guard{Resolver: resolver}
	})
	d := newSubscription(t)

	require.NoError(t, p.Validate(context.Background(), d))

	assert.Equal(t, [][3]string{{testTenantID, testPrincipal, publicURL}}, verifier.VerifiedCalls())
	assert.Equal(t, []string{publicHost + "."}, resolver.Calls(), "the address is checked when the callback isn't verified yet")
	calls := verifier.VerifyCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, testTenantID, calls[0].TenantID)
	assert.Equal(t, testPrincipal, calls[0].Principal)
	assert.Equal(t, publicURL, calls[0].URL)
	assert.Equal(t, d.ID, calls[0].SubscriptionID)
	require.Len(t, calls[0].Secrets, 1)
	assert.Equal(t, secretKey(t, testSecret(1)), calls[0].Secrets[0].Key)
	assert.Nil(t, calls[0].Secrets[0].InvalidAt)
}

func TestValidate_OptionalFields(t *testing.T) {
	t.Parallel()
	p := newProvider(t)
	for name, mutate := range map[string]func(*models.Destination){
		"no principal":    func(d *models.Destination) { delete(d.Config, destmcp.ConfigPrincipal) },
		"no arguments":    func(d *models.Destination) { delete(d.Config, destmcp.ConfigArguments) },
		"empty arguments": func(d *models.Destination) { d.Config[destmcp.ConfigArguments] = "" },
		"empty object":    func(d *models.Destination) { d.Config[destmcp.ConfigArguments] = "{}" },
		"any filter":      func(d *models.Destination) { d.Filter = models.Filter{"data": map[string]any{"x": 1}} },
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, p.Validate(context.Background(), newSubscription(t, mutate)))
		})
	}
}

func TestValidate_FieldErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mutate     func(*models.Destination)
		field, typ string
	}{
		{"wrong type", func(d *models.Destination) { d.Type = "webhook" }, "type", "invalid_type"},
		{"id with a space", func(d *models.Destination) { d.ID = "sub 1" }, "id", "invalid"},
		{"id too long", func(d *models.Destination) { d.ID = strings.Repeat("a", mcpevents.MaxSubscriptionIDBytes+1) }, "id", "invalid"},
		{"id with a dot", func(d *models.Destination) { d.ID = "sub.1" }, "id", "invalid"},
		{"no topics", func(d *models.Destination) { d.Topics = nil }, "topics", "invalid"},
		{"two topics", func(d *models.Destination) { d.Topics = models.Topics{"a", "b"} }, "topics", "invalid"},
		{"wildcard topic", func(d *models.Destination) { d.Topics = models.Topics{"*"} }, "topics", "invalid"},
		{"principal too long", func(d *models.Destination) {
			d.Config[destmcp.ConfigPrincipal] = strings.Repeat("p", destmcp.MaxPrincipalBytes+1)
		}, "config.principal", "maxlength"},
		{"arguments not JSON", func(d *models.Destination) { d.Config[destmcp.ConfigArguments] = "{nope" }, "config.arguments", "invalid"},
		{"arguments not an object", func(d *models.Destination) { d.Config[destmcp.ConfigArguments] = `["USD"]` }, "config.arguments", "invalid"},
		{"arguments not canonical", func(d *models.Destination) {
			d.Config[destmcp.ConfigArguments] = `{"total":{"$gte":100},"currency":"USD"}`
		}, "config.arguments", "invalid"},
		{"arguments with duplicate keys", func(d *models.Destination) {
			d.Config[destmcp.ConfigArguments] = `{"currency":"USD","currency":"EUR"}`
		}, "config.arguments", "invalid"},
		{"arguments too large", func(d *models.Destination) {
			d.Config[destmcp.ConfigArguments] = `{"currency":"` + strings.Repeat("a", destmcp.MaxArgumentsBytes) + `"}`
		}, "config.arguments", "maxlength"},
		{"url invalid", func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "not a url" }, "config.url", "invalid_url"},
		{"url missing", func(d *models.Destination) { delete(d.Config, destmcp.ConfigURL) }, "config.url", "invalid_url"},
		{"url with credentials", func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "https://user:pass@receiver.example.com/" }, "config.url", "invalid_url"},
		{"url scheme", func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "ftp://receiver.example.com/" }, "config.url", "https_required"},
		{"url not normalized", func(d *models.Destination) {
			d.Config[destmcp.ConfigURL] = "https://RECEIVER.example.com:443/mcp-events/abc123"
		}, "config.url", "invalid_url"},
		{"secret missing", func(d *models.Destination) { delete(d.Credentials, destmcp.CredentialSecret) }, "credentials.secret", "invalid_secret"},
		{"secret not whsec", func(d *models.Destination) { d.Credentials[destmcp.CredentialSecret] = "secret" }, "credentials.secret", "invalid_secret"},
		{"secret too short", func(d *models.Destination) { d.Credentials[destmcp.CredentialSecret] = "whsec_c2hvcnQ=" }, "credentials.secret", "invalid_secret"},
		{"previous secret invalid", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = "nope"
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = "2030-01-01T00:00:00Z"
		}, "credentials.previous_secret", "invalid"},
		{"previous secret without expiry", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = testSecret(2)
		}, "credentials.previous_secret_invalid_at", "invalid"},
		{"previous secret expiry without secret", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = "2030-01-01T00:00:00Z"
		}, "credentials.previous_secret", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			verifier := &fakeVerifier{}
			resolver := newResolver()
			p := newProvider(t, func(c *destmcp.Config) {
				c.Verifier = verifier
				c.Guard = &netguard.Guard{Resolver: resolver}
			})
			requireValidationError(t, p.Validate(context.Background(), newSubscription(t, tt.mutate)), tt.field, tt.typ)
			assert.Empty(t, verifier.VerifyCalls(), "static failures never reach verification")
			assert.Empty(t, verifier.VerifiedCalls())
			assert.Empty(t, resolver.Calls(), "static failures never resolve")
		})
	}
}

func TestValidate_AddressCheck(t *testing.T) {
	t.Parallel()
	notAllowed := map[string]string{
		"private address":       "https://internal.example.com/hook",
		"one private address":   "https://mixed.example.com/hook",
		"link-local metadata":   "https://metadata.example.com/hook",
		"mapped loopback":       "https://mapped.example.com/hook",
		"loopback v6":           "https://loopback-v6.example.com/hook",
		"unique local v6":       "https://unique-local.example.com/hook",
		"nxdomain":              "https://nx.example.com/hook",
		"resolver failure":      "https://servfail.example.com/hook",
		"resolver timeout":      "https://timeout.example.com/hook",
		"private ip literal":    "https://10.1.2.3/hook",
		"loopback ip literal":   "https://127.0.0.1:8443/hook",
		"loopback v6 literal":   "https://[::1]/hook",
		"unspecified literal":   "https://0.0.0.0/hook",
		"not allowlisted local": "https://localhost/hook",
	}
	for name, rawURL := range notAllowed {
		t.Run(name, func(t *testing.T) {
			verifier := &fakeVerifier{}
			p := newProvider(t, func(c *destmcp.Config) { c.Verifier = verifier })
			d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = rawURL })
			err := p.Validate(context.Background(), d)
			requireValidationError(t, err, "config.url", "address_not_allowed")
			assert.Empty(t, verifier.VerifyCalls(), "no challenge to a refused address")

			host := mustURL(t, rawURL).Hostname()
			assert.NotContains(t, err.Error(), host)
			assert.NotContains(t, err.Error(), "no such host")
		})
	}

	t.Run("http on a global address", func(t *testing.T) {
		t.Parallel()
		p := newProvider(t)
		d := newSubscription(t, func(d *models.Destination) {
			d.Config[destmcp.ConfigURL] = "http://receiver.example.com/mcp-events/abc123"
		})
		requireValidationError(t, p.Validate(context.Background(), d), "config.url", "https_required")
	})

	// A refused http URL gets the same answer whatever its host resolves to.
	t.Run("http answers don't depend on DNS", func(t *testing.T) {
		t.Parallel()
		allow, err := netguard.ParseAllowlist([]string{"localhost"})
		require.NoError(t, err)
		for _, host := range []string{"receiver.example.com", "internal.example.com", "nx.example.com", "servfail.example.com", "10.0.0.1"} {
			p := newProvider(t, func(c *destmcp.Config) {
				c.Guard = &netguard.Guard{Allowlist: allow, Resolver: newResolver()}
			})
			d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "http://" + host + "/hook" })
			requireValidationError(t, p.Validate(context.Background(), d), "config.url", "https_required")
		}
	})

	t.Run("global v6 address", func(t *testing.T) {
		t.Parallel()
		p := newProvider(t)
		d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "https://public-v6.example.com/hook" })
		require.NoError(t, p.Validate(context.Background(), d))
	})

	t.Run("allowlisted private over http needs AllowInsecureHTTP", func(t *testing.T) {
		t.Parallel()
		allow, err := netguard.ParseAllowlist([]string{"10.0.0.0/8"})
		require.NoError(t, err)
		d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "http://10.0.0.5:8080/hook" })

		strict := newProvider(t, func(c *destmcp.Config) { c.Guard = &netguard.Guard{Allowlist: allow} })
		requireValidationError(t, strict.Validate(context.Background(), d), "config.url", "https_required")

		relaxed := newProvider(t, func(c *destmcp.Config) {
			c.Guard = &netguard.Guard{Allowlist: allow}
			c.AllowInsecureHTTP = true
		})
		require.NoError(t, relaxed.Validate(context.Background(), d))
	})

	t.Run("allowlisted loopback over http", func(t *testing.T) {
		t.Parallel()
		p := newProvider(t, func(c *destmcp.Config) { c.Guard = loopbackGuard(t) })
		d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "http://127.0.0.1:8080/hook" })
		require.NoError(t, p.Validate(context.Background(), d))
	})
}

func TestValidate_CachedVerificationSkipsAddressCheck(t *testing.T) {
	t.Parallel()
	verifier := &fakeVerifier{verified: true}
	resolver := newResolver()
	p := newProvider(t, func(c *destmcp.Config) {
		c.Verifier = verifier
		c.Guard = &netguard.Guard{Resolver: resolver}
	})
	// The host now resolves to a private address; the delivery-time dialer
	// refuses it on every connection, so a refresh needn't resolve it.
	d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "https://internal.example.com/hook" })

	require.NoError(t, p.Validate(context.Background(), d))
	assert.Equal(t, [][3]string{{testTenantID, testPrincipal, "https://internal.example.com/hook"}}, verifier.VerifiedCalls())
	assert.Empty(t, resolver.Calls(), "a cached verification skips the address check")
	assert.Empty(t, verifier.VerifyCalls())
}

func TestValidate_CachedVerificationStillChecksScheme(t *testing.T) {
	t.Parallel()
	verifier := &fakeVerifier{verified: true}
	allow, err := netguard.ParseAllowlist([]string{"10.0.0.0/8"})
	require.NoError(t, err)
	p := newProvider(t, func(c *destmcp.Config) {
		c.Verifier = verifier
		c.Guard = &netguard.Guard{Allowlist: allow}
	})
	d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "http://10.0.0.5:8080/hook" })

	requireValidationError(t, p.Validate(context.Background(), d), "config.url", "https_required")
	assert.Empty(t, verifier.VerifiedCalls())
}

func TestValidate_CacheLookupFailureChecksEverything(t *testing.T) {
	t.Parallel()
	verifier := &fakeVerifier{verified: true, verifiedErr: errors.New("redis down")}
	resolver := newResolver()
	p := newProvider(t, func(c *destmcp.Config) {
		c.Verifier = verifier
		c.Guard = &netguard.Guard{Resolver: resolver}
	})
	d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "https://internal.example.com/hook" })
	requireValidationError(t, p.Validate(context.Background(), d), "config.url", "address_not_allowed")
	assert.NotEmpty(t, resolver.Calls())
}

func TestValidate_Verification(t *testing.T) {
	t.Parallel()

	t.Run("challenge failures", func(t *testing.T) {
		t.Parallel()
		for _, reason := range []string{
			mcpevents.ReasonChallengeFailed, mcpevents.ReasonHTTP4xx, mcpevents.ReasonHTTP5xx,
			mcpevents.ReasonTimeout, mcpevents.ReasonTLSError, mcpevents.ReasonConnectionRefused,
		} {
			p := newProvider(t, func(c *destmcp.Config) {
				c.Verifier = &fakeVerifier{verifyErr: mcpevents.CallbackEndpointError(reason)}
			})
			requireValidationError(t, p.Validate(context.Background(), newSubscription(t)), "config.url", reason)
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		t.Parallel()
		limited := mcpevents.ResourceExhausted(mcpevents.LimitVerificationRate, 10, 1500*time.Millisecond)
		p := newProvider(t, func(c *destmcp.Config) { c.Verifier = &fakeVerifier{verifyErr: limited} })
		err := p.Validate(context.Background(), newSubscription(t))
		var rl *destregistry.ErrDestinationRateLimited
		require.ErrorAs(t, err, &rl)
		assert.Equal(t, "rate_limited", rl.Code)
		assert.Equal(t, 1500*time.Millisecond, rl.RetryAfter)
	})

	t.Run("wrapped mcp error", func(t *testing.T) {
		t.Parallel()
		wrapped := errors.Join(errors.New("verify"), mcpevents.InvalidParams(mcpevents.FieldSecret, mcpevents.ReasonInvalidSecret))
		p := newProvider(t, func(c *destmcp.Config) { c.Verifier = &fakeVerifier{verifyErr: wrapped} })
		requireValidationError(t, p.Validate(context.Background(), newSubscription(t)), "credentials.secret", "invalid_secret")
	})

	t.Run("verifier failure", func(t *testing.T) {
		t.Parallel()
		p := newProvider(t, func(c *destmcp.Config) {
			c.Verifier = &fakeVerifier{verifyErr: errors.New("redis: connection refused")}
		})
		err := p.Validate(context.Background(), newSubscription(t))
		requireUnavailable(t, err)
		assert.NotContains(t, err.Error(), "redis")
	})

	t.Run("signs the challenge with every valid secret", func(t *testing.T) {
		t.Parallel()
		verifier := &fakeVerifier{}
		p := newProvider(t, func(c *destmcp.Config) { c.Verifier = verifier })
		d := newSubscription(t, func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = testSecret(2)
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = "2999-01-01T00:00:00Z"
		})
		require.NoError(t, p.Validate(context.Background(), d))
		calls := verifier.VerifyCalls()
		require.Len(t, calls, 1)
		require.Len(t, calls[0].Secrets, 2)
		assert.Equal(t, secretKey(t, testSecret(1)), calls[0].Secrets[0].Key)
		assert.Equal(t, secretKey(t, testSecret(2)), calls[0].Secrets[1].Key)
		require.NotNil(t, calls[0].Secrets[1].InvalidAt)
	})
}

func TestValidate_NoVerifierFailsClosed(t *testing.T) {
	t.Parallel()
	var typedNil *mcpevents.Verifier
	for name, v := range map[string]destmcp.Verifier{"nil": nil, "typed nil": typedNil} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resolver := newResolver()
			p := newProvider(t, func(c *destmcp.Config) {
				c.Verifier = v
				c.Guard = &netguard.Guard{Resolver: resolver}
			})
			requireUnavailable(t, p.Validate(context.Background(), newSubscription(t)))
			assert.Empty(t, resolver.Calls())
		})
	}
}

func TestValidate_ThroughRegistry(t *testing.T) {
	t.Parallel()
	registry := newRegistry(t, destmcp.Config{Guard: &netguard.Guard{Resolver: newResolver()}, Verifier: &fakeVerifier{}})
	d := newSubscription(t, func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "https://internal.example.com/hook" })
	err := registry.ValidateDestination(context.Background(), d)
	var verr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, []destregistry.ValidationErrorDetail{{Field: "config.url", Type: "address_not_allowed"}}, verr.Errors)

	require.NoError(t, registry.ValidateDestination(context.Background(), newSubscription(t)))
}

func requireUnavailable(t *testing.T, err error) {
	t.Helper()
	var ue *destregistry.ErrDestinationUnavailable
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, "verification_unavailable", ue.Code)
}
