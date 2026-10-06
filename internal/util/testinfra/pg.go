package testinfra

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	pgTestcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func NewPostgresConfig(t *testing.T) string {
	pgDB := &PGDB{}
	pgAddr := ensurePostgres(t)
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
	defer db.Close()
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
	defer db.Close()
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
		port, _ := strconv.Atoi(u.Port())
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

var postgresService = &service{name: "postgres", startHint: hintTest}

func ensurePostgres(t testing.TB) string {
	t.Helper()
	cfg := ReadConfig()
	// The postgres image runs a temporary server to initialise the cluster and
	// resets connections made to it, so connect rather than dial.
	return postgresService.ensure(t, cfg.PostgresURL,
		func() (string, error) { return startPGTestcontainer(cfg) },
		func(endpoint string) error {
			url := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s", "outpost", "outpost", endpoint, "default", "disable")
			db, err := pgxpool.New(context.Background(), url)
			if err != nil {
				return err
			}
			defer db.Close()
			return db.Ping(context.Background())
		})
}

func startPGTestcontainer(cfg *Config) (string, error) {
	ctx := context.Background()

	pgContainer, err := pgTestcontainer.Run(ctx,
		cfg.Images.Postgres,
		pgTestcontainer.WithUsername("outpost"),
		pgTestcontainer.WithPassword("outpost"),
		pgTestcontainer.WithDatabase("default"),
	)
	if err != nil {
		return "", err
	}

	endpoint, err := pgContainer.PortEndpoint(ctx, "5432/tcp", "")
	if err != nil {
		return "", err
	}
	log.Printf("Postgres running at %s", endpoint)
	return endpoint, nil
}
