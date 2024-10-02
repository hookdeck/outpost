package clickhouse

import (
	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2"
)

type DB = chdriver.Conn

func New() (DB, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{"127.0.0.1:9000"},
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
	return conn, err
}
