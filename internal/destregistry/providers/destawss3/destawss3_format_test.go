package destawss3_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSS3Publisher_Format_DefaultTemplate(t *testing.T) {
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

	// Use default template
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		`join('', [time.rfc3339_nano, '_', metadata."event-id", '.json'])`,
		"STANDARD",
	)

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)

	expectedKey := fixedTime.Format(time.RFC3339Nano) + "_" + event.ID + ".json"
	assert.Equal(t, "my-bucket", *input.Bucket)
	assert.Equal(t, expectedKey, *input.Key)
	assert.Equal(t, types.StorageClassStandard, input.StorageClass)
	assert.Equal(t, "application/json", *input.ContentType)

	// Verify metadata includes both event and system metadata
	assert.Equal(t, "meta_value", input.Metadata["meta_key"], "event metadata should be preserved")
	assert.Equal(t, event.ID, input.Metadata["event-id"], "event-id should be in metadata")
	assert.Equal(t, event.Topic, input.Metadata["topic"], "topic should be in metadata")
	assert.NotEmpty(t, input.Metadata["timestamp"], "timestamp should be in metadata")

	// Verify checksum
	data, _ := json.Marshal(event.Data)
	checksum := sha256.Sum256(data)
	expectedChecksum := base64.StdEncoding.EncodeToString(checksum[:])
	assert.Equal(t, expectedChecksum, *input.ChecksumSHA256)
}

func TestAWSS3Publisher_Format_DatePartitionTemplate(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
	event := models.Event{
		ID:    "event-123",
		Time:  fixedTime,
		Topic: "user.created",
		Data:  map[string]interface{}{"user_id": "user-456"},
	}

	// Use date partitioning template
	template := `join('/', ['year=', time.year, 'month=', time.month, 'day=', time.day, metadata."event-id", '.json'])`
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		template,
		"STANDARD",
	)

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)

	expectedKey := "year=/2024/month=/01/day=/15/event-123/.json"
	assert.Equal(t, expectedKey, *input.Key)
}

func TestAWSS3Publisher_Format_TopicBasedTemplate(t *testing.T) {
	event := models.Event{
		ID:    "event-123",
		Time:  time.Now(),
		Topic: "user.created",
		Data:  map[string]interface{}{"user_id": "user-456"},
	}

	// Use topic-based organization
	template := `join('/', [metadata.topic, time.date, metadata."event-id"])`
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		template,
		"STANDARD",
	)

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)

	expectedKey := "user.created/" + event.Time.UTC().Format("2006-01-02") + "/event-123"
	assert.Equal(t, expectedKey, *input.Key)
}

func TestAWSS3Publisher_Format_DataFieldTemplate(t *testing.T) {
	event := models.Event{
		ID:   "event-123",
		Time: time.Now(),
		Data: map[string]interface{}{
			"user_id": "user-456",
			"action":  "login",
		},
	}

	// Use data fields in template
	template := `join('/', ['users', data.user_id, 'actions', data.action, metadata."event-id"])`
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		template,
		"STANDARD",
	)

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)

	expectedKey := "users/user-456/actions/login/event-123"
	assert.Equal(t, expectedKey, *input.Key)
}

func TestAWSS3Publisher_Format_ComplexTemplate(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
	event := models.Event{
		ID:    "event-123",
		Time:  fixedTime,
		Topic: "order.placed",
		Metadata: map[string]string{
			"region": "us-west-2",
			"env":    "production",
		},
		Data: map[string]interface{}{
			"order_id":    "order-789",
			"customer_id": "cust-456",
			"amount":      99.99,
		},
	}

	// Complex template with multiple fields
	template := `join('/', [metadata.env, metadata.region, time.year, time.month, metadata.topic, data.customer_id, join('_', [data.order_id, time.unix])])`
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		template,
		"STANDARD",
	)

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)

	assert.Contains(t, *input.Key, "production/us-west-2/2024/01/order.placed/cust-456/order-789_")
}

func TestAWSS3Publisher_Format_InvalidTemplate(t *testing.T) {
	// This should panic since the template is invalid
	assert.Panics(t, func() {
		destawss3.NewAWSS3Publisher(
			nil,
			"my-bucket",
			"invalid[template",
			"STANDARD",
		)
	})
}

func TestAWSS3Publisher_Format_NilResult(t *testing.T) {
	event := models.Event{
		ID:   "event-123",
		Time: time.Now(),
		Data: map[string]interface{}{},
	}

	// Template that accesses non-existent field
	template := `data.nonexistent`
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		template,
		"STANDARD",
	)

	_, err := publisher.Format(context.Background(), &event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil result")
}

func TestAWSS3Publisher_Format_EmptyResult(t *testing.T) {
	event := models.Event{
		ID:   "event-123",
		Time: time.Now(),
		Data: map[string]interface{}{
			"empty": "",
		},
	}

	// Template that returns empty string
	template := `data.empty`
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		template,
		"STANDARD",
	)

	_, err := publisher.Format(context.Background(), &event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty string")
}

func TestAWSS3Publisher_Format_NumericResult(t *testing.T) {
	event := models.Event{
		ID:   "event-123",
		Time: time.Now(),
		Data: map[string]interface{}{
			"count": 42,
		},
	}

	// Template that returns a number
	template := `data.count`
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		template,
		"STANDARD",
	)

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "42", *input.Key)
}

func TestAWSS3Publisher_Format_BooleanResult(t *testing.T) {
	event := models.Event{
		ID:   "event-123",
		Time: time.Now(),
		Data: map[string]interface{}{
			"active": true,
		},
	}

	// Template that returns a boolean
	template := `data.active`
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		template,
		"STANDARD",
	)

	input, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "true", *input.Key)
}

func TestAWSS3Publisher_Format_TimeFields(t *testing.T) {
	fixedTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	event := models.Event{
		ID:   "event-123",
		Time: fixedTime,
		Data: map[string]interface{}{},
	}

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{"year", `time.year`, "2024"},
		{"month", `time.month`, "01"},
		{"day", `time.day`, "02"},
		{"hour", `time.hour`, "03"},
		{"minute", `time.minute`, "04"},
		{"second", `time.second`, "05"},
		{"date", `time.date`, "2024-01-02"},
		{"datetime", `time.datetime`, "2024-01-02T03:04:05"},
		{"unix", `time.unix`, fmt.Sprintf("%d", fixedTime.Unix())},
		{"rfc3339", `time.rfc3339`, fixedTime.Format(time.RFC3339)},
		{"rfc3339_nano", `time.rfc3339_nano`, fixedTime.Format(time.RFC3339Nano)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := destawss3.NewAWSS3Publisher(
				nil,
				"my-bucket",
				tt.template,
				"STANDARD",
			)

			input, err := publisher.Format(context.Background(), &event)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, *input.Key)
		})
	}
}

func TestAWSS3Publisher_Format_InvalidStorageClass(t *testing.T) {
	publisher := destawss3.NewAWSS3Publisher(
		nil,
		"my-bucket",
		`metadata."event-id"`,
		"INVALID",
	)

	event := models.Event{ID: "id", Time: time.Now()}
	_, err := publisher.Format(context.Background(), &event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "storage class")
}

// TestAWSS3Publisher_Format_LegacyPatterns demonstrates how to replicate the old
// configuration options (key_prefix, key_suffix, include_timestamp, include_event_id)
// using the new JMESPath template system.
//
// The new template system is much more flexible and allows:
// - Date-based partitioning: `join('/', ['year=', time.year, 'month=', time.month, 'day=', time.day, metadata."event-id"])`
// - Topic-based organization: `join('/', [metadata.topic, time.date, metadata."event-id"])`
// - Data-driven paths: `join('/', ['users', data.user_id, 'events', time.unix])`
// - Complex nested structures: `join('/', [metadata.env, metadata.region, time.year, metadata.topic, data.order_id])`
func TestAWSS3Publisher_Format_LegacyPatterns(t *testing.T) {
	fixedTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	event := models.Event{
		ID:    "event-123",
		Time:  fixedTime,
		Topic: "user.created",
		Data:  map[string]interface{}{"user_id": "user-456"},
	}

	tests := []struct {
		name        string
		template    string
		expected    string
		oldConfig   string
	}{
		{
			// OLD CONFIG:
			//   key_prefix: "events/"
			//   key_suffix: ".json"
			//   include_timestamp: false
			//   include_event_id: true
			name:      "prefix_and_suffix",
			template:  `join('', ['events/', metadata."event-id", '.json'])`,
			expected:  "events/event-123.json",
			oldConfig: "key_prefix='events/', key_suffix='.json', include_timestamp=false, include_event_id=true",
		},
		{
			// OLD CONFIG:
			//   key_prefix: ""
			//   key_suffix: ".json"
			//   include_timestamp: true
			//   include_event_id: true
			name:      "timestamp_and_event_id",
			template:  `join('', [time.rfc3339_nano, '_', metadata."event-id", '.json'])`,
			expected:  fixedTime.Format(time.RFC3339Nano) + "_event-123.json",
			oldConfig: "include_timestamp=true, include_event_id=true, key_suffix='.json'",
		},
		{
			// OLD CONFIG:
			//   key_prefix: ""
			//   key_suffix: ""
			//   include_timestamp: false
			//   include_event_id: true
			name:      "only_event_id",
			template:  `metadata."event-id"`,
			expected:  "event-123",
			oldConfig: "include_timestamp=false, include_event_id=true",
		},
		{
			// OLD CONFIG:
			//   key_prefix: ""
			//   key_suffix: ""
			//   include_timestamp: true
			//   include_event_id: false
			name:      "only_timestamp",
			template:  `time.rfc3339_nano`,
			expected:  fixedTime.Format(time.RFC3339Nano),
			oldConfig: "include_timestamp=true, include_event_id=false",
		},
		{
			// Complex example not directly mappable from old config
			name:      "complex_prefix_suffix",
			template:  `join('', ['logs/', time.year, '/', time.month, '/', metadata.topic, '/', time.day, '_', metadata."event-id", '.gz'])`,
			expected:  "logs/2024/01/user.created/02_event-123.gz",
			oldConfig: "Not possible with old config - demonstrates new flexibility",
		},
		{
			// Static key with date - old config couldn't do this elegantly
			name:      "no_timestamp_no_event_id",
			template:  `join('', ['static-key-', time.date, '.json'])`,
			expected:  "static-key-2024-01-02.json",
			oldConfig: "Not directly possible - would need static key_prefix and key_suffix only",
		},
		{
			// Modern S3 pattern with date partitioning - not possible with old config
			name:      "multiple_prefixes",
			template:  `join('/', ['data', 'raw', metadata.topic, time.date, metadata."event-id"])`,
			expected:  "data/raw/user.created/2024-01-02/event-123",
			oldConfig: "Not possible with old config - demonstrates date partitioning pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Old config: %s", tt.oldConfig)
			
			publisher := destawss3.NewAWSS3Publisher(
				nil,
				"my-bucket",
				tt.template,
				"STANDARD",
			)

			input, err := publisher.Format(context.Background(), &event)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, *input.Key)
		})
	}
}