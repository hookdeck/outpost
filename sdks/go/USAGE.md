<!-- Start SDK Example Usage [usage] -->
```go
package main

import (
	"context"
	"github.com/hookdeck/outpost/sdks/go/client"
	"log"
)

func main() {
	ctx := context.Background()

	s := client.New()

	res, err := s.Health.Check(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.Res != nil {
		// handle response
	}
}

```
<!-- End SDK Example Usage [usage] -->