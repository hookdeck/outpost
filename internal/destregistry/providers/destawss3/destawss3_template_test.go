package destawss3_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSS3Publisher_Publish_TemplateEvaluationErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "ordinary evaluation error",
			template: `join('/', ['prefix', metadata.operationId])`,
			want:     "could not format event for delivery",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			publisher := destawss3.NewAWSS3Publisher(destregistry.NewBasePublisher(), nil, "my-bucket", tt.template, "STANDARD")
			event := models.Event{ID: "evt_1", Time: time.Now(), Data: json.RawMessage(`{}`)}

			var delivery *destregistry.Delivery
			var err error
			require.NotPanics(t, func() {
				delivery, err = publisher.Publish(context.Background(), &event)
			})
			require.NotNil(t, delivery)
			assert.Equal(t, "failed", delivery.Status)
			require.Error(t, err)
			assert.Equal(t, tt.want, delivery.Response["error"])
		})
	}
}
