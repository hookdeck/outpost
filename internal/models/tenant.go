package models

import (
	"fmt"
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
	if hash["created_at"] == "" {
		return fmt.Errorf("missing created_at")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, hash["created_at"])
	if err != nil {
		return err
	}
	t.CreatedAt = createdAt

	// Deserialize updated_at if present, otherwise fallback to created_at (for existing records)
	if hash["updated_at"] != "" {
		updatedAt, err := time.Parse(time.RFC3339Nano, hash["updated_at"])
		if err != nil {
			return err
		}
		t.UpdatedAt = updatedAt
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
