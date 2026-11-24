package tenantstore_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicStore(t *testing.T) {
	ctx := t.Context()
	raw := tenantstore.NewMemTenantStore()
	public := tenantstore.NewPublicStore(raw)

	tenant := testutil.TenantFactory.Any()
	require.NoError(t, public.UpsertTenant(ctx, tenant))
	hook := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID), testutil.DestinationFactory.WithType("webhook"))
	sub := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID), testutil.DestinationFactory.WithType(models.DestinationTypeMCP))
	require.NoError(t, public.CreateDestination(ctx, hook))
	require.NoError(t, public.CreateDestination(ctx, sub))

	got, err := public.RetrieveDestination(ctx, tenant.ID, sub.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "an internal destination answers as unknown")
	got, err = public.RetrieveDestination(ctx, tenant.ID, hook.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, hook.ID, got.ID)

	listed, err := public.ListDestination(ctx, tenantstore.ListDestinationRequest{TenantID: tenant.ID})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, hook.ID, listed[0].ID)

	got, err = raw.RetrieveDestination(ctx, tenant.ID, sub.ID)
	require.NoError(t, err)
	assert.NotNil(t, got, "the wrapped store still holds it")
}
