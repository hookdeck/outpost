package destination_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destination"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDestinationModel(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	model := destination.NewDestinationModel(redisClient)

	input := destination.Destination{
		ID:   uuid.New().String(),
		Name: "Test Destination",
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, actual, "model.Get() should return nil when there's no value")
		assert.Nil(t, err, "model.Get() should not return an error when there's no value")
	})

	t.Run("sets", func(t *testing.T) {
		err := model.Set(context.Background(), input)
		assert.Nil(t, err, "model.Set() should not return an error")

		value, err := redisClient.Get(context.Background(), "destination:"+input.ID).Result()
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, input.Name, value, "model.Set() should set destination name %s", input.Name)
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, err, "model.Get() should not return an error")
		assert.Equal(t, input, *actual, "model.Get() should return %s", input)
	})

	t.Run("overrides", func(t *testing.T) {
		input.Name = "Test Destination 2"

		err := model.Set(context.Background(), input)
		assert.Nil(t, err, "model.Set() should not return an error", input)

		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, err, "model.Get() should not return an error")
		assert.Equal(t, input, *actual, "model.Get() should return %s", input)
	})

	t.Run("clears", func(t *testing.T) {
		deleted, err := model.Clear(context.Background(), input.ID)
		assert.Nil(t, err, "model.Clear() should not return an error")
		assert.Equal(t, *deleted, input, "model.Clear() should return deleted value", input)

		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, actual, "model.Clear() should properly remove value")
		assert.Nil(t, err, "model.Clear() should properly remove value")
	})
}
