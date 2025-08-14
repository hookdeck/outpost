package destawss3_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSS3Publisher_Format(t *testing.T) {
	fixedTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	event := models.Event{
		ID:    "event-123",
		Time:  fixedTime,
		Topic: "topic",
		Metadata: map[string]string{
			"meta_key": "meta_value",
		},
		Data: map[string]interface{}{"hello": "world"},
	}

	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		"events/",
		".json",
		"STANDARD",
		true,
		true,
	)

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)

	expectedKey := "events/" + fixedTime.Format(time.RFC3339Nano) + "_" + event.ID + ".json"
	assert.Equal(t, "my-bucket", *input.Bucket)
	assert.Equal(t, expectedKey, *input.Key)
	assert.Equal(t, types.StorageClassStandard, input.StorageClass)
	assert.Equal(t, "application/json", *input.ContentType)
	assert.Equal(t, map[string]string(event.Metadata), input.Metadata)

	// Verify checksum
	data, _ := json.Marshal(event.Data)
	hasher := sha256.Sum256(data)
	expectedChecksum := base64.StdEncoding.EncodeToString(hasher[:])
	assert.Equal(t, expectedChecksum, *input.ChecksumSHA256)
}

func TestAWSS3Publisher_Format_InvalidStorageClass(t *testing.T) {
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		"",
		"",
		"INVALID",
		true,
		true,
	)

	event := models.Event{ID: "id", Time: time.Now()}
	_, err := publisher.Format(context.Background(), &event)
	assert.Error(t, err)
}
