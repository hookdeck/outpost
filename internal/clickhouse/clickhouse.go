package clickhouse

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type (
	DB   = chdriver.Conn
	Rows = driver.Rows
)

type ClickHouseConfig struct {
	Addr     string
	Username string
	Password string
	Database string
}

func New(config *ClickHouseConfig) (DB, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{config.Addr},
		Auth: clickhouse.Auth{
			Database: config.Database,
			Username: config.Username,
			Password: config.Password,
		},

		// Debug: true,
		// Debugf: func(format string, v ...any) {
		// 	fmt.Printf(format+"\n", v...)
		// },
	})
	return conn, err
}

// TODO: replace this with a proper migration tool
func RunMigration_Temporary(ctx context.Context, db DB) error {
	if err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events (
			id String,
			tenant_id String,
			destination_id String,
			topic String,
			eligible_for_retry Bool,
			time DateTime,
			metadata String,
			data String
		)
		ENGINE = MergeTree
		ORDER BY (id, time);
	`); err != nil {
		return err
	}
	if err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deliveries (
			id String,
			delivery_event_id String,
			event_id String,
			destination_id String,
			status String,
			time DateTime
		)
		ENGINE = ReplacingMergeTree
		ORDER BY (id, time);
	`); err != nil {
		return err
	}
	return nil
}
