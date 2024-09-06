package models

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
)

type Tenant struct {
	ID        string    `json:"id" redis:"id"`
	CreatedAt time.Time `json:"created_at" redis:"created_at"`
}

type TenantModel struct{}

func NewTenantModel() *TenantModel {
	return &TenantModel{}
}

func (m *TenantModel) Get(ctx context.Context, cmdable redis.Cmdable, id string) (*Tenant, error) {
	hash, err := cmdable.HGetAll(ctx, redisTenantID(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(hash) == 0 {
		return nil, nil
	}
	tenant := &Tenant{}
	if err = tenant.parseRedisHash(hash); err != nil {
		return nil, err
	}
	return tenant, nil
}

func (m *TenantModel) Set(ctx context.Context, cmdable redis.Cmdable, tenant Tenant) error {
	return cmdable.HSet(ctx, redisTenantID(tenant.ID), tenant).Err()
}

func (m *TenantModel) Clear(ctx context.Context, cmdable redis.Cmdable, id string) error {
	return cmdable.Del(ctx, redisTenantID(id)).Err()
}

func (t *Tenant) parseRedisHash(hash map[string]string) error {
	if hash["id"] == "" {
		return fmt.Errorf("missing id")
	}
	t.ID = hash["id"]
	if hash["created_at"] == "" {
		return fmt.Errorf("missing created_at")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, hash["created_at"])
	if err != nil {
		return err
	}
	t.CreatedAt = createdAt
	return nil
}

func redisTenantID(tenantID string) string {
	return fmt.Sprintf("tenant:%s", tenantID)
}
