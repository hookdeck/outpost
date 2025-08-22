package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/go-redis/redis"
)

func main() {
	if len(os.Args) < 5 {
		fmt.Println("Redis Debug Tool - Test Redis connectivity and RSMQ operations")
		fmt.Println()
		fmt.Println("Usage: redis-debug <host> <port> <password> <database> [tls] [cluster]")
		fmt.Println()
		fmt.Println("Arguments:")
		fmt.Println("  host      Redis server hostname")
		fmt.Println("  port      Redis server port")
		fmt.Println("  password  Redis authentication password")
		fmt.Println("  database  Redis database number (ignored for cluster mode)")
		fmt.Println("  tls       Optional: 'true' to enable TLS")
		fmt.Println("  cluster   Optional: 'cluster' to use cluster client")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  # Regular Redis with TLS:")
		fmt.Println("  redis-debug myredis.cache.windows.net 6380 mypassword 0 true")
		fmt.Println()
		fmt.Println("  # Azure Managed Redis (cluster mode):")
		fmt.Println("  redis-debug myredis.rdb.cache.windows.net 10000 mypassword 0 true cluster")
		os.Exit(1)
	}

	host := os.Args[1]
	portStr := os.Args[2]
	password := os.Args[3]
	dbStr := os.Args[4]
	
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatal("Invalid port:", err)
	}
	
	db, err := strconv.Atoi(dbStr)
	if err != nil {
		log.Fatal("Invalid database:", err)
	}
	
	useTLS := len(os.Args) > 5 && os.Args[5] == "true"
	useCluster := len(os.Args) > 6 && os.Args[6] == "cluster"

	fmt.Printf("=== Redis Client Mode: %s ===\n", 
		map[bool]string{true: "CLUSTER", false: "REGULAR"}[useCluster])
	
	if useCluster {
		testClusterClient(host, port, password, useTLS)
	} else {
		testRegularClient(host, port, password, db, useTLS)
	}
}

func testClusterClient(host string, port int, password string, useTLS bool) {
	options := &redis.ClusterOptions{
		Addrs:    []string{fmt.Sprintf("%s:%d", host, port)},
		Password: password,
	}
	
	if useTLS {
		options.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
		}
	}

	client := redis.NewClusterClient(options)
	defer client.Close()

	fmt.Println("\n=== Redis Cluster Connectivity Test ===")
	
	// Test basic connectivity
	pong, err := client.Ping().Result()
	if err != nil {
		log.Fatal("Redis cluster PING failed:", err)
	}
	fmt.Printf("✅ PING: %s\n", pong)

	// Test TIME command
	timeResult, err := client.Time().Result()
	if err != nil {
		log.Fatal("Redis cluster TIME failed:", err)
	}
	fmt.Printf("✅ TIME: %v\n", timeResult)

	testRedisOperations(client)
}

func testRegularClient(host string, port int, password string, db int, useTLS bool) {
	options := &redis.Options{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Password: password,
		DB:       db,
	}
	
	if useTLS {
		options.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
		}
	}

	client := redis.NewClient(options)
	defer client.Close()

	fmt.Println("\n=== Redis Regular Connectivity Test ===")
	
	// Test basic connectivity
	pong, err := client.Ping().Result()
	if err != nil {
		log.Fatal("Redis PING failed:", err)
	}
	fmt.Printf("✅ PING: %s\n", pong)

	// Test TIME command
	timeResult, err := client.Time().Result()
	if err != nil {
		log.Fatal("Redis TIME failed:", err)
	}
	fmt.Printf("✅ TIME: %v\n", timeResult)

	testRedisOperations(client)
}

// Interface that both redis.Client and redis.ClusterClient implement
type RedisClientInterface interface {
	Exists(keys ...string) *redis.IntCmd
	Type(key string) *redis.StatusCmd
	HGetAll(key string) *redis.StringStringMapCmd
	HSetNX(key, field string, value interface{}) *redis.BoolCmd
	Del(keys ...string) *redis.IntCmd
	TxPipeline() redis.Pipeliner
}

func testRedisOperations(client RedisClientInterface) {
	// Check specific RSMQ keys
	rsmqKeys := []string{
		"rsmq:deliverymq-retry:Q",
		"rsmq:deliverymq-retry",
		"rsmq:QUEUES",
	}

	fmt.Println("\n=== RSMQ Key Analysis ===")
	for _, key := range rsmqKeys {
		exists, err := client.Exists(key).Result()
		if err != nil {
			fmt.Printf("❌ %s: Error checking existence: %v\n", key, err)
			continue
		}
		
		if exists == 0 {
			fmt.Printf("ℹ️  %s: Does not exist\n", key)
			continue
		}
		
		keyType, err := client.Type(key).Result()
		if err != nil {
			fmt.Printf("❌ %s: Error getting type: %v\n", key, err)
			continue
		}
		
		fmt.Printf("⚠️  %s: EXISTS (type: %s)\n", key, keyType)
		
		// If it's a hash, show the fields
		if keyType == "hash" {
			fields, err := client.HGetAll(key).Result()
			if err != nil {
				fmt.Printf("   Error getting hash fields: %v\n", err)
			} else {
				fmt.Printf("   Hash fields: %+v\n", fields)
			}
		}
	}

	fmt.Println("\n=== HSETNX Test ===")
	testKey := "rsmq:test-queue:Q"
	
	// Clean up any existing test key
	client.Del(testKey)
	
	// Test HSETNX command
	result, err := client.HSetNX(testKey, "vt", 30).Result()
	if err != nil {
		fmt.Printf("❌ HSETNX failed: %v\n", err)
	} else {
		fmt.Printf("✅ HSETNX result: %v\n", result)
	}
	
	// Clean up
	client.Del(testKey)

	fmt.Println("\n=== Transaction Test ===")
	tx := client.TxPipeline()
	tx.HSetNX("rsmq:test-tx:Q", "vt", 30)
	tx.HSetNX("rsmq:test-tx:Q", "delay", 0)
	tx.HSetNX("rsmq:test-tx:Q", "maxsize", 65536)
	
	results, err := tx.Exec()
	if err != nil {
		fmt.Printf("❌ Transaction failed: %v\n", err)
		fmt.Printf("   Results: %+v\n", results)
	} else {
		fmt.Printf("✅ Transaction succeeded with %d results\n", len(results))
	}
	
	// Clean up
	client.Del("rsmq:test-tx:Q")
	
	fmt.Println("\n=== Cleanup Existing RSMQ Keys ===")
	for _, key := range rsmqKeys {
		deleted, err := client.Del(key).Result()
		if err != nil {
			fmt.Printf("❌ Error deleting %s: %v\n", key, err)
		} else if deleted > 0 {
			fmt.Printf("🗑️  Deleted %s\n", key)
		}
	}
	
	fmt.Println("\n✅ Redis diagnostic complete!")
}