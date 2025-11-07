package testutil

import (
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
)

// ============================== Mock Tenant ==============================

var TenantFactory = &mockTenantFactory{}

type mockTenantFactory struct {
}

func (f *mockTenantFactory) Any(opts ...func(*models.Tenant)) models.Tenant {
	now := time.Now()
	tenant := models.Tenant{
		ID:                idgen.String(),
		DestinationsCount: 0,
		Topics:            []string{},
		Metadata:          nil,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	for _, opt := range opts {
		opt(&tenant)
	}

	return tenant
}

func (f *mockTenantFactory) WithID(id string) func(*models.Tenant) {
	return func(tenant *models.Tenant) {
		tenant.ID = id
	}
}

func (f *mockTenantFactory) WithMetadata(metadata map[string]string) func(*models.Tenant) {
	return func(tenant *models.Tenant) {
		tenant.Metadata = metadata
	}
}

func (f *mockTenantFactory) WithCreatedAt(createdAt time.Time) func(*models.Tenant) {
	return func(tenant *models.Tenant) {
		tenant.CreatedAt = createdAt
	}
}

func (f *mockTenantFactory) WithUpdatedAt(updatedAt time.Time) func(*models.Tenant) {
	return func(tenant *models.Tenant) {
		tenant.UpdatedAt = updatedAt
	}
}
