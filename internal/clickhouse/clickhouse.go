package clickhouse

import (
	"crypto/tls"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type (
	DB   = chdriver.Conn
	Rows = driver.Rows
)

type ClickHouseConfig struct {
	Addr       string
	Username   string
	Password   string
	Database   string
	TLSEnabled bool
	// MaxOpenConns sizes the connection pool; 0 uses the client default.
	MaxOpenConns int
	// DialTimeout bounds opening a connection and waiting for a free pooled
	// one; 0 means 10s.
	DialTimeout time.Duration
	// ReadTimeout bounds waiting for a query's response; 0 means 30s.
	ReadTimeout time.Duration
	// IdleClose closes the connection pool after this long with no calls, so a
	// quiet process doesn't keep connections that may have been dropped; the next
	// call reconnects. 0 means 5m.
	IdleClose time.Duration
}

const (
	defaultDialTimeout = 10 * time.Second
	defaultReadTimeout = 30 * time.Second
	defaultIdleClose   = 5 * time.Minute
)

func New(config *ClickHouseConfig) (DB, error) {
	idle := config.IdleClose
	if idle <= 0 {
		idle = defaultIdleClose
	}
	return newConn(func() (driver.Conn, error) { return clickhouse.Open(options(config)) }, idle)
}

func options(config *ClickHouseConfig) *clickhouse.Options {
	dialTimeout := config.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = defaultDialTimeout
	}
	readTimeout := config.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = defaultReadTimeout
	}
	opts := &clickhouse.Options{
		Addr: []string{config.Addr},
		Auth: clickhouse.Auth{
			Database: config.Database,
			Username: config.Username,
			Password: config.Password,
		},
		DialTimeout: dialTimeout,
		ReadTimeout: readTimeout,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},

		// Debug: true,
		// Debugf: func(format string, v ...any) {
		// 	fmt.Printf(format+"\n", v...)
		// },
	}

	if config.TLSEnabled {
		opts.TLS = &tls.Config{}
	}

	if config.MaxOpenConns > 0 {
		opts.MaxOpenConns = config.MaxOpenConns
		opts.MaxIdleConns = max(1, config.MaxOpenConns/2)
	}

	return opts
}
