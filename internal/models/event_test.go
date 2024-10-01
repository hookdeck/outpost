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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupClickHouseConnection(t *testing.T) clickhouse.Conn {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{"127.0.0.1:9000"},
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
	return conn
}

func TestEventModel_InsertMany(t *testing.T) {
	t.Parallel()

	conn := setupClickHouseConnection(t)

	ctx := context.Background()

	eventModel := models.NewEventModel()

	event := &models.Event{
		ID: uuid.New().String(),
		// ID:            "78168f99-2a05-4e3b-950a-60621b01a6c4",
		TenantID:      "tenant:" + uuid.New().String(),
		DestinationID: "destination:" + uuid.New().String(),
		Topic:         "user_created",
		// Topic: "user_updated",
		Time: time.Now(),
		Data: map[string]interface{}{
			"mykey": "myvalue",
		},
	}

	err := eventModel.InsertMany(ctx, conn, []*models.Event{event})
	assert.Nil(t, err)
}

func TestEventModel_List(t *testing.T) {
	t.Parallel()

	conn := setupClickHouseConnection(t)

	ctx := context.Background()

	eventModel := models.NewEventModel()

	events, err := eventModel.List(ctx, conn)
	require.Nil(t, err)

	for i := range events {
		log.Println(events[i])
	}
}

func TestDeliveryModel_InsertMany(t *testing.T) {
	t.Parallel()

	conn := setupClickHouseConnection(t)

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
