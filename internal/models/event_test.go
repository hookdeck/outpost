package models_test

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupClickHouseConnection(t *testing.T) (clickhouse.Conn, func()) {
	endpoint, cleanup, err := testutil.StartTestContainerClickHouse()
	require.Nil(t, err)

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{endpoint},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "",
		},
		Debug: true,
		Debugf: func(format string, v ...any) {
			fmt.Printf(format+"\n", v...)
		},
	})
	require.Nil(t, err)

	ctx := context.Background()
	err = conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS eventkit")
	require.Nil(t, err)
	err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS eventkit.events (
			id String,
			tenant_id String,
			destination_id String,
			topic String,
			time DateTime,
			data String
		)
		ENGINE = MergeTree
		ORDER BY (id, time);
	`)
	require.Nil(t, err)
	err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS eventkit.deliveries (
			id String,
			delivery_event_id String,
			event_id String,
			destination_id String,
			status String,
			time DateTime
		)
		ENGINE = ReplacingMergeTree
		ORDER BY (id, time);
	`)
	require.Nil(t, err)

	return conn, cleanup
}

func TestIntegrationEventModel_InsertMany(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	conn, cleanup := setupClickHouseConnection(t)
	defer cleanup()

	ctx := context.Background()

	eventModel := models.NewEventModel()

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

	err := eventModel.InsertMany(ctx, conn, []*models.Event{event})
	assert.Nil(t, err)
}

func TestIntegrationEventModel_List(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	conn, cleanup := setupClickHouseConnection(t)
	defer cleanup()

	ctx := context.Background()

	eventModel := models.NewEventModel()

	events, err := eventModel.List(ctx, conn)
	require.Nil(t, err)

	for i := range events {
		log.Println(events[i])
	}
}

func TestIntegrationDeliveryModel_InsertMany(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	conn, cleanup := setupClickHouseConnection(t)
	defer cleanup()

	ctx := context.Background()

	deliveryModel := models.NewDeliveryModel()

	delivery := &models.Delivery{
		ID:              uuid.New().String(),
		DeliveryEventID: "de:" + uuid.New().String(),
		EventID:         "event:" + uuid.New().String(),
		DestinationID:   "destination:" + uuid.New().String(),
		Status:          "success",
		Time:            time.Now(),
	}

	err := deliveryModel.InsertMany(ctx, conn, []*models.Delivery{delivery})
	assert.Nil(t, err)
}
