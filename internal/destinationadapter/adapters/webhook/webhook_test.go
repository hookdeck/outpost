package webhook_test

import (
	"context"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters/webhook"
	"github.com/stretchr/testify/assert"
)

func TestWebhookDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := adapters.DestinationAdapterValue{
		ID:   uuid.New().String(),
		Type: "webhooks",
		Config: map[string]string{
			"url": "https://example.com",
		},
		Credentials: map[string]string{},
	}

	webhookDestination := webhook.New()

	t.Run("should not return error for valid destination", func(t *testing.T) {
		t.Parallel()

		err := webhookDestination.Validate(nil, validDestination)

		assert.Nil(t, err)
	})

	t.Run("should validate type", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := webhookDestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "invalid destination type")
	})

	t.Run("should validate config", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{}
		err := webhookDestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "url is required for webhook destination config")
	})
}

func TestWebhookDestination_Publish(t *testing.T) {
	t.Parallel()

	webhookDestination := webhook.New()

	destination := adapters.DestinationAdapterValue{
		ID:   uuid.New().String(),
		Type: "webhooks",
		Config: map[string]string{
			"url": "https://example.com",
		},
		Credentials: map[string]string{},
	}

	t.Run("should validate before publish", func(t *testing.T) {
		t.Parallel()

		invalidDestination := destination
		invalidDestination.Type = "invalid"

		err := webhookDestination.Publish(nil, invalidDestination, nil)
		assert.ErrorContains(t, err, "invalid destination type")
	})

	t.Run("should send webhook request", func(t *testing.T) {
		t.Parallel()

		// Set up test server to receive webhook
		var request *http.Request
		var body []byte
		mux := http.NewServeMux()
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = r
			var err error
			body, err = io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusOK)
		}))
		server := &http.Server{
			Addr:    randomPort(),
			Handler: mux,
		}
		serverURL := "http://localhost" + server.Addr + "/webhook"

		errchan := make(chan error)
		go func() {
			if err := server.ListenAndServe(); err != http.ErrServerClosed {
				errchan <- err
			} else {
				errchan <- nil
			}
		}()

		go func() {
			time.Sleep(time.Second / 2)
			server.Shutdown(context.Background())
		}()

		finalDestination := destination
		finalDestination.Config["url"] = serverURL

		err := webhookDestination.Publish(context.Background(), finalDestination, &adapters.Event{
			ID:               uuid.New().String(),
			TenantID:         uuid.New().String(),
			DestinationID:    uuid.New().String(),
			Topic:            "test",
			EligibleForRetry: true,
			Time:             time.Now(),
			Metadata:         map[string]string{},
			Data: map[string]interface{}{
				"mykey": "myvalue",
			},
		})

		err = <-errchan
		if err != nil {
			t.Fatal(err)
		}

		assert.NotNil(t, request)
		assert.Equal(t, "POST", request.Method)
		assert.Equal(t, "/webhook", request.URL.Path)
		assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
		assert.Equal(t, `{"mykey":"myvalue"}`, string(body), "webhook request body doesn't match expectation")
	})
}

// Create a random port number between 3500 and 3600
func randomPort() string {
	randomPortNumber := 3500 + rand.Intn(100)
	return ":" + strconv.Itoa(randomPortNumber)
}
