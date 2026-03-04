package main

import (
	"context"
	"fmt"
	"log"
	"os"

	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"github.com/joho/godotenv"
)

func withJwt(ctx context.Context, jwt string, serverURL string, tenantID string) {
	log.Println("--- Running with Tenant JWT ---")

	apiServerURL := fmt.Sprintf("%s/api/v1", serverURL)

	jwtClient := outpostgo.New(
		outpostgo.WithSecurity(components.Security{
			APIKey: outpostgo.String(jwt),
		}),
		outpostgo.WithServerURL(apiServerURL),
	)

	destRes, err := jwtClient.Destinations.List(ctx, outpostgo.String(tenantID), nil, nil)
	if err != nil {
		log.Fatalf("Failed to list destinations with JWT: %v", err)
	}

	if destRes != nil && destRes.Destinations != nil {
		log.Printf("Successfully listed %d destinations using JWT.", len(destRes.Destinations))
	} else {
		log.Println("List destinations with JWT returned no data or an unexpected response structure.")
	}
}

func withAdminApiKey(ctx context.Context, serverURL string, adminAPIKey string, tenantID string) {
	log.Println("--- Running with Admin API Key ---")

	apiServerURL := fmt.Sprintf("%s/api/v1", serverURL)

	adminClient := outpostgo.New(
		outpostgo.WithSecurity(components.Security{
			APIKey: outpostgo.String(adminAPIKey),
		}),
		outpostgo.WithServerURL(apiServerURL),
	)

	healthRes, err := adminClient.Health.Check(ctx)
	if err != nil {
		log.Fatalf("Health check failed: %v", err)
	}

	if healthRes != nil && healthRes.Res != nil {
		log.Printf("Health check successful. Details: %s", *healthRes.Res)
	} else {
		log.Println("Health check returned no data or an unexpected response structure.")
	}

	destRes, err := adminClient.Destinations.List(ctx, outpostgo.String(tenantID), nil, nil)
	if err != nil {
		log.Fatalf("Failed to list destinations with Admin Key: %v", err)
	}

	if destRes != nil && destRes.Destinations != nil {
		log.Printf("Successfully listed %d destinations using Admin Key for tenant %s.", len(destRes.Destinations), tenantID)
	} else {
		log.Println("List destinations with Admin Key returned no data or an unexpected response structure.")
	}

	tokenRes, err := adminClient.Tenants.GetToken(ctx, outpostgo.String(tenantID))
	if err != nil {
		log.Fatalf("Failed to get tenant token: %v", err)
	}

	if tokenRes != nil && tokenRes.TenantToken != nil && tokenRes.TenantToken.Token != nil {
		log.Printf("Successfully obtained tenant JWT for tenant %s.", tenantID)
		withJwt(ctx, *tokenRes.TenantToken.Token, serverURL, tenantID)
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

	serverURL := os.Getenv("SERVER_URL")
	adminAPIKey := os.Getenv("ADMIN_API_KEY")
	tenantID := os.Getenv("TENANT_ID")

	if serverURL == "" {
		log.Fatal("SERVER_URL environment variable not set")
	}
	if adminAPIKey == "" {
		log.Fatal("ADMIN_API_KEY environment variable not set")
	}
	if tenantID == "" {
		log.Fatal("TENANT_ID environment variable not set")
	}

	ctx := context.Background()
	withAdminApiKey(ctx, serverURL, adminAPIKey, tenantID)

	log.Println("--- Auth example finished ---")
}
