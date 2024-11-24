package testinfra

import (
	"context"
	"log"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/util/testutil"
	chTestcontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

func NewClickHouseConfig(t *testing.T) clickhouse.ClickHouseConfig {
	chConfig := clickhouse.ClickHouseConfig{
		Addr:     ensureClickHouse(),
		Username: "default",
		Password: "",
		Database: "default",
	}
	database := "test_" + testutil.RandomString(10)
	initDB(&chConfig, database)
	t.Cleanup(func() {
		clearDB(chConfig, database)
	})
	return chConfig
}

func initDB(chConfig *clickhouse.ClickHouseConfig, database string) {
	chDB, err := clickhouse.New(chConfig)
	if err != nil {
		panic(err)
	}
	if err := chDB.Exec(context.Background(), "CREATE DATABASE IF NOT EXISTS "+database); err != nil {
		log.Println("cmd", "CREATE DATABASE IF NOT EXISTS "+database)
		panic(err)
	}
	chConfig.Database = database
}

func clearDB(chConfig clickhouse.ClickHouseConfig, database string) {
	chConfig.Database = "default" // ensure connecting to default DB
	chDB, err := clickhouse.New(&chConfig)
	if err != nil {
		panic(err)
	}
	if err := chDB.Exec(context.Background(), "DROP DATABASE "+database); err != nil {
		panic(err)
	}
}

var chOnce sync.Once

func ensureClickHouse() string {
	cfg := ReadConfig()
	if cfg.ClickHouseURL == "" {
		chOnce.Do(func() {
			startCHTestcontainer(cfg)
		})
	}
	return cfg.ClickHouseURL
}

func startCHTestcontainer(cfg *Config) {
	ctx := context.Background()

	clickHouseContainer, err := chTestcontainer.Run(ctx,
		"clickhouse/clickhouse-server:latest",
		chTestcontainer.WithUsername("default"),
		chTestcontainer.WithPassword(""),
		chTestcontainer.WithDatabase("default"),
	)
	if err != nil {
		panic(err)
	}

	endpoint, err := clickHouseContainer.PortEndpoint(ctx, "9000/tcp", "")
	if err != nil {
		panic(err)
	}
	log.Printf("ClickHouse running at %s", endpoint)
	cfg.ClickHouseURL = endpoint
	cfg.cleanupFns = append(cfg.cleanupFns, func() {
		if err := clickHouseContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate container: %s", err)
		}
	})
}
