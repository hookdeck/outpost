package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	outpost "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
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
	mu                sync.Mutex
	tenantsCreated    int
	destinationsCreated int
	errors           []string
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

	// Initialize faker
	gofakeit.Seed(time.Now().UnixNano())

	// Calculate estimated destinations
	avgDestinations := (*minDestinations + *maxDestinations) / 2
	estimatedTotal := *numTenants * avgDestinations

	// Display configuration
	fmt.Printf("=== Outpost Data Seeder Configuration ===\n")
	fmt.Printf("Server: %s\n", *serverURL)
	fmt.Printf("Tenants to create: %d\n", *numTenants)
	fmt.Printf("Destinations per tenant: %d-%d (avg: %d)\n", *minDestinations, *maxDestinations, avgDestinations)
	fmt.Printf("Estimated total destinations: ~%d\n", estimatedTotal)
	fmt.Printf("Concurrency: %d workers\n", *concurrency)
	fmt.Printf("\n")

	// Confirmation prompt
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

	// Create SDK client
	client := outpost.New(
		outpost.WithServerURL(*serverURL),
		outpost.WithSecurity(components.Security{
			AdminAPIKey: outpost.String(*apiKey),
		}),
	)

	ctx := context.Background()

	// Health check
	fmt.Printf("Checking server health...\n")
	healthResp, err := client.Health.Check(ctx)
	if err != nil {
		fmt.Printf("❌ Health check failed: %v\n", err)
		fmt.Printf("\nPlease ensure the Outpost server is running at %s\n", *serverURL)
		return
	}
	if healthResp.Res != nil {
		fmt.Printf("✅ Server is healthy: %s\n", *healthResp.Res)
	} else {
		fmt.Printf("✅ Server responded to health check\n")
	}
	fmt.Println()

	stats := &seedStats{}

	fmt.Printf("Starting seed process...\n")

	// Create worker pool
	tenantChan := make(chan int, *numTenants)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go worker(ctx, client, tenantChan, stats, &wg)
	}

	// Queue work
	for i := 0; i < *numTenants; i++ {
		tenantChan <- i
	}
	close(tenantChan)

	// Wait for completion
	wg.Wait()

	// Print summary
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

func worker(ctx context.Context, client *outpost.Outpost, tenantChan <-chan int, stats *seedStats, wg *sync.WaitGroup) {
	defer wg.Done()

	for range tenantChan {
		// Create tenant
		tenantID := generateTenantID()

		if *verbose {
			fmt.Printf("Creating tenant: %s\n", tenantID)
		}

		resp, err := client.Tenants.Upsert(ctx, &tenantID)
		if err != nil {
			stats.addError(fmt.Sprintf("Failed to create tenant %s: %v", tenantID, err))
			continue
		}

		if resp.Tenant == nil {
			stats.addError(fmt.Sprintf("No tenant returned for %s", tenantID))
			continue
		}

		stats.addTenant()

		// Create destinations for this tenant
		numDests := rand.Intn(*maxDestinations-*minDestinations+1) + *minDestinations
		for i := 0; i < numDests; i++ {
			if err := createDestination(ctx, client, tenantID, stats); err != nil {
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

func createDestination(ctx context.Context, client *outpost.Outpost, tenantID string, stats *seedStats) error {
	// Keep it simple - only create webhook destinations
	destCreate := components.DestinationCreate{
		DestinationCreateWebhook: &components.DestinationCreateWebhook{
			Type: components.DestinationCreateWebhookTypeWebhook,
			Topics: generateTopics(),
			Config: components.WebhookConfig{
				URL: fmt.Sprintf("https://mock.hookdeck.com/%s", gofakeit.UUID()),
			},
		},
	}

	_, err := client.Destinations.Create(ctx, destCreate, &tenantID)
	return err
}

func generateTenantID() string {
	// Generate various tenant ID formats using proper ID types
	formats := []func() string{
		func() string { return gofakeit.UUID() },
		func() string { return fmt.Sprintf("org_%s", gofakeit.UUID()) },
		func() string { return fmt.Sprintf("tenant_%s", gofakeit.UUID()) },
		func() string { return fmt.Sprintf("user_%s", gofakeit.UUID()) },
		func() string { return fmt.Sprintf("team_%s", gofakeit.UUID()) },
		func() string { return fmt.Sprintf("cus_%s", generateCUID()) },
		func() string { return generateCUID() },
	}
	return formats[rand.Intn(len(formats))]()
}

// generateCUID generates a CUID-like string (simplified version)
func generateCUID() string {
	// Generate a 25-character CUID-like ID
	chars := "0123456789abcdefghijklmnopqrstuvwxyz"
	result := make([]byte, 25)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

func generateTopics() components.Topics {
	// 30% chance of wildcard (all topics)
	if rand.Float32() < 0.3 {
		return components.CreateTopicsTopicsEnum(components.TopicsEnumWildcard)
	}

	// Use only the allowed topics for now
	allowedTopics := []string{
		"user.created",
		"user.updated",
		"user.deleted",
	}

	// Randomly select 1-3 topics from the allowed list
	numTopics := rand.Intn(len(allowedTopics)) + 1
	selectedTopics := make([]string, 0, numTopics)

	// Randomly select topics
	perm := rand.Perm(len(allowedTopics))
	for i := 0; i < numTopics; i++ {
		selectedTopics = append(selectedTopics, allowedTopics[perm[i]])
	}

	return components.CreateTopicsArrayOfStr(selectedTopics)
}