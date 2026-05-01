package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

func runPublishEventExample() {
	adminAPIKey := os.Getenv("ADMIN_API_KEY")
	if adminAPIKey == "" {
		log.Fatal("Please set the ADMIN_API_KEY environment variable.")
	}

	tenantID := os.Getenv("TENANT_ID")
	if tenantID == "" {
		tenantID = "hookdeck"
	}

	apiServerURL := os.Getenv("API_BASE_URL")
	if apiServerURL == "" {
		serverURL := os.Getenv("SERVER_URL")
		if serverURL == "" {
			serverURL = "http://localhost:3333"
		}
		apiServerURL = fmt.Sprintf("%s/api/v1", serverURL)
	}

	client := outpostgo.New(
		outpostgo.WithSecurity(adminAPIKey),
		outpostgo.WithServerURL(apiServerURL),
	)

	topic := "order.created"
	payload := map[string]interface{}{
		"order_id":     "ord_2Ua9d1o2b3c4d5e6f7g8h9i0j",
		"customer_id":  "cus_1a2b3c4d5e6f7g8h9i0j",
		"total_amount": "99.99",
		"currency":     "USD",
		"items": []map[string]interface{}{
			{
				"product_id": "prod_1a2b3c4d5e6f7g8h9i0j",
				"name":       "Example Product 1",
				"quantity":   1,
				"price":      "49.99",
			},
			{
				"product_id": "prod_9z8y7x6w5v4u3t2s1r0q",
				"name":       "Example Product 2",
				"quantity":   1,
				"price":      "50.00",
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Failed to marshal payload: %v", err)
	}

	var data map[string]interface{}
	err = json.Unmarshal(payloadBytes, &data)
	if err != nil {
		log.Fatalf("Failed to unmarshal payload into data: %v", err)
	}

	request := components.PublishRequest{
		Topic:    &topic,
		Data:     data,
		TenantID: &tenantID,
	}

	res, err := client.Publish(context.Background(), request)
	if err != nil {
		log.Fatalf("Error publishing event: %v", err)
	}

	if res.HTTPMeta.Response.StatusCode == 202 {
		fmt.Println("Event published successfully")
		if res.PublishResponse != nil {
			fmt.Printf("Event ID: %s\n", res.PublishResponse.GetID())
		}
	} else {
		fmt.Printf("Failed to publish event. Status code: %d\n", res.HTTPMeta.Response.StatusCode)
	}
}
