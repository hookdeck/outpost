<!-- Start SDK Example Usage [usage] -->
```go
package main

import (
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New(
		outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
	)

	res, err := s.Publish(ctx, components.PublishRequest{
		ID:               outpostgo.Pointer("evt_abc123xyz789"),
		TenantID:         outpostgo.Pointer("tenant_123"),
		Topic:            outpostgo.Pointer("user.created"),
		EligibleForRetry: outpostgo.Pointer(true),
		Metadata: map[string]string{
			"source": "crm",
		},
		Data: map[string]any{
			"user_id": "userid",
			"status":  "active",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.PublishResponse != nil {
		// handle response
	}
}

```
<!-- End SDK Example Usage [usage] -->