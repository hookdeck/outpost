package mcpworker

import (
	"context"
	"slices"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"go.uber.org/zap"
)

// TenantSnapshot is what tenant.subscription.updated compares.
type TenantSnapshot struct {
	Topics            []string
	DestinationsCount int
}

// TenantUpdates reports tenant.subscription.updated for the tenants whose
// subscriptions a pass deleted: Snapshot runs once per tenant, before the
// pass deletes its first subscription, and Report once the pass is over.
type TenantUpdates interface {
	// Snapshot captures the tenant; ok=false skips reporting it.
	Snapshot(ctx context.Context, tenantID string) (snap TenantSnapshot, ok bool)
	// Report emits tenant.subscription.updated when the tenant's topics or
	// destination count differ from snap.
	Report(ctx context.Context, tenantID string, snap TenantSnapshot)
}

// TenantRetriever reads tenants with their topics and destination count.
type TenantRetriever interface {
	RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error)
}

// NewTenantUpdates reports through emitter, like the destination
// endpoints do. It does nothing while emitter filters the topic out.
func NewTenantUpdates(store TenantRetriever, emitter opevents.Emitter, logger *logging.Logger) TenantUpdates {
	return &tenantUpdates{store: store, emitter: emitter, logger: logger}
}

type tenantUpdates struct {
	store   TenantRetriever
	emitter opevents.Emitter
	logger  *logging.Logger
}

func (u *tenantUpdates) Snapshot(ctx context.Context, tenantID string) (TenantSnapshot, bool) {
	if !u.emitter.Enabled(opevents.TopicTenantSubscriptionUpdated) {
		return TenantSnapshot{}, false
	}
	tenant, err := u.store.RetrieveTenant(ctx, tenantID)
	if err != nil || tenant == nil {
		if err != nil && ctx.Err() == nil {
			u.logger.Warn("mcp subscriptions worker: failed to retrieve tenant for subscription update",
				zap.String("tenant_id", tenantID), zap.Error(err))
		}
		return TenantSnapshot{}, false
	}
	return TenantSnapshot{Topics: tenant.Topics, DestinationsCount: tenant.DestinationsCount}, true
}

func (u *tenantUpdates) Report(ctx context.Context, tenantID string, prev TenantSnapshot) {
	tenant, err := u.store.RetrieveTenant(ctx, tenantID)
	if err != nil {
		u.logger.Warn("mcp subscriptions worker: failed to retrieve tenant for subscription update",
			zap.String("tenant_id", tenantID), zap.Error(err))
		return
	}
	if tenant == nil {
		return // deleted meanwhile
	}
	now := TenantSnapshot{Topics: tenant.Topics, DestinationsCount: tenant.DestinationsCount}
	if slices.Equal(now.Topics, prev.Topics) && now.DestinationsCount == prev.DestinationsCount {
		return
	}
	if err := u.emitter.Emit(ctx, opevents.TenantSubscriptionUpdatedEvent(opevents.TenantSubscriptionUpdatedData{
		TenantID:                  tenantID,
		Topics:                    now.Topics,
		PreviousTopics:            prev.Topics,
		DestinationsCount:         now.DestinationsCount,
		PreviousDestinationsCount: prev.DestinationsCount,
	})); err != nil {
		u.logger.Warn("mcp subscriptions worker: failed to emit subscription update",
			zap.String("tenant_id", tenantID), zap.Error(err))
	}
}
