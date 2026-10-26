package destmcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func orderCatalog(t *testing.T) *topicschema.Catalog {
	t.Helper()
	c, err := topicschema.NewCatalog([]string{topicOrderCreated, "order.updated"}, topicschema.Definitions{
		topicOrderCreated: {
			Description: "An order was placed.",
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{
				"currency":{"type":"string","enum":["USD","EUR"]},
				"total":{"type":"number"}
			}}`),
		},
	})
	require.NoError(t, err)
	return c
}

func withCatalog(t *testing.T) *destmcp.Provider {
	t.Helper()
	return newProvider(t, func(c *destmcp.Config) { c.Catalog = orderCatalog(t) })
}

func TestCatalog_ValidSubscription(t *testing.T) {
	t.Parallel()
	p := withCatalog(t)
	require.NoError(t, p.ValidateCreate(newSubscription(t)))
	require.NoError(t, p.ValidateCreate(newSubscription(t, func(d *models.Destination) { delete(d.Config, destmcp.ConfigArguments) })))
}

func TestCatalog_TopicMustBeAnEvent(t *testing.T) {
	t.Parallel()
	p := withCatalog(t)
	for _, topic := range []string{"order.updated", "unknown"} {
		err := p.ValidateCreate(newSubscription(t, func(d *models.Destination) { d.Topics = models.Topics{topic} }))
		requireValidationError(t, err, "topics", "not_found")
	}
}

func TestCatalog_ArgumentsAreChecked(t *testing.T) {
	t.Parallel()
	p := withCatalog(t)
	err := p.ValidateCreate(newSubscription(t, func(d *models.Destination) {
		d.Config[destmcp.ConfigArguments] = `{"currency":5,"total":"x"}`
	}))
	var verr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, []destregistry.ValidationErrorDetail{
		{Field: "config.arguments.currency", Type: "invalid"},
		{Field: "config.arguments.total", Type: "invalid"},
	}, verr.Errors)

	err = p.ValidateCreate(newSubscription(t, func(d *models.Destination) {
		d.Config[destmcp.ConfigArguments] = `{"currency":"GBP"}`
	}))
	requireValidationError(t, err, "config.arguments.currency", "invalid")

	err = p.ValidateCreate(newSubscription(t, func(d *models.Destination) {
		d.Config[destmcp.ConfigArguments] = `{"orderId":"o_1"}`
	}))
	requireValidationError(t, err, "config.arguments", "invalid")
}

func TestCatalog_FilterMustBeAbsent(t *testing.T) {
	t.Parallel()
	p := withCatalog(t)
	d := newSubscription(t, func(d *models.Destination) {
		d.Filter = models.Filter{"data": map[string]any{"currency": "EUR"}}
	})
	requireValidationError(t, p.ValidateCreate(d), "filter", "invalid")
}

func TestCatalog_ValidationDoesNotRecheckTheCatalog(t *testing.T) {
	t.Parallel()
	p := withCatalog(t)
	stale := newSubscription(t, func(d *models.Destination) {
		d.Topics = models.Topics{"order.updated"}
		d.Config[destmcp.ConfigArguments] = `{"currency":"GBP"}`
	})
	require.NoError(t, p.Validate(context.Background(), stale), "a rotation after the definitions changed")
}

func TestCatalog_PreprocessDerivesFilter(t *testing.T) {
	t.Parallel()
	p := withCatalog(t)
	d := newSubscription(t)
	require.NoError(t, p.Preprocess(d, nil, nil))
	raw, err := json.Marshal(d.Filter)
	require.NoError(t, err)
	assert.JSONEq(t, `{"data":{"currency":"USD","total":{"$gte":100}}}`, string(raw))
	require.NoError(t, p.Validate(context.Background(), d))

	none := newSubscription(t, func(d *models.Destination) { delete(d.Config, destmcp.ConfigArguments) })
	require.NoError(t, p.Preprocess(none, nil, nil))
	assert.Nil(t, none.Filter)
}

func TestCatalog_PreprocessKeepsFilterOnUpdate(t *testing.T) {
	t.Parallel()
	p := withCatalog(t)
	original := newSubscription(t)
	require.NoError(t, p.Preprocess(original, nil, nil))

	refresh := newSubscription(t, func(d *models.Destination) { d.Credentials[destmcp.CredentialSecret] = testSecret(9) })
	refresh.Filter = original.Filter
	require.NoError(t, p.Preprocess(refresh, original, nil))
	require.NoError(t, p.Validate(context.Background(), refresh))

	edited := newSubscription(t)
	edited.Filter = models.Filter{"data": map[string]any{"currency": "EUR"}}
	requireValidationError(t, p.Preprocess(edited, original, nil), "filter", "immutable")

	createdWithoutCatalog := newSubscription(t)
	createdWithoutCatalog.Filter = models.Filter{"data": map[string]any{"x": 1}}
	rotated := newSubscription(t, func(d *models.Destination) { d.Credentials[destmcp.CredentialSecret] = testSecret(9) })
	rotated.Filter = createdWithoutCatalog.Filter
	require.NoError(t, p.Preprocess(rotated, createdWithoutCatalog, nil))
	require.NoError(t, p.Validate(context.Background(), rotated))

	noFilter := newSubscription(t)
	update := newSubscription(t)
	require.NoError(t, p.Preprocess(update, noFilter, nil), "a destination stored without a derived filter keeps none")
	assert.Nil(t, update.Filter)
}

func TestNoCatalog_ArgumentsAndFilterNotChecked(t *testing.T) {
	t.Parallel()
	p := newProvider(t)
	d := newSubscription(t, func(d *models.Destination) {
		d.Topics = models.Topics{"anything"}
		d.Config[destmcp.ConfigArguments] = `{"whatever":1}`
		d.Filter = models.Filter{"data": map[string]any{"x": 1}}
	})
	require.NoError(t, p.ValidateCreate(d))
	require.NoError(t, p.Validate(context.Background(), d))
	require.NoError(t, p.Preprocess(d, nil, nil))
	assert.Equal(t, models.Filter{"data": map[string]any{"x": 1}}, d.Filter)
}
