package tenant

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

type TenantModel struct {
	redisClient *redis.Client
}

func NewTenantModel(redisClient *redis.Client) *TenantModel {
	return &TenantModel{
		redisClient: redisClient,
	}
}

func (m *TenantModel) Get(c context.Context, id string) (*Tenant, error) {
	hash, err := m.redisClient.HGetAll(c, redisTenantID(id)).Result()
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

func (m *TenantModel) Set(c context.Context, tenant Tenant) error {
	if err := m.redisClient.HSet(c, redisTenantID(tenant.ID), tenant).Err(); err != nil {
		return err
	}
	return nil
}

func (m *TenantModel) Clear(c context.Context, id string) (*Tenant, error) {
	tenant, err := m.Get(c, id)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		return nil, nil
	}
	if err := m.redisClient.Del(c, redisTenantID(id)).Err(); err != nil {
		return nil, err
	}
	return tenant, nil
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
