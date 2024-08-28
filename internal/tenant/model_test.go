package tenant_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/tenant"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestTenantModel(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	model := tenant.NewTenantModel(redisClient)

	input := tenant.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, actual, "model.Get() should return nil when there's no value")
		assert.Nil(t, err, "model.Get() should not return an error when there's no value")
	})

	t.Run("sets", func(t *testing.T) {
		err := model.Set(context.Background(), input)
		assert.Nil(t, err, "model.Set() should not return an error")

		hash, err := redisClient.HGetAll(context.Background(), "tenant:"+input.ID).Result()
		if err != nil {
			t.Fatal(err)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, hash["created_at"])
		if err != nil {
			t.Fatal(err)
		}
		assert.True(t, input.CreatedAt.Equal(createdAt), "model.Set() should set tenant created timestamp %s", input.CreatedAt)
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, err, "model.Get() should not return an error")
		assert.True(t, cmp.Equal(input, *actual), "model.Get() should return %s", input)
	})

	t.Run("overrides", func(t *testing.T) {
		input.CreatedAt = time.Now()

		err := model.Set(context.Background(), input)
		assert.Nil(t, err, "model.Set() should not return an error")

		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, err, "model.Get() should not return an error")
		assert.True(t, cmp.Equal(input, *actual), "model.Get() should return %s", input)
	})

	t.Run("clears", func(t *testing.T) {
		deleted, err := model.Clear(context.Background(), input.ID)
		assert.Nil(t, err, "model.Clear() should not return an error")
		assert.True(t, cmp.Equal(*deleted, input), "model.Clear() should return deleted value")

		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, actual, "model.Clear() should properly remove value")
		assert.Nil(t, err, "model.Clear() should properly remove value")
	})
}
