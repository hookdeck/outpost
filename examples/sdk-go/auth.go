package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/joho/godotenv"
)

func withTenantApiKey(ctx context.Context, tenantApiKey string, apiServerURL string, tenantID string) {
	log.Println("--- Running with tenant-scoped API key ---")
	// 0.13.1: use the tenant-scoped API key (from tenants.GetToken). List destinations only returns destinations for that tenant.
	client := outpostgo.New(
		outpostgo.WithSecurity(tenantApiKey),
		outpostgo.WithServerURL(apiServerURL),
	)

	destRes, err := client.Destinations.List(ctx, tenantID, nil, nil)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "401") || strings.Contains(errStr, "Unauthorized") {
			log.Printf("List destinations with tenant token returned 401. The server could not verify the JWT — ensure API_JWT_SECRET is set on the Outpost deployment (see sdks/schemas/README.md). Error: %v", err)
		}
		log.Fatalf("Failed to list destinations with tenant API key: %v", err)
	}

	if destRes != nil && destRes.Destinations != nil {
		log.Printf("Successfully listed %d destinations using tenant API key.", len(destRes.Destinations))
	} else {
		log.Println("List destinations with tenant API key returned no data or an unexpected response structure.")
	}
}

func withAdminApiKey(ctx context.Context, apiServerURL string, adminAPIKey string, tenantID string) {
	log.Println("--- Running with Admin API Key ---")

	adminClient := outpostgo.New(
		outpostgo.WithSecurity(adminAPIKey),
		outpostgo.WithServerURL(apiServerURL),
	)

	healthRes, err := adminClient.Health.Check(ctx)
	if err != nil {
		// Health endpoint not available on managed Outpost (404); continue with rest of example
		if strings.Contains(err.Error(), "404") {
			log.Println("Health endpoint not available (e.g. managed Outpost). Skipping.")
		} else {
			log.Fatalf("Health check failed: %v", err)
		}
	} else if healthRes != nil && healthRes.Object != nil {
		log.Printf("Health check successful. Details: %+v", healthRes.Object)
	} else {
		log.Println("Health check returned no data or an unexpected response structure.")
	}

	destRes, err := adminClient.Destinations.List(ctx, tenantID, nil, nil)
	if err != nil {
		log.Fatalf("Failed to list destinations with Admin Key: %v", err)
	}

	if destRes != nil && destRes.Destinations != nil {
		log.Printf("Successfully listed %d destinations using Admin Key for tenant %s.", len(destRes.Destinations), tenantID)
	} else {
		log.Println("List destinations with Admin Key returned no data or an unexpected response structure.")
	}

	tokenRes, err := adminClient.Tenants.GetToken(ctx, tenantID)
	if err != nil {
		log.Fatalf("Failed to get tenant token: %v", err)
	}

	if tokenRes != nil && tokenRes.TenantToken != nil && tokenRes.TenantToken.Token != nil {
		log.Printf("Successfully obtained tenant-scoped API key for tenant %s.", tenantID)
		withTenantApiKey(ctx, *tokenRes.TenantToken.Token, apiServerURL, tenantID)
	} else {
		log.Println("Get tenant token returned no data or an unexpected response structure.")
	}
}

// Renamed main to runAuthExample to avoid conflict
func runAuthExample() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, proceeding without it")
	}

	adminAPIKey := os.Getenv("ADMIN_API_KEY")
	tenantID := os.Getenv("TENANT_ID")

	if adminAPIKey == "" {
		log.Fatal("ADMIN_API_KEY environment variable not set")
	}
	if tenantID == "" {
		log.Fatal("TENANT_ID environment variable not set")
	}

	// Use API_BASE_URL when set (e.g. live Outpost), else SERVER_URL + /api/v1
	apiServerURL := os.Getenv("API_BASE_URL")
	if apiServerURL == "" {
		serverURL := os.Getenv("SERVER_URL")
		if serverURL == "" {
			serverURL = "http://localhost:3333"
		}
		apiServerURL = fmt.Sprintf("%s/api/v1", serverURL)
	}

	ctx := context.Background()
	withAdminApiKey(ctx, apiServerURL, adminAPIKey, tenantID)

	log.Println("--- Auth example finished ---")
}
