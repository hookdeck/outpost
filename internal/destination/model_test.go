package destination_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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
		ID:         uuid.New().String(),
		Type:       "webhooks",
		Topics:     []string{"user.created", "user.updated"},
		CreatedAt:  time.Now(),
		DisabledAt: nil,
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, actual)
		assert.Nil(t, err)
	})

	t.Run("sets", func(t *testing.T) {
		err := model.Set(context.Background(), input)
		assert.Nil(t, err)
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("overrides", func(t *testing.T) {
		input.Type = "not-webhooks"

		err := model.Set(context.Background(), input)
		assert.Nil(t, err)

		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("clears", func(t *testing.T) {
		deleted, err := model.Clear(context.Background(), input.ID)
		assert.Nil(t, err)
		assertEqualDestination(t, input, *deleted)

		actual, err := model.Get(context.Background(), input.ID)
		assert.Nil(t, actual)
		assert.Nil(t, err)
	})
}

func assertEqualDestination(t *testing.T, expected, actual destination.Destination) {
	assert.Equal(t, expected.ID, actual.ID)
	assert.Equal(t, expected.Type, actual.Type)
	assert.Equal(t, expected.Topics, actual.Topics)
	assert.True(t, cmp.Equal(expected.CreatedAt, actual.CreatedAt))
	assert.True(t, cmp.Equal(expected.DisabledAt, actual.DisabledAt))
}
