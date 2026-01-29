package logstore

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/chlogstore"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/memlogstore"
	"github.com/hookdeck/outpost/internal/logstore/pglogstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TimeFilter = driver.TimeFilter
type ListEventRequest = driver.ListEventRequest
type ListEventResponse = driver.ListEventResponse
type ListAttemptRequest = driver.ListAttemptRequest
type ListAttemptResponse = driver.ListAttemptResponse
type RetrieveEventRequest = driver.RetrieveEventRequest
type RetrieveAttemptRequest = driver.RetrieveAttemptRequest
type AttemptRecord = driver.AttemptRecord
type LogEntry = models.LogEntry

type LogStore interface {
	ListEvent(context.Context, ListEventRequest) (ListEventResponse, error)
	ListAttempt(context.Context, ListAttemptRequest) (ListAttemptResponse, error)
	RetrieveEvent(ctx context.Context, request RetrieveEventRequest) (*models.Event, error)
	RetrieveAttempt(ctx context.Context, request RetrieveAttemptRequest) (*AttemptRecord, error)
	InsertMany(context.Context, []*models.LogEntry) error
}

type DriverOpts struct {
	CH           clickhouse.DB
	PG           *pgxpool.Pool
	DeploymentID string
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
		return chlogstore.NewLogStore(driverOpts.CH, driverOpts.DeploymentID), nil
	}
	if driverOpts.PG != nil {
		return pglogstore.NewLogStore(driverOpts.PG), nil
	}

	return nil, errors.New("no driver provided")
}

// NewMemLogStore returns an in-memory log store for testing.
func NewMemLogStore() LogStore {
	return memlogstore.NewLogStore()
}

type Config struct {
	ClickHouse   *clickhouse.ClickHouseConfig
	Postgres     *string
	DeploymentID string
}

func MakeDriverOpts(cfg Config) (DriverOpts, error) {
	driverOpts := DriverOpts{
		DeploymentID: cfg.DeploymentID,
	}

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
