package config

import (
	"log"
	"os"
	"strconv"
)

var (
	Port = 0

	RedisHost     = ""
	RedisPort     = ""
	RedisPassword = ""
	RedisDatabase = 0
)

func init() {
	Port = mustInt(os.Getenv("PORT"))

	RedisHost = os.Getenv("REDIS_HOST")
	RedisPort = os.Getenv("REDIS_PORT")
	RedisPassword = os.Getenv("REDIS_PASSWORD")
	RedisDatabase = mustInt(os.Getenv("REDIS_DATABASE"))
}

func mustInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("failed to parse %s as int", s)
	}
	return i
}
