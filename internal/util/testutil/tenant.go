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
	tenant := models.Tenant{
		ID:                idgen.String(),
		DestinationsCount: 0,
		Topics:            []string{},
		Metadata:          nil,
		CreatedAt:         time.Now(),
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
