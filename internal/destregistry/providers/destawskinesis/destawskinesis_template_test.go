package destawskinesis_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSKinesisPublisher_PartitionKeyTemplate_OrdinaryErrorFallsBackToEventID(t *testing.T) {
	t.Parallel()

	publisher := destawskinesis.NewAWSKinesisPublisher(nil, "my-stream", `join('/', [data.missing])`, false)
	event := models.Event{ID: "evt_1", Topic: "t", Data: json.RawMessage(`{}`)}

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "evt_1", *input.PartitionKey)
}
