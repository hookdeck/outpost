package destmcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
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
	d := newSubscription(t, p)

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

func TestValidate_NoArguments(t *testing.T) {
	t.Parallel()
	p := newProvider(t)
	d := newSubscription(t, p, func(d *models.Destination) {
		d.Config[destmcp.ConfigArguments] = "{}"
		rederive(d)
	})
	require.NoError(t, p.Validate(context.Background(), d))
}

func TestValidate_FieldErrors(t *testing.T) {
	t.Parallel()

	type want struct {
		field, typ string
		// cause is the expected mcp_error data; nil means no mcp_error cause.
		cause map[string]any
		kind  mcpevents.Kind
	}
	invalidParams := func(field, typ, mcpField, reason string) want {
		return want{field: field, typ: typ, kind: mcpevents.KindInvalidParams, cause: map[string]any{"field": mcpField, "reason": reason}}
	}
	internal := func(field, typ string) want { return want{field: field, typ: typ} }

	tests := []struct {
		name   string
		mutate func(*models.Destination)
		want   want
	}{
		{"wrong type", func(d *models.Destination) { d.Type = "webhook" }, internal("type", "invalid_type")},
		{"missing principal", func(d *models.Destination) { delete(d.Config, destmcp.ConfigPrincipal) },
			invalidParams("config.principal", "required", "principal", "required")},
		{"principal too long", func(d *models.Destination) {
			d.Config[destmcp.ConfigPrincipal] = strings.Repeat("p", destmcp.MaxPrincipalBytes+1)
		}, invalidParams("config.principal", "too_large", "principal", "too_large")},
		{"missing event", func(d *models.Destination) { delete(d.Config, destmcp.ConfigEvent) },
			invalidParams("config.event", "required", "name", "required")},
		{"unknown event", func(d *models.Destination) {
			d.Config[destmcp.ConfigEvent] = "nope"
			d.Topics = models.Topics{"nope"}
		}, want{field: "config.event", typ: "not_found", kind: mcpevents.KindNotFound, cause: map[string]any{"kind": "event"}}},
		{"event not MCP-enabled", func(d *models.Destination) {
			d.Config[destmcp.ConfigEvent] = topicAudit
			d.Topics = models.Topics{topicAudit}
		}, want{field: "config.event", typ: "not_found", kind: mcpevents.KindNotFound, cause: map[string]any{"kind": "event"}}},
		{"topics differ from event", func(d *models.Destination) { d.Topics = models.Topics{topicOrderShipped} }, internal("topics", "invalid")},
		{"wildcard topics", func(d *models.Destination) { d.Topics = models.Topics{"*"} }, internal("topics", "invalid")},
		{"extra topic", func(d *models.Destination) { d.Topics = append(d.Topics, topicOrderShipped) }, internal("topics", "invalid")},
		{"arguments not JSON", func(d *models.Destination) { d.Config[destmcp.ConfigArguments] = "{nope" },
			invalidParams("config.arguments", "invalid", "arguments", "invalid")},
		{"arguments not an object", func(d *models.Destination) { d.Config[destmcp.ConfigArguments] = `["USD"]` },
			invalidParams("config.arguments", "invalid", "arguments", "invalid")},
		{"arguments missing", func(d *models.Destination) { delete(d.Config, destmcp.ConfigArguments) },
			invalidParams("config.arguments", "invalid", "arguments", "invalid")},
		{"arguments not canonical", func(d *models.Destination) {
			d.Config[destmcp.ConfigArguments] = `{"total":{"$gte":100},"currency":"USD"}`
		}, invalidParams("config.arguments", "invalid", "arguments", "invalid")},
		{"arguments with duplicate keys", func(d *models.Destination) {
			d.Config[destmcp.ConfigArguments] = `{"currency":"USD","currency":"EUR"}`
		}, invalidParams("config.arguments", "invalid", "arguments", "invalid")},
		{"arguments too large", func(d *models.Destination) {
			d.Config[destmcp.ConfigArguments] = `{"currency":"` + strings.Repeat("a", mcpevents.MaxArgumentsBytes) + `"}`
		}, invalidParams("config.arguments", "too_large", "arguments", "too_large")},
		{"url invalid", func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "not a url" },
			invalidParams("config.url", "invalid_url", "delivery.url", "invalid_url")},
		{"url missing", func(d *models.Destination) { delete(d.Config, destmcp.ConfigURL) },
			invalidParams("config.url", "invalid_url", "delivery.url", "invalid_url")},
		{"url with credentials", func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "https://user:pass@receiver.example.com/" },
			invalidParams("config.url", "invalid_url", "delivery.url", "invalid_url")},
		{"url scheme", func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "ftp://receiver.example.com/" },
			invalidParams("config.url", "https_required", "delivery.url", "https_required")},
		{"url not normalized", func(d *models.Destination) {
			d.Config[destmcp.ConfigURL] = "https://RECEIVER.example.com:443/mcp-events/abc123"
		}, invalidParams("config.url", "invalid_url", "delivery.url", "invalid_url")},
		{"secret missing", func(d *models.Destination) { delete(d.Credentials, destmcp.CredentialSecret) },
			invalidParams("credentials.secret", "invalid_secret", "delivery.secret", "invalid_secret")},
		{"secret not whsec", func(d *models.Destination) { d.Credentials[destmcp.CredentialSecret] = "secret" },
			invalidParams("credentials.secret", "invalid_secret", "delivery.secret", "invalid_secret")},
		{"secret too short", func(d *models.Destination) { d.Credentials[destmcp.CredentialSecret] = "whsec_c2hvcnQ=" },
			invalidParams("credentials.secret", "invalid_secret", "delivery.secret", "invalid_secret")},
		{"previous secret invalid", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = "nope"
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = "2030-01-01T00:00:00Z"
		}, internal("credentials.previous_secret", "invalid")},
		{"previous secret without expiry", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = testSecret(2)
		}, internal("credentials.previous_secret_invalid_at", "invalid")},
		{"previous secret expiry without secret", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = "2030-01-01T00:00:00Z"
		}, internal("credentials.previous_secret", "invalid")},
		{"subscription id missing", func(d *models.Destination) { delete(d.Config, destmcp.ConfigSubscriptionID) },
			internal("config.subscription_id", "invalid")},
		{"subscription id not derived", func(d *models.Destination) {
			d.Config[destmcp.ConfigSubscriptionID] = "sub_00000000000000000000000000000000"
			d.ID = "sub_00000000000000000000000000000000"
		}, internal("config.subscription_id", "invalid")},
		{"destination id differs", func(d *models.Destination) { d.ID = "des_1" }, internal("config.subscription_id", "invalid")},
		{"key changed without a new id", func(d *models.Destination) { d.Config[destmcp.ConfigPrincipal] = "someone_else" },
			internal("config.subscription_id", "invalid")},
		{"schema hash missing", func(d *models.Destination) { delete(d.Config, destmcp.ConfigSchemaHash) },
			internal("config.schema_hash", "invalid")},
		{"schema hash of another topic", func(d *models.Destination) {
			d.Config[destmcp.ConfigSchemaHash] = strings.Repeat("0", 64)
		}, internal("config.schema_hash", "invalid")},
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
			d := newSubscription(t, p, tt.mutate)

			cause := requireValidationError(t, p.Validate(context.Background(), d), tt.want.field, tt.want.typ)
			if tt.want.cause == nil {
				assert.Nil(t, cause, "a caller bug is not an mcp_error")
			} else {
				require.NotNil(t, cause)
				assert.Equal(t, tt.want.kind, cause.Kind)
				assert.Equal(t, tt.want.cause, cause.Data)
			}
			assert.Empty(t, verifier.VerifyCalls(), "static failures never reach verification")
			assert.Empty(t, verifier.VerifiedCalls())
			assert.Empty(t, resolver.Calls(), "static failures never resolve")
		})
	}
}

func TestValidate_ArgumentsSchema(t *testing.T) {
	t.Parallel()
	const sentinel = "S3NT1N3L"
	tests := map[string]string{
		"wrong type":         `{"currency":12345}`,
		"wrong operator":     `{"total":{"$regex":"` + sentinel + `"}}`,
		"unknown argument":   `{"` + sentinel + `":1}`,
		"hidden argument":    `{"orderId":"` + sentinel + `"}`,
		"wrong element type": `{"currency":["USD",12345]}`,
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newProvider(t)
			d := newSubscription(t, p, func(d *models.Destination) {
				d.Config[destmcp.ConfigArguments] = args
				rederive(d)
			})
			cause := requireValidationError(t, p.Validate(context.Background(), d), "config.arguments", "invalid")
			require.NotNil(t, cause)
			assert.Equal(t, mcpevents.KindInvalidParams, cause.Kind)
			assert.Equal(t, "arguments", cause.Data["field"])
			assert.Equal(t, "invalid", cause.Data["reason"])
			errs, ok := cause.Data["errors"].([]string)
			require.True(t, ok, "errors lists the schema problems")
			require.NotEmpty(t, errs)
			for _, e := range errs {
				assert.True(t, strings.HasPrefix(e, "arguments"), e)
			}
			body, err := json.Marshal(cause)
			require.NoError(t, err)
			assert.NotContains(t, string(body), sentinel, "argument values never appear in errors")
			assert.NotContains(t, string(body), "12345")
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
	var bodies []string
	for name, rawURL := range notAllowed {
		t.Run(name, func(t *testing.T) {
			verifier := &fakeVerifier{}
			p := newProvider(t, func(c *destmcp.Config) { c.Verifier = verifier })
			d := newSubscription(t, p, func(d *models.Destination) {
				d.Config[destmcp.ConfigURL] = rawURL
				rederive(d)
			})
			err := p.Validate(context.Background(), d)
			cause := requireValidationError(t, err, "config.url", "address_not_allowed")
			require.NotNil(t, cause)
			assert.Equal(t, map[string]any{"field": "delivery.url", "reason": "address_not_allowed"}, cause.Data)
			assert.Empty(t, verifier.VerifyCalls(), "no challenge to a refused address")

			body, jerr := cause.MarshalJSON()
			require.NoError(t, jerr)
			bodies = append(bodies, string(body))
			host := mustURL(t, rawURL).Hostname()
			for _, s := range []string{err.Error(), string(body)} {
				assert.NotContains(t, s, host)
				assert.NotContains(t, s, "10.0.0.1")
				assert.NotContains(t, s, "192.168")
				assert.NotContains(t, s, "no such host")
			}
		})
	}
	for _, b := range bodies[1:] {
		assert.Equal(t, bodies[0], b, "every refusal renders the same mcp_error, so the check can't probe internal DNS")
	}

	t.Run("http on a global address", func(t *testing.T) {
		t.Parallel()
		p := newProvider(t)
		d := newSubscription(t, p, func(d *models.Destination) {
			d.Config[destmcp.ConfigURL] = "http://receiver.example.com/mcp-events/abc123"
			rederive(d)
		})
		cause := requireValidationError(t, p.Validate(context.Background(), d), "config.url", "https_required")
		require.NotNil(t, cause)
		assert.Equal(t, map[string]any{"field": "delivery.url", "reason": "https_required"}, cause.Data)
	})

	// With a loopback allowlist, http needs the host resolved; whatever it
	// resolves to (or not), a refused http URL gets the same answer.
	t.Run("http answers don't depend on DNS", func(t *testing.T) {
		t.Parallel()
		allow, _, err := netguard.ParseAllowlist([]string{"localhost"})
		require.NoError(t, err)
		var bodies []string
		for _, host := range []string{"receiver.example.com", "internal.example.com", "nx.example.com", "servfail.example.com", "10.0.0.1"} {
			p := newProvider(t, func(c *destmcp.Config) {
				c.Guard = &netguard.Guard{Allowlist: allow, Resolver: newResolver()}
			})
			d := newSubscription(t, p, func(d *models.Destination) {
				d.Config[destmcp.ConfigURL] = "http://" + host + "/hook"
				rederive(d)
			})
			cause := requireValidationError(t, p.Validate(context.Background(), d), "config.url", "https_required")
			require.NotNil(t, cause, host)
			body, err := cause.MarshalJSON()
			require.NoError(t, err)
			bodies = append(bodies, string(body))
		}
		for _, b := range bodies[1:] {
			assert.Equal(t, bodies[0], b)
		}
	})

	t.Run("global v6 address", func(t *testing.T) {
		t.Parallel()
		p := newProvider(t)
		d := newSubscription(t, p, func(d *models.Destination) {
			d.Config[destmcp.ConfigURL] = "https://public-v6.example.com/hook"
			rederive(d)
		})
		require.NoError(t, p.Validate(context.Background(), d))
	})

	t.Run("allowlisted loopback over http", func(t *testing.T) {
		t.Parallel()
		p := newProvider(t, func(c *destmcp.Config) { c.Guard = loopbackGuard(t) })
		d := newSubscription(t, p, func(d *models.Destination) {
			d.Config[destmcp.ConfigURL] = "http://127.0.0.1:8080/hook"
			rederive(d)
		})
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
	d := newSubscription(t, p, func(d *models.Destination) {
		d.Config[destmcp.ConfigURL] = "https://internal.example.com/hook"
		rederive(d)
	})

	require.NoError(t, p.Validate(context.Background(), d))
	assert.Equal(t, [][3]string{{testTenantID, testPrincipal, "https://internal.example.com/hook"}}, verifier.VerifiedCalls())
	assert.Empty(t, resolver.Calls(), "a cached verification skips the address check")
	assert.Empty(t, verifier.VerifyCalls())
}

func TestValidate_CacheLookupFailureChecksEverything(t *testing.T) {
	t.Parallel()
	verifier := &fakeVerifier{verified: true, verifiedErr: errors.New("redis down")}
	resolver := newResolver()
	p := newProvider(t, func(c *destmcp.Config) {
		c.Verifier = verifier
		c.Guard = &netguard.Guard{Resolver: resolver}
	})
	d := newSubscription(t, p, func(d *models.Destination) {
		d.Config[destmcp.ConfigURL] = "https://internal.example.com/hook"
		rederive(d)
	})
	requireValidationError(t, p.Validate(context.Background(), d), "config.url", "address_not_allowed")
	assert.NotEmpty(t, resolver.Calls())
}

func TestValidate_Verification(t *testing.T) {
	t.Parallel()

	t.Run("challenge failures are mcp errors", func(t *testing.T) {
		t.Parallel()
		for _, reason := range []string{
			mcpevents.ReasonChallengeFailed, mcpevents.ReasonHTTP4xx, mcpevents.ReasonHTTP5xx,
			mcpevents.ReasonTimeout, mcpevents.ReasonTLSError, mcpevents.ReasonConnectionRefused,
		} {
			p := newProvider(t, func(c *destmcp.Config) {
				c.Verifier = &fakeVerifier{verifyErr: mcpevents.CallbackEndpointError(reason)}
			})
			cause := requireValidationError(t, p.Validate(context.Background(), newSubscription(t, p)), "config.url", reason)
			require.NotNil(t, cause)
			assert.Equal(t, mcpevents.KindCallbackEndpointError, cause.Kind)
			assert.Equal(t, map[string]any{"reason": reason}, cause.Data)
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		t.Parallel()
		limited := mcpevents.ResourceExhausted(mcpevents.LimitVerificationRate, 10, 0)
		p := newProvider(t, func(c *destmcp.Config) { c.Verifier = &fakeVerifier{verifyErr: limited} })
		cause := requireValidationError(t, p.Validate(context.Background(), newSubscription(t, p)), "config.url", "resource_exhausted")
		require.NotNil(t, cause)
		assert.Equal(t, mcpevents.KindResourceExhausted, cause.Kind)
		assert.Equal(t, map[string]any{"limit": "verification_rate", "max": 10}, cause.Data)
	})

	t.Run("wrapped mcp error", func(t *testing.T) {
		t.Parallel()
		wrapped := errorsJoin(mcpevents.InvalidParams(mcpevents.FieldDeliverySecret, mcpevents.ReasonInvalidSecret))
		p := newProvider(t, func(c *destmcp.Config) { c.Verifier = &fakeVerifier{verifyErr: wrapped} })
		cause := requireValidationError(t, p.Validate(context.Background(), newSubscription(t, p)), "credentials.secret", "invalid_secret")
		require.NotNil(t, cause)
	})

	t.Run("verifier failure is not the client's", func(t *testing.T) {
		t.Parallel()
		storeErr := errors.New("redis: connection refused")
		p := newProvider(t, func(c *destmcp.Config) { c.Verifier = &fakeVerifier{verifyErr: storeErr} })
		err := p.Validate(context.Background(), newSubscription(t, p))
		cause := requireValidationError(t, err, "config.url", "verification_unavailable")
		assert.Nil(t, cause)
		assert.ErrorIs(t, err, destmcp.ErrVerificationUnavailable)
		assert.ErrorIs(t, err, storeErr)
	})

	t.Run("cancelled request", func(t *testing.T) {
		t.Parallel()
		p := newProvider(t, func(c *destmcp.Config) { c.Verifier = &fakeVerifier{verifyErr: context.Canceled} })
		err := p.Validate(context.Background(), newSubscription(t, p))
		assert.ErrorIs(t, err, destmcp.ErrVerificationUnavailable)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("signs the challenge with every valid secret", func(t *testing.T) {
		t.Parallel()
		verifier := &fakeVerifier{}
		p := newProvider(t, func(c *destmcp.Config) { c.Verifier = verifier })
		d := newSubscription(t, p, func(d *models.Destination) {
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
			err := p.Validate(context.Background(), newSubscription(t, p))
			cause := requireValidationError(t, err, "config.url", "verification_unavailable")
			assert.Nil(t, cause)
			assert.ErrorIs(t, err, destmcp.ErrVerificationUnavailable)
			assert.Empty(t, resolver.Calls())
		})
	}
}

func TestValidate_CodeProfile(t *testing.T) {
	t.Parallel()
	p := newProvider(t, func(c *destmcp.Config) { c.CodeProfile = mcpevents.CodeProfileSEP3415 })
	d := newSubscription(t, p, func(d *models.Destination) {
		d.Config[destmcp.ConfigEvent] = "nope"
		d.Topics = models.Topics{"nope"}
	})
	cause := requireValidationError(t, p.Validate(context.Background(), d), "config.event", "not_found")
	require.NotNil(t, cause)
	assert.Equal(t, -32023, cause.Code())

	_, err := destmcp.New(metadata.NewMetadataLoader(""), destmcp.Config{CodeProfile: "bogus"})
	assert.Error(t, err)
}

// The registry hands the provider's error to the caller unchanged, Cause
// included.
func TestValidate_ThroughRegistry(t *testing.T) {
	t.Parallel()
	registry := newRegistry(t, destmcp.Config{Catalog: newCatalog(t), Guard: &netguard.Guard{Resolver: newResolver()}, Verifier: &fakeVerifier{}})
	p := mcpProvider(t, registry)
	d := newSubscription(t, p, func(d *models.Destination) {
		d.Config[destmcp.ConfigURL] = "https://internal.example.com/hook"
		rederive(d)
	})
	err := registry.ValidateDestination(context.Background(), d)
	var verr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, []destregistry.ValidationErrorDetail{{Field: "config.url", Type: "address_not_allowed"}}, verr.Errors)
	var mcpErr *mcpevents.Error
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, mcpevents.KindInvalidParams, mcpErr.Kind)

	require.NoError(t, registry.ValidateDestination(context.Background(), newSubscription(t, p)))
}

// errorsJoin wraps err the way a caller might.
func errorsJoin(err error) error {
	return errors.Join(errors.New("verify"), err)
}
