package logstore

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/chlogstore"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
)

type ListEventRequest = driver.ListEventRequest
type ListDeliveryRequest = driver.ListDeliveryRequest

type LogStore interface {
	ListEvent(context.Context, ListEventRequest) ([]*models.Event, string, error)
	RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error)
	ListDelivery(ctx context.Context, request ListDeliveryRequest) ([]*models.Delivery, error)
	InsertManyEvent(context.Context, []*models.Event) error
	InsertManyDelivery(context.Context, []*models.Delivery) error
}

type DriverOpts struct {
	CH clickhouse.DB
}

func (d *DriverOpts) Close() error {
	if d.CH != nil {
		return d.CH.Close()
	}
	return nil
}

func NewLogStore(ctx context.Context, driverOpts DriverOpts) (LogStore, error) {
	if driverOpts.CH != nil {
		return chlogstore.NewLogStore(driverOpts.CH), nil
	}

	return nil, errors.New("no driver provided")
}

type Config struct {
	ClickHouse *clickhouse.ClickHouseConfig
}

func MakeDriverOpts(cfg Config) (DriverOpts, error) {
	driverOpts := DriverOpts{}

	if cfg.ClickHouse != nil {
		chDB, err := clickhouse.New(cfg.ClickHouse)
		if err != nil {
			return DriverOpts{}, err
		}

		driverOpts.CH = chDB
	}

	return driverOpts, nil
}
