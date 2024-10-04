// TODO

package models_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationLogRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	conn, cleanup := setupClickHouseConnection(t)
	defer cleanup()

	ctx := context.Background()
	logRepo := models.NewLogRepo(conn)

	t.Run("inserts many event", func(t *testing.T) {
		event := &models.Event{
			ID:            uuid.New().String(),
			TenantID:      "tenant:" + uuid.New().String(),
			DestinationID: "destination:" + uuid.New().String(),
			Topic:         "user_created",
			Time:          time.Now(),
			Data: map[string]interface{}{
				"mykey": "myvalue",
			},
		}

		err := logRepo.InsertManyEvent(ctx, []*models.Event{event})
		assert.Nil(t, err)
	})

	t.Run("lists event", func(t *testing.T) {
		events, err := logRepo.ListEvent(ctx)
		require.Nil(t, err)
		for i := range events {
			log.Println(events[i])
		}
	})

	t.Run("inserts many delivery", func(t *testing.T) {
		delivery := &models.Delivery{
			ID:              uuid.New().String(),
			DeliveryEventID: "de:" + uuid.New().String(),
			EventID:         "event:" + uuid.New().String(),
			DestinationID:   "destination:" + uuid.New().String(),
			Status:          "success",
			Time:            time.Now(),
		}

		err := logRepo.InsertManyDelivery(ctx, []*models.Delivery{delivery})
		assert.Nil(t, err)
	})
}
