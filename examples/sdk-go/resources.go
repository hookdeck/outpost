package main

import (
	"context"
	"fmt"
	"io" // For reading response body
	"log"
	"time"

	"github.com/google/uuid"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

func manageOutpostResources(adminAPIKey string, apiServerURL string) {
	// 1. Create an Outpost instance using the AdminAPIKey (apiServerURL is API_BASE_URL or SERVER_URL + /api/v1)
	outpostAdmin := outpostgo.New(
		outpostgo.WithSecurity(adminAPIKey),
		outpostgo.WithServerURL(apiServerURL),
	)

	tenantID := "hookdeck"
	topic := "user.created"
	newDestinationName := fmt.Sprintf("My Test Destination %s", uuid.New().String())

	ctx := context.Background()

	// 2. Create a tenant
	log.Printf("Creating tenant: %s\n", tenantID)
	tenantRes, err := outpostAdmin.Tenants.Upsert(ctx, tenantID, nil)
	if err != nil {
		log.Fatalf("Failed to create tenant: %v\n", err)
	}
	if tenantRes != nil && tenantRes.Tenant != nil {
		log.Printf("Tenant created successfully: %+v\n", tenantRes.Tenant)
	} else {
		log.Println("Tenant creation returned no data or an unexpected response structure.")
	}

	// 3. Create a destination for the tenant
	log.Printf("Creating destination: %s for tenant %s...\n", newDestinationName, tenantID)
	destinationBody := components.DestinationCreate{
		Type: components.DestinationCreateTypeWebhook,
		DestinationCreateWebhook: &components.DestinationCreateWebhook{
			Type: components.DestinationCreateWebhookTypeWebhook,
			Topics: components.Topics{
				ArrayOfStr: []string{topic},
			},
			Config: components.WebhookConfig{
				URL: "https://example.com/webhook",
			},
		},
	}

	destRes, err := outpostAdmin.Destinations.Create(
		ctx,
		tenantID,
		destinationBody,
	)
	if err != nil {
		log.Fatalf("Failed to create destination: %v\n", err)
	}
	if destRes != nil && destRes.Destination != nil {
		log.Printf("Destination created successfully: %+v\n", destRes.Destination)
	} else {
		log.Println("Destination creation returned no data or an unexpected response structure.")
	}

	// 4. Publish an event for the created tenant
	eventPayload := map[string]interface{}{
		"userId":    "user_456",
		"orderId":   "order_xyz",
		"timestamp": time.Now().Format(time.RFC3339),
	}

	log.Printf("Publishing event to topic %s for tenant %s...\n", topic, tenantID)
	// Publishing event using components.PublishRequest.
	publishRequest := components.PublishRequest{
		TenantID:         outpostgo.String(tenantID),
		Topic:            outpostgo.String(topic),
		Data:             eventPayload,
		EligibleForRetry: outpostgo.Bool(true),
	}
	publishRes, err := outpostAdmin.Publish.Event(ctx, publishRequest)
	if err != nil {
		log.Fatalf("Failed to publish event: %v\n", err)
	}

	log.Println("Event published successfully")
	// TODO: result should have an ID property
	if publishRes != nil && publishRes.HTTPMeta.Response != nil && publishRes.HTTPMeta.Response.Body != nil {
		bodyBytes, err := io.ReadAll(publishRes.HTTPMeta.Response.Body)
		if err != nil {
			log.Printf("Error reading publish response body: %v", err)
		} else {
			log.Printf("Publish response body: %s", string(bodyBytes))
		}
		_ = publishRes.HTTPMeta.Response.Body.Close()
	} else if publishRes != nil {
		log.Printf("Publish response (raw SDK response struct): %+v", publishRes)
	}

	log.Println("--- Example finished ---")
}
