package redistenantstore

import (
	"context"

	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

// DeleteTenantOnce runs a single tenant delete attempt against a destination
// list that may be stale, and reports whether the store asked to retry.
func DeleteTenantOnce(ctx context.Context, ts driver.TenantStore, tenantID string, destinationIDs []string) (retry bool, err error) {
	code, err := ts.(*store).deleteTenantOnce(ctx, tenantID, destinationIDs)
	return code == deleteTenantRetry, err
}
