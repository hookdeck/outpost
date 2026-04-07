package destinationmockserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	RawBody  string                 `json:"raw_body"`
}

type MockStore interface {
	ListDestination(ctx context.Context) ([]models.Destination, error)
	RetrieveDestination(ctx context.Context, id string) (*models.Destination, error)
	UpsertDestination(ctx context.Context, destination models.Destination) error
	DeleteDestination(ctx context.Context, id string) error

	ReceiveEvent(ctx context.Context, destinationID string, rawBody []byte, metadata map[string]string) (*Event, error)
	ListEvent(ctx context.Context, destinationID string) ([]Event, error)
	ClearEvents(ctx context.Context, destinationID string) error
}

type mockStore struct {
	mu           sync.RWMutex
	destinations map[string]models.Destination
	events       map[string][]Event
}

func NewMockStore() MockStore {
	return &mockStore{
		destinations: make(map[string]models.Destination),
		events:       make(map[string][]Event),
	}
}

func (s *mockStore) ListDestination(ctx context.Context) ([]models.Destination, error) {
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

func (s *mockStore) RetrieveDestination(ctx context.Context, id string) (*models.Destination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	destination, ok := s.destinations[id]
	if !ok {
		return nil, errors.New("destination not found")
	}
	return &destination, nil
}

func (s *mockStore) UpsertDestination(ctx context.Context, destination models.Destination) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destinations[destination.ID] = destination
	return nil
}

func (s *mockStore) DeleteDestination(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.destinations[id]; !ok {
		return errors.New("destination not found")
	}
	delete(s.destinations, id)
	delete(s.events, id)
	return nil
}

func (s *mockStore) ReceiveEvent(ctx context.Context, destinationID string, rawBody []byte, metadata map[string]string) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	destination, ok := s.destinations[destinationID]
	if !ok {
		return nil, errors.New("destination not found")
	}

	if s.events[destinationID] == nil {
		s.events[destinationID] = make([]Event, 0)
	}

	// Unmarshal raw body into map for Payload field
	var payload map[string]interface{}
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// Initialize event
	event := Event{
		Success:  true,
		Verified: false,
		Payload:  payload,
		RawBody:  string(rawBody),
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
				rawBody,
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
								rawBody,
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

	// Parse signature from header
	// Header format: v0=signature1,signature2
	if !strings.HasPrefix(signature, "v0=") {
		return false
	}
	signatures := strings.Split(strings.TrimPrefix(signature, "v0="), ",")
	if len(signatures) == 0 {
		return false
	}

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
		destwebhook.WithSignatureFormatter(destwebhook.NewSignatureFormatter(destwebhook.DefaultSignatureContentTmpl)),
		destwebhook.WithHeaderFormatter(destwebhook.NewHeaderFormatter(destwebhook.DefaultSignatureHeaderTmpl)),
	)

	for _, sig := range signatures {
		if sm.VerifySignature(sig, secret, destwebhook.SignaturePayload{
			Body: string(payload),
		}) {
			return true
		}
	}

	return false
}

func (s *mockStore) ListEvent(ctx context.Context, destinationID string) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events, ok := s.events[destinationID]
	if !ok {
		return nil, errors.New("no events found for destination")
	}
	return events, nil
}

func (s *mockStore) ClearEvents(ctx context.Context, destinationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.destinations[destinationID]; !ok {
		return errors.New("destination not found")
	}
	s.events[destinationID] = make([]Event, 0)
	return nil
}
