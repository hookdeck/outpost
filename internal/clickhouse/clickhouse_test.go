package clickhouse

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOptions(t *testing.T) {
	t.Run("zero config keeps client defaults", func(t *testing.T) {
		opts := options(&ClickHouseConfig{Addr: "localhost:9000"})
		assert.Equal(t, 10*time.Second, opts.DialTimeout)
		assert.Equal(t, 30*time.Second, opts.ReadTimeout)
		assert.Equal(t, 0, opts.MaxOpenConns)
		assert.Equal(t, 0, opts.MaxIdleConns)
	})

	t.Run("pool size sets idle to half", func(t *testing.T) {
		opts := options(&ClickHouseConfig{Addr: "localhost:9000", MaxOpenConns: 40})
		assert.Equal(t, 40, opts.MaxOpenConns)
		assert.Equal(t, 20, opts.MaxIdleConns)
	})

	t.Run("pool of one keeps one idle", func(t *testing.T) {
		opts := options(&ClickHouseConfig{Addr: "localhost:9000", MaxOpenConns: 1})
		assert.Equal(t, 1, opts.MaxIdleConns)
	})

	t.Run("dial timeout", func(t *testing.T) {
		opts := options(&ClickHouseConfig{Addr: "localhost:9000", DialTimeout: 3 * time.Second})
		assert.Equal(t, 3*time.Second, opts.DialTimeout)
	})

	t.Run("read timeout", func(t *testing.T) {
		opts := options(&ClickHouseConfig{Addr: "localhost:9000", ReadTimeout: 5 * time.Second})
		assert.Equal(t, 5*time.Second, opts.ReadTimeout)
	})
}
