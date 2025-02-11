package testinfra

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	pgTestcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func NewPostgresConfig(t *testing.T) string {
	pgDB := &PGDB{}
	pgAddr := ensurePostgres()
	defaultPGURL := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s", "outpost", "outpost", pgAddr, "default", "disable")
	database := "test_" + testutil.RandomString(10)
	pgURL := strings.Replace(defaultPGURL, "default", database, 1)
	pgDB.init(defaultPGURL, database)
	t.Cleanup(func() {
		pgDB.clear(defaultPGURL, database)
	})
	return pgURL
}

type PGDB struct{}

func (pgDB *PGDB) init(url, database string) {
	db, err := pgxpool.New(context.Background(), url)
	if err != nil {
		panic(err)
	}
	if _, err := db.Exec(context.Background(), "CREATE DATABASE "+database); err != nil {
		log.Println("cmd", "CREATE DATABASE "+database)
		panic(err)
	}
}

func (pgDB *PGDB) clear(url, database string) {
	db, err := pgxpool.New(context.Background(), url)
	if err != nil {
		panic(err)
	}
	if _, err := db.Exec(context.Background(), "DROP DATABASE "+database); err != nil {
		panic(err)
	}
}

func (pgDB *PGDB) getPGHost(pgURL string) string {
	u, err := url.Parse(pgURL)
	if err != nil {
		return "localhost"
	}
	return u.Hostname()
}

func (pgDB *PGDB) getPGPort(pgURL string) int {
	if strings.Contains(pgURL, "://") {
		u, err := url.Parse(pgURL)
		if err != nil {
			log.Println("err", err)
			return 5432
		}
		log.Println("u", u)
		port, _ := strconv.Atoi(u.Port())
		log.Println("port", port)
		if port == 0 {
			return 5432
		}
		return port
	}

	// Handle localhost:port format
	parts := strings.Split(pgURL, ":")
	if len(parts) != 2 {
		return 5432
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Println("err", err)
		return 5432
	}
	return port
}

var pgOnce sync.Once

func ensurePostgres() string {
	cfg := ReadConfig()
	if cfg.PostgresURL == "" {
		pgOnce.Do(func() {
			startPGTestcontainer(cfg)
		})
	}
	return cfg.PostgresURL
}

func startPGTestcontainer(cfg *Config) {
	ctx := context.Background()

	pgContainer, err := pgTestcontainer.Run(ctx,
		"postgres:latest",
		pgTestcontainer.WithUsername("outpost"),
		pgTestcontainer.WithPassword("outpost"),
		pgTestcontainer.WithDatabase("default"),
	)
	if err != nil {
		panic(err)
	}

	endpoint, err := pgContainer.PortEndpoint(ctx, "5432/tcp", "")
	if err != nil {
		panic(err)
	}
	log.Printf("Postgres running at %s", endpoint)
	cfg.PostgresURL = endpoint
	cfg.cleanupFns = append(cfg.cleanupFns, func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate container: %s", err)
		}
	})
}
