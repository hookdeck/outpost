package destinationmockserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
)

type Event struct {
	Success  bool                   `json:"success"`
	Verified bool                   `json:"verified"`
	Payload  map[string]interface{} `json:"payload"`
}

type EntityStore interface {
	ListDestination(ctx context.Context) ([]models.Destination, error)
	RetrieveDestination(ctx context.Context, id string) (*models.Destination, error)
	UpsertDestination(ctx context.Context, destination models.Destination) error
	DeleteDestination(ctx context.Context, id string) error

	ReceiveEvent(ctx context.Context, destinationID string, payload map[string]interface{}, metadata map[string]string) (*Event, error)
	ListEvent(ctx context.Context, destinationID string) ([]Event, error)
	ClearEvents(ctx context.Context, destinationID string) error
}

type entityStore struct {
	mu           sync.RWMutex
	destinations map[string]models.Destination
	events       map[string][]Event
}

func NewEntityStore() EntityStore {
	return &entityStore{
		destinations: make(map[string]models.Destination),
		events:       make(map[string][]Event),
	}
}

func (s *entityStore) ListDestination(ctx context.Context) ([]models.Destination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	destinationList := make([]models.Destination, len(s.destinations))
	index := 0
	for _, destination := range s.destinations {
		destinationList[index] = destination
		index += 1
	}
	return destinationList, nil
}

func (s *entityStore) RetrieveDestination(ctx context.Context, id string) (*models.Destination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	destination, ok := s.destinations[id]
	if !ok {
		return nil, errors.New("destination not found")
	}
	return &destination, nil
}

func (s *entityStore) UpsertDestination(ctx context.Context, destination models.Destination) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destinations[destination.ID] = destination
	return nil
}

func (s *entityStore) DeleteDestination(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.destinations[id]; !ok {
		return errors.New("destination not found")
	}
	delete(s.destinations, id)
	delete(s.events, id)
	return nil
}

func (s *entityStore) ReceiveEvent(ctx context.Context, destinationID string, payload map[string]interface{}, metadata map[string]string) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	destination, ok := s.destinations[destinationID]
	if !ok {
		return nil, errors.New("destination not found")
	}

	if s.events[destinationID] == nil {
		s.events[destinationID] = make([]Event, 0)
	}

	// Convert payload to JSON for signature verification
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Initialize event
	event := Event{
		Success:  true,
		Verified: false,
		Payload:  payload,
	}

	// Check if should_err is set
	if metadata["should_err"] == "true" {
		event.Success = false
	}

	// Verify signature if credentials are present
	if signature := metadata["signature"]; signature != "" {
		// Try current secret
		if secret := destination.Credentials["secret"]; secret != "" {
			event.Verified = verifySignature(
				secret,
				payloadBytes,
				signature,
				destination.Config["signature_algorithm"],
				destination.Config["signature_encoding"],
			)
		}

		// If not verified and there's a previous secret, try that
		if !event.Verified {
			if prevSecret := destination.Credentials["previous_secret"]; prevSecret != "" {
				// Check if the previous secret is still valid
				if invalidAtStr := destination.Credentials["previous_secret_invalid_at"]; invalidAtStr != "" {
					if invalidAt, err := time.Parse(time.RFC3339, invalidAtStr); err == nil {
						if time.Now().Before(invalidAt) {
							event.Verified = verifySignature(
								prevSecret,
								payloadBytes,
								signature,
								destination.Config["signature_algorithm"],
								destination.Config["signature_encoding"],
							)
						}
					}
				}
			}
		}
	}

	s.events[destinationID] = append(s.events[destinationID], event)
	return &event, nil
}

// verifySignature verifies the signature using the provided secret and algorithm
func verifySignature(secret string, payload []byte, signature string, algorithm string, encoding string) bool {
	log.Println("verifySignature", secret, payload, signature, algorithm, encoding)
	if signature == "" {
		return false
	}

	// Default to hmac-sha256 and hex encoding
	if algorithm == "" {
		algorithm = "hmac-sha256"
	}
	if encoding == "" {
		encoding = "hex"
	}

	// Parse timestamp and signature from header
	// Header format: t=1234567890,v0=signature1,signature2
	var timestamp time.Time
	var signatures []string

	parts := strings.Split(signature, ",")
	for i, part := range parts {
		if strings.HasPrefix(part, "t=") {
			ts, err := strconv.ParseInt(strings.TrimPrefix(part, "t="), 10, 64)
			if err != nil {
				return false
			}
			timestamp = time.Unix(ts, 0)
		} else if strings.HasPrefix(part, "v0=") {
			// First v0 part contains the prefix
			signatures = append(signatures, strings.TrimPrefix(part, "v0="))
		} else if i > 0 && strings.HasPrefix(parts[i-1], "v0=") {
			// Additional signatures after v0= don't have the prefix
			signatures = append(signatures, part)
		}
	}

	// If we couldn't parse timestamp or no signatures found, verification fails
	if timestamp.IsZero() || len(signatures) == 0 {
		return false
	}

	// Create a new signature manager with the secret
	secrets := []destwebhook.WebhookSecret{
		{
			Key:       secret,
			CreatedAt: time.Now(),
		},
	}

	sm := destwebhook.NewSignatureManager(
		secrets,
		destwebhook.WithEncoder(destwebhook.GetEncoder(encoding)),
		destwebhook.WithAlgorithm(destwebhook.GetAlgorithm(algorithm)),
	)

	// Try each signature
	for _, sig := range signatures {
		if sm.VerifySignature(sig, secret, destwebhook.SignaturePayload{
			Body:      string(payload),
			Timestamp: timestamp,
		}) {
			return true
		}
	}

	return false
}

func (s *entityStore) ListEvent(ctx context.Context, destinationID string) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events, ok := s.events[destinationID]
	if !ok {
		return nil, errors.New("no events found for destination")
	}
	return events, nil
}

func (s *entityStore) ClearEvents(ctx context.Context, destinationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.destinations[destinationID]; !ok {
		return errors.New("destination not found")
	}
	s.events[destinationID] = make([]Event, 0)
	return nil
}
