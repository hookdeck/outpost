package testinfra

import (
	"context"
	"log"
	"testing"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/util/testutil"
	chTestcontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

func NewClickHouseConfig(t *testing.T) clickhouse.ClickHouseConfig {
	chConfig := clickhouse.ClickHouseConfig{
		Addr:     ensureClickHouse(t),
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

var clickhouseService = &service{name: "clickhouse", startHint: hintTest}

func ensureClickHouse(t testing.TB) string {
	t.Helper()
	cfg := ReadConfig()
	return clickhouseService.ensure(t, cfg.ClickHouseURL,
		func() (string, error) { return startCHTestcontainer(cfg) },
		func(endpoint string) error {
			chDB, err := clickhouse.New(&clickhouse.ClickHouseConfig{
				Addr:     endpoint,
				Username: "default",
				Database: "default",
			})
			if err != nil {
				return err
			}
			return chDB.Exec(context.Background(), "SELECT 1")
		})
}

func startCHTestcontainer(cfg *Config) (string, error) {
	ctx := context.Background()

	clickHouseContainer, err := chTestcontainer.Run(ctx,
		cfg.Images.ClickHouse,
		chTestcontainer.WithUsername("default"),
		chTestcontainer.WithPassword(""),
		chTestcontainer.WithDatabase("default"),
	)
	if err != nil {
		return "", err
	}

	endpoint, err := clickHouseContainer.PortEndpoint(ctx, "9000/tcp", "")
	if err != nil {
		return "", err
	}
	log.Printf("ClickHouse running at %s", endpoint)
	return endpoint, nil
}
