package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestIngester_Ingest(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)
	ingestor := ingest.New(logger, &ingest.IngestConfig{})
	cleanup, _ := ingestor.Init(context.Background())
	defer cleanup()

	t.Run("ingests without error", func(t *testing.T) {
		event := ingest.Event{
			ID:            "event-id",
			TenantID:      "tenant-id",
			DestinationID: "destination-id",
			Topic:         "topic",
			Time:          time.Now(),
			Metadata:      map[string]string{"key": "value"},
			Data:          map[string]interface{}{"key": "value"},
		}

		err := ingestor.Ingest(context.Background(), event)

		assert.Nil(t, err)
	})
}
