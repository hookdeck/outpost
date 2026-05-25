// Bench for ClickHouse client-side compression (issue #909).
//
// Alternates inserts with Compression off / LZ4 / ZSTD against the same CH
// instance and reports wall-clock, throughput, and exact wire bytes pulled
// from system.query_log via tagged query_ids.
//
// Env (same names as the app):
//
//	CLICKHOUSE_ADDR          required, host:port
//	CLICKHOUSE_USERNAME      default "default"
//	CLICKHOUSE_PASSWORD      default ""
//	CLICKHOUSE_DATABASE      default "default"
//	CLICKHOUSE_TLS_ENABLED   "true" to enable TLS
//
// Flags:
//
//	--batches    runs per mode (default 5)
//	--rows       rows per batch (default 10000)
//	--modes      comma-separated subset of off,lz4,zstd (default all)
//	--keep       keep the temp table on exit
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type mode struct {
	name   string
	method clickhouse.CompressionMethod
	on     bool
}

var allModes = []mode{
	{"off", 0, false},
	{"lz4", clickhouse.CompressionLZ4, true},
	{"zstd", clickhouse.CompressionZSTD, true},
}

type runResult struct {
	mode        string
	queryID     string
	wallMs      float64
	rows        int
	wireBytes   uint64 // NetworkReceiveBytes (server-side, post-compression on wire)
	uncompBytes uint64 // ReadCompressedBytes uncompressed equivalent (best-effort)
	serverMs    uint64
}

func main() {
	batches := flag.Int("batches", 5, "runs per mode")
	rows := flag.Int("rows", 10000, "rows per batch")
	modesFlag := flag.String("modes", "off,lz4,zstd", "comma-separated subset of off,lz4,zstd")
	keep := flag.Bool("keep", false, "keep bench table on exit")
	flag.Parse()

	addr := mustEnv("CLICKHOUSE_ADDR")
	user := envOr("CLICKHOUSE_USERNAME", "default")
	pass := os.Getenv("CLICKHOUSE_PASSWORD")
	db := envOr("CLICKHOUSE_DATABASE", "default")
	tlsOn := os.Getenv("CLICKHOUSE_TLS_ENABLED") == "true"

	selected := pickModes(*modesFlag)
	if len(selected) == 0 {
		log.Fatalf("no valid modes in %q", *modesFlag)
	}

	table := "bench_compression_" + randSuffix()
	fmt.Printf("CH      : %s db=%s tls=%v\n", addr, db, tlsOn)
	fmt.Printf("Table   : %s\n", table)
	fmt.Printf("Modes   : %s\n", joinModeNames(selected))
	fmt.Printf("Plan    : %d batches × %d rows per mode\n\n", *batches, *rows)

	// Admin conn (no compression) for DDL + system.query_log lookups.
	admin, err := openConn(addr, user, pass, db, tlsOn, mode{name: "admin"})
	if err != nil {
		log.Fatalf("admin conn: %v", err)
	}
	defer admin.Close()

	ctx := context.Background()

	if err := admin.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id         String,
			tenant_id  String,
			topic      String,
			event_time DateTime64(3),
			metadata   String,
			payload    String
		) ENGINE = MergeTree ORDER BY (tenant_id, event_time)
	`, table)); err != nil {
		log.Fatalf("create table: %v", err)
	}
	if !*keep {
		defer func() {
			if err := admin.Exec(context.Background(), "DROP TABLE IF EXISTS "+table); err != nil {
				log.Printf("warn: drop table: %v", err)
			}
		}()
	}

	// Warm-up: one throwaway insert per mode to prime connections / caches.
	for _, m := range selected {
		conn, err := openConn(addr, user, pass, db, tlsOn, m)
		if err != nil {
			log.Fatalf("warmup open %s: %v", m.name, err)
		}
		if _, _, err := doInsert(ctx, conn, table, 100); err != nil {
			log.Fatalf("warmup insert %s: %v", m.name, err)
		}
		conn.Close()
	}

	// Alternate modes to spread transient blips evenly.
	var results []runResult
	for b := 0; b < *batches; b++ {
		for _, m := range selected {
			conn, err := openConn(addr, user, pass, db, tlsOn, m)
			if err != nil {
				log.Fatalf("open %s: %v", m.name, err)
			}
			qid, wall, err := doInsert(ctx, conn, table, *rows)
			conn.Close()
			if err != nil {
				log.Fatalf("insert %s: %v", m.name, err)
			}
			fmt.Printf("  [%s] batch %d/%d  wall=%6.1fms  qid=%s\n",
				pad(m.name, 4), b+1, *batches, wall, qid)
			results = append(results, runResult{
				mode: m.name, queryID: qid, wallMs: wall, rows: *rows,
			})
		}
	}

	// Give CH a moment to flush system.query_log (async by default).
	fmt.Println("\nWaiting for system.query_log to flush...")
	if err := admin.Exec(ctx, "SYSTEM FLUSH LOGS"); err != nil {
		log.Printf("warn: flush logs: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Backfill wire bytes per query_id.
	for i := range results {
		r := &results[i]
		row := admin.QueryRow(ctx, `
			SELECT
				ProfileEvents['NetworkReceiveBytes'] AS bytes_in,
				ProfileEvents['ReadCompressedBytes'] AS read_compressed,
				query_duration_ms
			FROM system.query_log
			WHERE type = 'QueryFinish' AND query_id = ?
			ORDER BY event_time DESC LIMIT 1
		`, r.queryID)
		var bytesIn, readCompressed, durMs uint64
		if err := row.Scan(&bytesIn, &readCompressed, &durMs); err != nil {
			log.Printf("warn: query_log lookup %s: %v", r.queryID, err)
			continue
		}
		r.wireBytes = bytesIn
		r.uncompBytes = readCompressed
		r.serverMs = durMs
	}

	printSummary(results)
}

func doInsert(ctx context.Context, conn driver.Conn, table string, rows int) (string, float64, error) {
	qid := "bench_" + randSuffix()
	ctx = clickhouse.Context(ctx, clickhouse.WithQueryID(qid))

	batch, err := conn.PrepareBatch(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, tenant_id, topic, event_time, metadata, payload)", table))
	if err != nil {
		return qid, 0, fmt.Errorf("prepare: %w", err)
	}

	now := time.Now()
	for i := 0; i < rows; i++ {
		if err := batch.Append(
			fmt.Sprintf("evt_%d_%d", now.UnixNano(), i),
			fmt.Sprintf("tenant_%d", i%4),
			topicFor(i),
			now.Add(time.Duration(i)*time.Millisecond),
			metadataJSON(i),
			payloadJSON(i),
		); err != nil {
			return qid, 0, fmt.Errorf("append: %w", err)
		}
	}

	start := time.Now()
	if err := batch.Send(); err != nil {
		return qid, 0, fmt.Errorf("send: %w", err)
	}
	return qid, float64(time.Since(start).Microseconds()) / 1000.0, nil
}

func openConn(addr, user, pass, db string, tlsOn bool, m mode) (driver.Conn, error) {
	opts := &clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{Database: db, Username: user, Password: pass},
		// Force a single connection so driver-level stats / behavior is predictable.
		MaxOpenConns:    1,
		MaxIdleConns:    1,
		ConnMaxLifetime: time.Hour,
		DialTimeout:     10 * time.Second,
		ReadTimeout:     60 * time.Second,
	}
	if tlsOn {
		opts.TLS = &tls.Config{}
	}
	if m.on {
		opts.Compression = &clickhouse.Compression{Method: m.method}
	}
	return clickhouse.Open(opts)
}

// --- payload generators (realistic-ish JSON) ---

func topicFor(i int) string {
	switch i % 3 {
	case 0:
		return "order.created"
	case 1:
		return "order.updated"
	default:
		return "payment.received"
	}
}

func metadataJSON(i int) string {
	return fmt.Sprintf(
		`{"source":"bench","seq":%d,"region":"us-east-1","trace_id":"%s"}`,
		i, randSuffix())
}

func payloadJSON(i int) string {
	// ~700 bytes of repetitive JSON — compresses well, like real event bodies.
	return fmt.Sprintf(`{
"event_id":"evt_%d","type":"order.created","version":"v1",
"customer":{"id":"cus_%d","email":"user%d@example.com","name":"Customer %d","tier":"gold","since":"2024-01-15"},
"order":{"id":"ord_%d","total_cents":%d,"currency":"USD","items":[
  {"sku":"SKU-100%d","qty":1,"price_cents":1999,"name":"Widget"},
  {"sku":"SKU-200%d","qty":2,"price_cents":4999,"name":"Gadget"}
],"shipping":{"address1":"123 Main St","city":"Brooklyn","state":"NY","postal":"11201","country":"US"}},
"payment":{"method":"card","brand":"visa","last4":"4242","captured":true},
"metadata":{"campaign":"spring_2025","ref":"newsletter","experiment":"checkout_v3"}
}`, i, i, i, i, i, 1000+i*7, i, i)
}

// --- helpers ---

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("env %s required", k)
	}
	return v
}
func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func randSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
func pickModes(csv string) []mode {
	want := map[string]bool{}
	for _, p := range strings.Split(csv, ",") {
		want[strings.TrimSpace(p)] = true
	}
	var out []mode
	for _, m := range allModes {
		if want[m.name] {
			out = append(out, m)
		}
	}
	return out
}
func joinModeNames(ms []mode) string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.name
	}
	return strings.Join(names, ",")
}

func printSummary(results []runResult) {
	byMode := map[string][]runResult{}
	for _, r := range results {
		byMode[r.mode] = append(byMode[r.mode], r)
	}
	var modes []string
	for m := range byMode {
		modes = append(modes, m)
	}
	sort.Strings(modes)

	fmt.Println("\n=== Summary (median across batches) ===")
	fmt.Printf("%-6s  %10s  %10s  %12s  %12s  %12s\n",
		"mode", "wall_ms", "server_ms", "wire_bytes", "uncomp_b", "rows/s")
	fmt.Println(strings.Repeat("-", 76))

	var baseWall, baseWire float64
	for _, m := range modes {
		rs := byMode[m]
		wall := medianF(walls(rs))
		server := medianU(serverMs(rs))
		wire := medianU(wires(rs))
		uncomp := medianU(uncomps(rs))
		rowsPerSec := float64(rs[0].rows) / (wall / 1000.0)
		fmt.Printf("%-6s  %10.1f  %10d  %12d  %12d  %12.0f\n",
			m, wall, server, wire, uncomp, rowsPerSec)
		if m == "off" {
			baseWall, baseWire = wall, float64(wire)
		}
	}

	if baseWall > 0 {
		fmt.Println("\n=== vs. off ===")
		fmt.Printf("%-6s  %10s  %10s\n", "mode", "wall Δ%", "wire Δ%")
		fmt.Println(strings.Repeat("-", 32))
		for _, m := range modes {
			if m == "off" {
				continue
			}
			rs := byMode[m]
			wall := medianF(walls(rs))
			wire := float64(medianU(wires(rs)))
			wallDelta := (wall - baseWall) / baseWall * 100
			wireDelta := 0.0
			if baseWire > 0 {
				wireDelta = (wire - baseWire) / baseWire * 100
			}
			fmt.Printf("%-6s  %+10.1f  %+10.1f\n", m, wallDelta, wireDelta)
		}
	}
}

func walls(rs []runResult) []float64 {
	out := make([]float64, len(rs))
	for i, r := range rs {
		out[i] = r.wallMs
	}
	return out
}
func serverMs(rs []runResult) []uint64 {
	out := make([]uint64, len(rs))
	for i, r := range rs {
		out[i] = r.serverMs
	}
	return out
}
func wires(rs []runResult) []uint64 {
	out := make([]uint64, len(rs))
	for i, r := range rs {
		out[i] = r.wireBytes
	}
	return out
}
func uncomps(rs []runResult) []uint64 {
	out := make([]uint64, len(rs))
	for i, r := range rs {
		out[i] = r.uncompBytes
	}
	return out
}
func medianF(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}
func medianU(v []uint64) uint64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]uint64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}
