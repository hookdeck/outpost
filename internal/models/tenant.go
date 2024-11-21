package models

import (
	"fmt"
	"time"
)

type Tenant struct {
	ID                string    `json:"id" redis:"id"`
	DestinationsCount int       `json:"destinations_count" redis:"-"`
	Topics            []string  `json:"topics" redis:"-"`
	CreatedAt         time.Time `json:"created_at" redis:"created_at"`
}

func (t *Tenant) parseRedisHash(hash map[string]string) error {
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
	return nil
}
