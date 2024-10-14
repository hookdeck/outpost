package clickhouse

import (
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
