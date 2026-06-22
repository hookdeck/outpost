package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/brianvoe/gofakeit/v6"
)

var (
	serverURL       = flag.String("server", "http://localhost:3333/api/v1", "Outpost server URL")
	apiKey          = flag.String("apikey", "apikey", "API key for authentication")
	numTenants      = flag.Int("tenants", 100, "Number of tenants to create")
	minDestinations = flag.Int("min-destinations", 1, "Minimum destinations per tenant")
	maxDestinations = flag.Int("max-destinations", 10, "Maximum destinations per tenant")
	concurrency     = flag.Int("concurrency", 10, "Number of concurrent workers")
	verbose         = flag.Bool("verbose", false, "Enable verbose output")
	skipConfirm     = flag.Bool("yes", false, "Skip confirmation prompt")
	help            = flag.Bool("help", false, "Show help message")
)

type seedStats struct {
	mu                  sync.Mutex
	tenantsCreated      int
	destinationsCreated int
	errors              []string
}

func (s *seedStats) addTenant() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenantsCreated++
}

func (s *seedStats) addDestination() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destinationsCreated++
}

func (s *seedStats) addError(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, err)
}

type client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func (c *client) do(ctx context.Context, method, path string, body any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Outpost Data Seeder - Generate test data for Outpost\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %s [options]\n\n", "seed")
		fmt.Fprintf(flag.CommandLine.Output(), "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\nExamples:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  # Create 100 tenants with default settings\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  seed\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  # Create 500 tenants with 5-20 destinations each\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  seed -tenants=500 -min-destinations=5 -max-destinations=20\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  # Skip confirmation and run with verbose output\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  seed -yes -verbose\n")
	}

	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	gofakeit.Seed(time.Now().UnixNano())

	avgDestinations := (*minDestinations + *maxDestinations) / 2
	estimatedTotal := *numTenants * avgDestinations

	fmt.Printf("=== Outpost Data Seeder Configuration ===\n")
	fmt.Printf("Server: %s\n", *serverURL)
	fmt.Printf("Tenants to create: %d\n", *numTenants)
	fmt.Printf("Destinations per tenant: %d-%d (avg: %d)\n", *minDestinations, *maxDestinations, avgDestinations)
	fmt.Printf("Estimated total destinations: ~%d\n", estimatedTotal)
	fmt.Printf("Concurrency: %d workers\n", *concurrency)
	fmt.Printf("\n")

	if !*skipConfirm {
		fmt.Printf("This will create approximately %d tenants and %d destinations.\n", *numTenants, estimatedTotal)
		fmt.Printf("Continue? (y/N): ")

		var response string
		fmt.Scanln(&response)

		if response != "y" && response != "Y" && response != "yes" && response != "Yes" {
			fmt.Println("Operation cancelled.")
			return
		}
		fmt.Println()
	}

	c := &client{
		baseURL: strings.TrimRight(*serverURL, "/"),
		apiKey:  *apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}

	ctx := context.Background()

	fmt.Printf("Checking server health...\n")
	healthURL := strings.TrimSuffix(*serverURL, "/api/v1") + "/healthz"
	healthResp, err := http.Get(healthURL)
	if err != nil {
		fmt.Printf("❌ Health check failed: %v\n", err)
		fmt.Printf("\nPlease ensure the Outpost server is running at %s\n", *serverURL)
		return
	}
	defer healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		fmt.Printf("❌ Health check failed: status %d\n", healthResp.StatusCode)
		return
	}
	fmt.Printf("✅ Server is healthy\n")
	fmt.Println()

	stats := &seedStats{}

	fmt.Printf("Starting seed process...\n")

	tenantChan := make(chan int, *numTenants)
	var wg sync.WaitGroup

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go worker(ctx, c, tenantChan, stats, &wg)
	}

	for i := 0; i < *numTenants; i++ {
		tenantChan <- i
	}
	close(tenantChan)

	wg.Wait()

	fmt.Printf("\n=== Seeding Complete ===\n")
	fmt.Printf("Tenants created: %d\n", stats.tenantsCreated)
	fmt.Printf("Destinations created: %d\n", stats.destinationsCreated)
	if len(stats.errors) > 0 {
		fmt.Printf("Errors encountered: %d\n", len(stats.errors))
		if *verbose {
			fmt.Println("\nErrors:")
			for _, err := range stats.errors {
				fmt.Printf("  - %s\n", err)
			}
		}
	}
}

func worker(ctx context.Context, c *client, tenantChan <-chan int, stats *seedStats, wg *sync.WaitGroup) {
	defer wg.Done()

	for i := range tenantChan {
		tenantID := fmt.Sprintf("tenant_%d", i+1)

		if *verbose {
			fmt.Printf("Creating tenant: %s\n", tenantID)
		}

		if err := c.do(ctx, http.MethodPut, "/tenants/"+tenantID, nil); err != nil {
			stats.addError(fmt.Sprintf("Failed to create tenant %s: %v", tenantID, err))
			continue
		}

		stats.addTenant()

		numDests := rand.Intn(*maxDestinations-*minDestinations+1) + *minDestinations
		for j := 0; j < numDests; j++ {
			if err := createDestination(ctx, c, tenantID); err != nil {
				stats.addError(fmt.Sprintf("Failed to create destination for tenant %s: %v", tenantID, err))
			} else {
				stats.addDestination()
			}
		}

		if *verbose {
			fmt.Printf("  Created %d destinations for tenant %s\n", numDests, tenantID)
		}
	}
}

func createDestination(ctx context.Context, c *client, tenantID string) error {
	body := map[string]any{
		"type":   "webhook",
		"topics": generateTopics(),
		"config": map[string]string{
			"url": fmt.Sprintf("https://mock.hookdeck.com/%s", gofakeit.UUID()),
		},
	}
	return c.do(ctx, http.MethodPost, "/tenants/"+tenantID+"/destinations", body)
}

func generateTopics() any {
	if rand.Float32() < 0.3 {
		return "*"
	}

	allowedTopics := []string{
		"user.created",
		"user.updated",
		"user.deleted",
	}

	numTopics := rand.Intn(len(allowedTopics)) + 1
	perm := rand.Perm(len(allowedTopics))
	selected := make([]string, 0, numTopics)
	for i := 0; i < numTopics; i++ {
		selected = append(selected, allowedTopics[perm[i]])
	}
	return selected
}
