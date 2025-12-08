package logstore

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/chlogstore"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/pglogstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ListDeliveryEventRequest = driver.ListDeliveryEventRequest
type ListDeliveryEventResponse = driver.ListDeliveryEventResponse
type RetrieveEventRequest = driver.RetrieveEventRequest
type RetrieveDeliveryEventRequest = driver.RetrieveDeliveryEventRequest

type LogStore interface {
	ListDeliveryEvent(context.Context, ListDeliveryEventRequest) (ListDeliveryEventResponse, error)
	RetrieveEvent(ctx context.Context, request RetrieveEventRequest) (*models.Event, error)
	RetrieveDeliveryEvent(ctx context.Context, request RetrieveDeliveryEventRequest) (*models.DeliveryEvent, error)
	InsertManyDeliveryEvent(context.Context, []*models.DeliveryEvent) error
}

type DriverOpts struct {
	CH clickhouse.DB
	PG *pgxpool.Pool
}

func (d *DriverOpts) Close() error {
	if d.CH != nil {
		return d.CH.Close()
	}
	if d.PG != nil {
		d.PG.Close()
	}
	return nil
}

func NewLogStore(ctx context.Context, driverOpts DriverOpts) (LogStore, error) {
	if driverOpts.CH != nil {
		return chlogstore.NewLogStore(driverOpts.CH), nil
	}
	if driverOpts.PG != nil {
		return pglogstore.NewLogStore(driverOpts.PG), nil
	}

	return nil, errors.New("no driver provided")
}

type Config struct {
	ClickHouse *clickhouse.ClickHouseConfig
	Postgres   *string
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

	if cfg.Postgres != nil && *cfg.Postgres != "" {
		pgDB, err := pgxpool.New(context.Background(), *cfg.Postgres)
		if err != nil {
			return DriverOpts{}, err
		}
		driverOpts.PG = pgDB
	}

	return driverOpts, nil
}
