package destkafka

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKafkaPublisher_PartitionKeyTemplate_PanicIsFailedAttempt(t *testing.T) {
	t.Parallel()

	for _, template := range []string{`to_string(merge([{a: data}]))`, "contains(`[{}]`, `{}`)"} {
		// The template fails before the producer client is used, so none is set.
		publisher := &KafkaPublisher{
			BasePublisher:        destregistry.NewBasePublisher(),
			partitionKeyTemplate: template,
		}
		event := models.Event{ID: "evt_1", Topic: "t", Data: json.RawMessage(`{"a":"b"}`)}

		var delivery *destregistry.Delivery
		var err error
		require.NotPanics(t, func() {
			delivery, err = publisher.Publish(context.Background(), &event)
		})

		require.NotNil(t, delivery, "a failed attempt is recorded so the message is acked")
		assert.Equal(t, "failed", delivery.Status)
		assert.Equal(t, "ERR", delivery.Code)
		assert.Equal(t, "partition_key_template could not be evaluated: the expression failed on an unsupported value", delivery.Response["error"])

		var pubErr *destregistry.ErrDestinationPublishAttempt
		require.ErrorAs(t, err, &pubErr)
		assert.Equal(t, "kafka", pubErr.Provider)
		assert.Equal(t, "format_failed", pubErr.Data["error"])
	}
}
