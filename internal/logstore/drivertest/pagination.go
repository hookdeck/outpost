package drivertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination/paginationtest"
	"github.com/stretchr/testify/require"
)

// testPagination tests cursor-based pagination using paginationtest.Suite.
func testPagination(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	baseTime := time.Now().Truncate(time.Second)
	farPast := baseTime.Add(-48 * time.Hour)

	t.Run("ListDeliveryEvent", func(t *testing.T) {
		var tenantID, destinationID, idPrefix string

		suite := paginationtest.Suite[*models.DeliveryEvent]{
			Name: "ListDeliveryEvent",

			Cleanup: func(ctx context.Context) error {
				tenantID = idgen.String()
				destinationID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *models.DeliveryEvent {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)
				deliveryTime := eventTime.Add(100 * time.Millisecond)

				event := &models.Event{
					ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
					TenantID:         tenantID,
					DestinationID:    destinationID,
					Topic:            "test.topic",
					EligibleForRetry: true,
					Time:             eventTime,
					Metadata:         map[string]string{},
					Data:             map[string]any{},
				}

				delivery := &models.Delivery{
					ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
					EventID:       event.ID,
					DestinationID: destinationID,
					Status:        "success",
					Time:          deliveryTime,
					Code:          "200",
				}

				return &models.DeliveryEvent{
					ID:            fmt.Sprintf("%s_de_%03d", idPrefix, i),
					DestinationID: destinationID,
					Event:         *event,
					Delivery:      delivery,
				}
			},

			InsertMany: func(ctx context.Context, items []*models.DeliveryEvent) error {
				return logStore.InsertManyDeliveryEvent(ctx, items)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*models.DeliveryEvent], error) {
				res, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
					TenantID:  tenantID,
					Limit:     opts.Limit,
					SortOrder: opts.Order,
					Next:      opts.Next,
					Prev:      opts.Prev,
					Start:     &farPast,
				})
				if err != nil {
					return paginationtest.ListResult[*models.DeliveryEvent]{}, err
				}
				return paginationtest.ListResult[*models.DeliveryEvent]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(de *models.DeliveryEvent) string {
				return de.Delivery.ID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})

	t.Run("ListDeliveryEvent_WithDestinationFilter", func(t *testing.T) {
		var tenantID, targetDestID, otherDestID, idPrefix string

		suite := paginationtest.Suite[*models.DeliveryEvent]{
			Name: "ListDeliveryEvent_WithDestinationFilter",

			Cleanup: func(ctx context.Context) error {
				tenantID = idgen.String()
				targetDestID = idgen.Destination()
				otherDestID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *models.DeliveryEvent {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)
				deliveryTime := eventTime.Add(100 * time.Millisecond)

				destID := targetDestID
				if i%2 == 1 {
					destID = otherDestID
				}

				event := &models.Event{
					ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
					TenantID:         tenantID,
					DestinationID:    destID,
					Topic:            "test.topic",
					EligibleForRetry: true,
					Time:             eventTime,
					Metadata:         map[string]string{},
					Data:             map[string]any{},
				}

				delivery := &models.Delivery{
					ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
					EventID:       event.ID,
					DestinationID: destID,
					Status:        "success",
					Time:          deliveryTime,
					Code:          "200",
				}

				return &models.DeliveryEvent{
					ID:            fmt.Sprintf("%s_de_%03d", idPrefix, i),
					DestinationID: destID,
					Event:         *event,
					Delivery:      delivery,
				}
			},

			InsertMany: func(ctx context.Context, items []*models.DeliveryEvent) error {
				return logStore.InsertManyDeliveryEvent(ctx, items)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*models.DeliveryEvent], error) {
				res, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
					TenantID:       tenantID,
					DestinationIDs: []string{targetDestID},
					Limit:          opts.Limit,
					SortOrder:      opts.Order,
					Next:           opts.Next,
					Prev:           opts.Prev,
					Start:          &farPast,
				})
				if err != nil {
					return paginationtest.ListResult[*models.DeliveryEvent]{}, err
				}
				return paginationtest.ListResult[*models.DeliveryEvent]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(de *models.DeliveryEvent) string {
				return de.Delivery.ID
			},

			Matches: func(de *models.DeliveryEvent) bool {
				return de.DestinationID == targetDestID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})

	t.Run("ListEvent", func(t *testing.T) {
		var eventTenantID, eventDestID, idPrefix string

		suite := paginationtest.Suite[*models.Event]{
			Name: "ListEvent",

			Cleanup: func(ctx context.Context) error {
				eventTenantID = idgen.String()
				eventDestID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *models.Event {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)

				return &models.Event{
					ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
					TenantID:         eventTenantID,
					DestinationID:    eventDestID,
					Topic:            "test.topic",
					EligibleForRetry: true,
					Time:             eventTime,
					Metadata:         map[string]string{},
					Data:             map[string]any{},
				}
			},

			InsertMany: func(ctx context.Context, items []*models.Event) error {
				des := make([]*models.DeliveryEvent, len(items))
				for i, evt := range items {
					deliveryTime := evt.Time.Add(100 * time.Millisecond)
					des[i] = &models.DeliveryEvent{
						ID:            fmt.Sprintf("%s_de_%03d", idPrefix, i),
						DestinationID: evt.DestinationID,
						Event:         *evt,
						Delivery: &models.Delivery{
							ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
							EventID:       evt.ID,
							DestinationID: evt.DestinationID,
							Status:        "success",
							Time:          deliveryTime,
							Code:          "200",
						},
					}
				}
				return logStore.InsertManyDeliveryEvent(ctx, des)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*models.Event], error) {
				res, err := logStore.ListEvent(ctx, driver.ListEventRequest{
					TenantID:   eventTenantID,
					Limit:      opts.Limit,
					SortOrder:  opts.Order,
					Next:       opts.Next,
					Prev:       opts.Prev,
					EventStart: &farPast,
				})
				if err != nil {
					return paginationtest.ListResult[*models.Event]{}, err
				}
				return paginationtest.ListResult[*models.Event]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(e *models.Event) string {
				return e.ID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})

	t.Run("ListEvent_WithDestinationFilter", func(t *testing.T) {
		var tenantID, targetDestID, otherDestID, idPrefix string

		suite := paginationtest.Suite[*models.Event]{
			Name: "ListEvent_WithDestinationFilter",

			Cleanup: func(ctx context.Context) error {
				tenantID = idgen.String()
				targetDestID = idgen.Destination()
				otherDestID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *models.Event {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)

				destID := targetDestID
				if i%2 == 1 {
					destID = otherDestID
				}

				return &models.Event{
					ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
					TenantID:         tenantID,
					DestinationID:    destID,
					Topic:            "test.topic",
					EligibleForRetry: true,
					Time:             eventTime,
					Metadata:         map[string]string{},
					Data:             map[string]any{},
				}
			},

			InsertMany: func(ctx context.Context, items []*models.Event) error {
				des := make([]*models.DeliveryEvent, len(items))
				for i, evt := range items {
					deliveryTime := evt.Time.Add(100 * time.Millisecond)
					des[i] = &models.DeliveryEvent{
						ID:            fmt.Sprintf("%s_de_%03d", idPrefix, i),
						DestinationID: evt.DestinationID,
						Event:         *evt,
						Delivery: &models.Delivery{
							ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
							EventID:       evt.ID,
							DestinationID: evt.DestinationID,
							Status:        "success",
							Time:          deliveryTime,
							Code:          "200",
						},
					}
				}
				return logStore.InsertManyDeliveryEvent(ctx, des)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*models.Event], error) {
				res, err := logStore.ListEvent(ctx, driver.ListEventRequest{
					TenantID:       tenantID,
					DestinationIDs: []string{targetDestID},
					Limit:          opts.Limit,
					SortOrder:      opts.Order,
					Next:           opts.Next,
					Prev:           opts.Prev,
					EventStart:     &farPast,
				})
				if err != nil {
					return paginationtest.ListResult[*models.Event]{}, err
				}
				return paginationtest.ListResult[*models.Event]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(e *models.Event) string {
				return e.ID
			},

			Matches: func(e *models.Event) bool {
				return e.DestinationID == targetDestID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})
}
