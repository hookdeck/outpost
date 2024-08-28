package tenant

import (
	"context"
	"fmt"

	"github.com/hookdeck/EventKit/internal/redis"
)

type Tenant struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

type TenantModel struct {
	redisClient *redis.Client
}

func NewTenantModel(redisClient *redis.Client) *TenantModel {
	return &TenantModel{
		redisClient: redisClient,
	}
}

func (m *TenantModel) Get(c context.Context, id string) (*Tenant, error) {
	destination, err := m.redisClient.Get(c, redisTenantID(id)).Result()
	if err == redis.Nil {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &Tenant{
		ID:        id,
		CreatedAt: destination,
	}, nil
}

func (m *TenantModel) Set(c context.Context, tenant Tenant) error {
	if err := m.redisClient.Set(c, redisTenantID(tenant.ID), tenant.CreatedAt, 0).Err(); err != nil {
		return err
	}
	return nil
}

func (m *TenantModel) Clear(c context.Context, id string) (*Tenant, error) {
	destination, err := m.Get(c, id)
	if err != nil {
		return nil, err
	}
	if destination == nil {
		return nil, nil
	}
	if err := m.redisClient.Del(c, redisTenantID(id)).Err(); err != nil {
		return nil, err
	}
	return destination, nil
}

func redisTenantID(tenantID string) string {
	return fmt.Sprintf("tenant:%s", tenantID)
}
