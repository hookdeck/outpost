package destwebhook_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func credentialHeadersDestination(config, credentials map[string]string) models.Destination {
	cfg := map[string]string{"url": "http://example.com/webhook"}
	for k, v := range config {
		cfg[k] = v
	}
	return testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(cfg),
		testutil.DestinationFactory.WithCredentials(credentials),
	)
}

func requireValidationField(t *testing.T, err error, field, typ string) {
	t.Helper()
	var verr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &verr)
	require.Len(t, verr.Errors, 1)
	assert.Equal(t, field, verr.Errors[0].Field)
	assert.Equal(t, typ, verr.Errors[0].Type)
}

func TestWebhookPublisher_CredentialHeaders(t *testing.T) {
	t.Parallel()

	t.Run("sends credential headers alongside config headers", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t)
		destination := credentialHeadersDestination(
			map[string]string{"custom_headers": `{"x-tenant-id":"tenant-abc"}`},
			map[string]string{
				"secret":         "test-secret",
				"custom_headers": `{"Authorization":"Bearer sk_live_123"}`,
			},
		)

		publisher, err := provider.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)
		event := testutil.EventFactory.Any()
		req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		assert.Equal(t, "Bearer sk_live_123", req.Header.Get("Authorization"))
		assert.Equal(t, "tenant-abc", req.Header.Get("x-tenant-id"))
	})

	t.Run("rejects a name in both config and credentials, case-insensitively", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t)
		destination := credentialHeadersDestination(
			map[string]string{"custom_headers": `{"authorization":"Bearer a"}`},
			map[string]string{"custom_headers": `{"Authorization":"Bearer b"}`},
		)

		err := provider.Validate(context.Background(), &destination)
		requireValidationField(t, err, "credentials.custom_headers.Authorization", "invalid")
	})

	t.Run("rejects reserved and malformed header names", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t)

		destination := credentialHeadersDestination(nil, map[string]string{"custom_headers": `{"Host":"x"}`})
		requireValidationField(t, provider.Validate(context.Background(), &destination), "credentials.custom_headers.Host", "forbidden")

		destination = credentialHeadersDestination(nil, map[string]string{"custom_headers": `{"bad header":"x"}`})
		requireValidationField(t, provider.Validate(context.Background(), &destination), "credentials.custom_headers.bad header", "pattern")

		destination = credentialHeadersDestination(nil, map[string]string{"custom_headers": `{"Authorization":""}`})
		requireValidationField(t, provider.Validate(context.Background(), &destination), "credentials.custom_headers.Authorization", "required")
	})

	t.Run("rejects a value that isn't a JSON object of strings", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t)
		destination := credentialHeadersDestination(nil, map[string]string{"custom_headers": `["Authorization"]`})
		requireValidationField(t, provider.Validate(context.Background(), &destination), "credentials.custom_headers", "invalid")
	})
}

func TestWebhookDestination_HeadersPreprocess(t *testing.T) {
	t.Parallel()

	// update runs a tenant update of field's custom_headers from saved to
	// sent; "" means the key is absent.
	update := func(t *testing.T, field string, saved, sent string) (models.Destination, error) {
		t.Helper()
		provider := NewTestProvider(t)
		original := credentialHeadersDestination(nil, map[string]string{"secret": "whsec_original"})
		updated := credentialHeadersDestination(nil, map[string]string{"secret": "whsec_original"})
		set := func(d *models.Destination, v string) {
			if v == "" {
				return
			}
			if field == "config" {
				d.Config["custom_headers"] = v
			} else {
				d.Credentials["custom_headers"] = v
			}
		}
		set(&original, saved)
		set(&updated, sent)
		err := provider.Preprocess(&updated, &original, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
		return updated, err
	}
	get := func(d models.Destination, field string) (string, bool) {
		if field == "config" {
			v, ok := d.Config["custom_headers"]
			return v, ok
		}
		v, ok := d.Credentials["custom_headers"]
		return v, ok
	}

	for _, field := range []string{"credentials"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			t.Run("a header sent with a value is set; headers not sent are kept", func(t *testing.T) {
				t.Parallel()
				updated, err := update(t, field,
					`{"Authorization":"Bearer old","X-Api-Key":"key-old"}`,
					`{"X-Api-Key":"key-new","X-Extra":"extra"}`)
				require.NoError(t, err)
				got, _ := get(updated, field)
				assert.JSONEq(t, `{"Authorization":"Bearer old","X-Api-Key":"key-new","X-Extra":"extra"}`, got)
			})

			t.Run("a header sent as null is removed", func(t *testing.T) {
				t.Parallel()
				updated, err := update(t, field,
					`{"Authorization":"Bearer old","X-Api-Key":"key-old"}`,
					`{"x-api-key":null}`)
				require.NoError(t, err)
				got, _ := get(updated, field)
				assert.JSONEq(t, `{"Authorization":"Bearer old"}`, got)
			})

			t.Run("names match case-insensitively", func(t *testing.T) {
				t.Parallel()
				updated, err := update(t, field, `{"Authorization":"Bearer old"}`, `{"authorization":"Bearer new"}`)
				require.NoError(t, err)
				got, _ := get(updated, field)
				assert.JSONEq(t, `{"authorization":"Bearer new"}`, got)
			})

			t.Run("a rename that only changes case keeps the header", func(t *testing.T) {
				t.Parallel()
				// Map order is random; repeat so a wrong order would show.
				for range 50 {
					updated, err := update(t, field,
						`{"Authorization":"Bearer old"}`,
						`{"Authorization":null,"authorization":"Bearer old"}`)
					require.NoError(t, err)
					got, _ := get(updated, field)
					require.JSONEq(t, `{"authorization":"Bearer old"}`, got)
				}
			})

			t.Run("two values for the same name are rejected", func(t *testing.T) {
				t.Parallel()
				_, err := update(t, field, "", `{"X-A":"1","x-a":"2"}`)
				var verr *destregistry.ErrDestinationValidation
				require.ErrorAs(t, err, &verr)
				require.Len(t, verr.Errors, 1)
				assert.Contains(t, []string{field + ".custom_headers.X-A", field + ".custom_headers.x-a"}, verr.Errors[0].Field)
				assert.Equal(t, "invalid", verr.Errors[0].Type)
			})

			t.Run("a saved map the request doesn't change is kept as is", func(t *testing.T) {
				t.Parallel()
				saved := `{"X-A":"1","x-a":"2"}`
				updated, err := update(t, field, saved, saved)
				require.NoError(t, err)
				got, _ := get(updated, field)
				assert.Equal(t, saved, got)
			})

			t.Run("removing every header removes the field", func(t *testing.T) {
				t.Parallel()
				updated, err := update(t, field, `{"Authorization":"Bearer old"}`, `{"Authorization":null}`)
				require.NoError(t, err)
				_, ok := get(updated, field)
				assert.False(t, ok)
			})

			t.Run("an empty object changes nothing", func(t *testing.T) {
				t.Parallel()
				updated, err := update(t, field, `{"Authorization":"Bearer old"}`, `{}`)
				require.NoError(t, err)
				got, _ := get(updated, field)
				assert.JSONEq(t, `{"Authorization":"Bearer old"}`, got)
			})

			t.Run("a field cleared by the request stays cleared", func(t *testing.T) {
				t.Parallel()
				updated, err := update(t, field, `{"Authorization":"Bearer old"}`, "")
				require.NoError(t, err)
				_, ok := get(updated, field)
				assert.False(t, ok)
			})
		})
	}

	t.Run("a tenant can set credential headers on create; the signing secret is still generated", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t)
		destination := credentialHeadersDestination(nil, map[string]string{"custom_headers": `{"Authorization":"Bearer x"}`})

		require.NoError(t, provider.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"}))
		assert.Equal(t, `{"Authorization":"Bearer x"}`, destination.Credentials["custom_headers"])
		assert.NotEmpty(t, destination.Credentials["secret"])
	})

	t.Run("a tenant can change credential headers on update; the signing secret is kept", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t)
		original := credentialHeadersDestination(nil, map[string]string{
			"secret":         "whsec_original",
			"custom_headers": `{"Authorization":"Bearer old"}`,
		})
		updated := credentialHeadersDestination(nil, map[string]string{
			"secret":         "whsec_original",
			"custom_headers": `{"Authorization":"Bearer new"}`,
		})

		require.NoError(t, provider.Preprocess(&updated, &original, &destregistry.PreprocessDestinationOpts{Role: "tenant"}))
		assert.Equal(t, `{"Authorization":"Bearer new"}`, updated.Credentials["custom_headers"])
		assert.Equal(t, "whsec_original", updated.Credentials["secret"])
	})

	t.Run("an empty header value is rejected", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t)
		original := credentialHeadersDestination(nil, map[string]string{
			"secret":         "whsec_original",
			"custom_headers": `{"Authorization":"Bearer old"}`,
		})
		updated := credentialHeadersDestination(nil, map[string]string{
			"secret":         "whsec_original",
			"custom_headers": `{"Authorization":""}`,
		})

		require.NoError(t, provider.Preprocess(&updated, &original, &destregistry.PreprocessDestinationOpts{Role: "tenant"}))
		requireValidationField(t, provider.Validate(context.Background(), &updated), "credentials.custom_headers.Authorization", "required")
	})

	t.Run("credential headers survive secret rotation", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t)
		original := credentialHeadersDestination(nil, map[string]string{
			"secret":         "whsec_original",
			"custom_headers": `{"Authorization":"Bearer x"}`,
		})
		updated := credentialHeadersDestination(nil, map[string]string{
			"secret":         "whsec_original",
			"custom_headers": `{"Authorization":"Bearer x"}`,
			"rotate_secret":  "true",
		})

		require.NoError(t, provider.Preprocess(&updated, &original, &destregistry.PreprocessDestinationOpts{Role: "tenant"}))
		assert.Equal(t, `{"Authorization":"Bearer x"}`, updated.Credentials["custom_headers"])
		assert.Equal(t, "whsec_original", updated.Credentials["previous_secret"])
	})
}

func TestWebhookDestination_CredentialHeadersObfuscation(t *testing.T) {
	t.Parallel()

	provider := NewTestProvider(t)
	destination := credentialHeadersDestination(nil, map[string]string{
		"secret":         "whsec_abc",
		"custom_headers": `{"Authorization":"Bearer sk_live_123","X-Api-Key":"short"}`,
	})

	result := provider.ObfuscateDestination(&destination)

	var masked map[string]string
	require.NoError(t, json.Unmarshal([]byte(result.Credentials["custom_headers"]), &masked))
	assert.Equal(t, map[string]string{
		"Authorization": "Bear**************",
		"X-Api-Key":     "*****",
	}, masked)
	// Stored value untouched.
	assert.Equal(t, `{"Authorization":"Bearer sk_live_123","X-Api-Key":"short"}`, destination.Credentials["custom_headers"])
	// The signing secret stays readable, as before.
	assert.Equal(t, "whsec_abc", result.Credentials["secret"])
}

func TestWebhookPublisher_CredentialHeadersOnRedirect(t *testing.T) {
	t.Parallel()

	var got http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/final", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	provider := NewTestProvider(t, destwebhook.WithFollowRedirects(true))
	destination := credentialHeadersDestination(
		map[string]string{
			"url":            origin.URL + "/webhook",
			"custom_headers": `{"X-Tenant-Id":"acme"}`,
		},
		map[string]string{
			"secret":         "test-secret",
			"custom_headers": `{"X-Api-Key":"key-123"}`,
		},
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	event := testutil.EventFactory.Any()
	delivery, err := publisher.Publish(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "success", delivery.Status)

	require.NotNil(t, got, "redirect target was not reached")
	assert.Empty(t, got.Get("X-Api-Key"), "credential header must not follow a redirect to another host")
	assert.Equal(t, "acme", got.Get("X-Tenant-Id"))
}
