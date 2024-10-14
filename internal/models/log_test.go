// TODO

package models_test

import (
	"context"
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
	require.NoError(t, err)

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{endpoint},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "",
		},
		// Debug: true,
		// Debugf: func(format string, v ...any) {
		// 	fmt.Printf(format+"\n", v...)
		// },
	})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS eventkit"))
	require.NoError(t, conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS eventkit.events (
			id String,
			tenant_id String,
			destination_id String,
			topic String,
			time DateTime,
			metadata String,
			data String
		)
		ENGINE = MergeTree
		ORDER BY (id, time);
	`))
	require.NoError(t, conn.Exec(ctx, `
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
	`))

	return conn, cleanup
}

func TestIntegrationLogStore_EventCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	conn, cleanup := setupClickHouseConnection(t)
	defer cleanup()

	ctx := context.Background()
	logStore := models.NewLogStore(conn)

	tenantID := uuid.New().String()
	events := []*models.Event{}
	baseTime := time.Now()
	for i := 0; i < 20; i++ {
		events = append(events,
			testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Second)),
			),
		)
	}

	t.Run("insert many event", func(t *testing.T) {
		assert.NoError(t, logStore.InsertManyEvent(ctx, events))
	})

	t.Run("list event empty", func(t *testing.T) {
		queriedEvents, nextCursor, err := logStore.ListEvent(ctx, models.ListEventRequest{
			TenantID: "unknown",
			Limit:    5,
			Cursor:   "",
		})
		require.NoError(t, err)
		assert.Empty(t, queriedEvents)
		assert.Empty(t, nextCursor)
	})

	var cursor string
	t.Run("list event", func(t *testing.T) {
		queriedEvents, nextCursor, err := logStore.ListEvent(ctx, models.ListEventRequest{
			TenantID: tenantID,
			Limit:    5,
			Cursor:   "",
		})
		require.NoError(t, err)
		require.Len(t, queriedEvents, 5)
		for i := 0; i < 5; i++ {
			require.Equal(t, events[i].ID, queriedEvents[i].ID)
		}
		assert.Equal(t, events[4].Time.UTC().Format(time.RFC3339), nextCursor)
		cursor = nextCursor
	})

	t.Run("list event with cursor", func(t *testing.T) {
		queriedEvents, nextCursor, err := logStore.ListEvent(ctx, models.ListEventRequest{
			TenantID: tenantID,
			Limit:    5,
			Cursor:   cursor,
		})
		require.NoError(t, err)
		require.Len(t, queriedEvents, 5)
		for i := 0; i < 5; i++ {
			require.Equal(t, events[5+i].ID, queriedEvents[i].ID)
		}
		assert.Equal(t, events[9].Time.UTC().Format(time.RFC3339), nextCursor)
	})
}

func TestIntegrationLogStore_DeliveryCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	conn, cleanup := setupClickHouseConnection(t)
	defer cleanup()

	ctx := context.Background()
	logStore := models.NewLogStore(conn)

	event := testutil.EventFactory.Any()
	require.NoError(t, logStore.InsertManyEvent(ctx, []*models.Event{&event}))

	deliveries := []*models.Delivery{}
	baseTime := time.Now()
	for i := 0; i < 20; i++ {
		deliveries = append(deliveries, &models.Delivery{
			ID:              uuid.New().String(),
			EventID:         event.ID,
			DeliveryEventID: uuid.New().String(),
			DestinationID:   uuid.New().String(),
			Status:          "success",
			Time:            baseTime.Add(-time.Duration(i) * time.Second),
		})
	}

	t.Run("insert many delivery", func(t *testing.T) {
		require.NoError(t, logStore.InsertManyDelivery(ctx, deliveries))
	})

	t.Run("list delivery empty", func(t *testing.T) {
		queriedDeliveries, err := logStore.ListDelivery(ctx, models.ListDeliveryRequest{
			EventID: "unknown",
		})
		require.NoError(t, err)
		assert.Empty(t, queriedDeliveries)
	})

	t.Run("list delivery", func(t *testing.T) {
		queriedDeliveries, err := logStore.ListDelivery(ctx, models.ListDeliveryRequest{
			EventID: event.ID,
		})
		require.NoError(t, err)
		assert.Len(t, queriedDeliveries, len(deliveries))
		for i := 0; i < len(deliveries); i++ {
			assert.Equal(t, deliveries[i].ID, queriedDeliveries[i].ID)
		}
	})
}
