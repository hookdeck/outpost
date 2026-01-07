package models

import (
	"fmt"
	"strconv"
	"time"
)

type Tenant struct {
	ID                string    `json:"id" redis:"id"`
	DestinationsCount int       `json:"destinations_count" redis:"-"`
	Topics            []string  `json:"topics" redis:"-"`
	Metadata          Metadata  `json:"metadata,omitempty" redis:"-"`
	CreatedAt         time.Time `json:"created_at" redis:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" redis:"updated_at"`
}

func (t *Tenant) parseRedisHash(hash map[string]string) error {
	if _, ok := hash["deleted_at"]; ok {
		return ErrTenantDeleted
	}
	if hash["id"] == "" {
		return fmt.Errorf("missing id")
	}
	t.ID = hash["id"]

	// Parse created_at - supports both numeric (Unix timestamp) and RFC3339 formats
	// This enables lazy migration: old records have RFC3339, new records have Unix timestamps
	var err error
	t.CreatedAt, err = parseTimestamp(hash["created_at"])
	if err != nil {
		return fmt.Errorf("invalid created_at: %w", err)
	}

	// Parse updated_at - same lazy migration support
	if hash["updated_at"] != "" {
		t.UpdatedAt, err = parseTimestamp(hash["updated_at"])
		if err != nil {
			// Fallback to created_at if updated_at is invalid
			t.UpdatedAt = t.CreatedAt
		}
	} else {
		t.UpdatedAt = t.CreatedAt
	}

	// Deserialize metadata if present
	if metadataStr, exists := hash["metadata"]; exists && metadataStr != "" {
		err = t.Metadata.UnmarshalBinary([]byte(metadataStr))
		if err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
	}

	return nil
}

// parseTimestamp parses a timestamp from either numeric (Unix) or RFC3339 format.
// This supports lazy migration from RFC3339 strings to Unix timestamps.
func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("missing timestamp")
	}

	// Try to parse as Unix timestamp (numeric) first - new format
	if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(ts, 0).UTC(), nil
	}

	// Fallback to RFC3339Nano (old format)
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}

	// Fallback to RFC3339
	return time.Parse(time.RFC3339, value)
}
