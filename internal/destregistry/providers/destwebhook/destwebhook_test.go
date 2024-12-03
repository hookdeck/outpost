package destwebhook_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{}),
	)

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, webhookDestination.Validate(nil, &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := webhookDestination.Validate(nil, &invalidDestination)
		assert.Error(t, err)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{}
		err := webhookDestination.Validate(nil, &invalidDestination)

		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.url", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"url": "not-a-valid-url",
		}
		err := webhookDestination.Validate(nil, &invalidDestination)

		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.url", validationErr.Errors[0].Field)
		assert.Equal(t, "format", validationErr.Errors[0].Type)
	})
}

type webhookDestinationSuite struct {
	timeoutSecond *int
	ctx           context.Context
	server        http.Server
	webhookURL    string
	errchan       chan error
	handler       func(w http.ResponseWriter, r *http.Request)
	teardown      func()
}

func (suite *webhookDestinationSuite) SetupTest(t *testing.T) {
	teardownFuncs := []func(){}
	if suite.ctx == nil {
		var timeout time.Duration
		if suite.timeoutSecond == nil {
			timeout = 1 * time.Second
		} else {
			timeout = time.Duration(*suite.timeoutSecond) * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		suite.ctx = ctx
		teardownFuncs = append(teardownFuncs, cancel)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if suite.handler == nil {
			w.WriteHeader(http.StatusOK)
		} else {
			suite.handler(w, r)
		}
	}))
	suite.server = http.Server{
		Addr:    testutil.RandomPort(),
		Handler: mux,
	}
	suite.webhookURL = "http://localhost" + suite.server.Addr + "/webhook"

	suite.errchan = make(chan error)
	go func() {
		if err := suite.server.ListenAndServe(); err != http.ErrServerClosed {
			suite.errchan <- err
		} else {
			suite.errchan <- nil
		}
	}()
	go func() {
		<-suite.ctx.Done()
		suite.server.Shutdown(context.Background())
	}()

	suite.teardown = func() {
		for _, teardown := range teardownFuncs {
			teardown()
		}
	}
}

func (suite *webhookDestinationSuite) TeardownTest(t *testing.T) {
	suite.teardown()
}

func TestWebhookDestination_Publish(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{}),
	)

	t.Run("should validate before publish", func(t *testing.T) {
		t.Parallel()

		invalidDestination := destination
		invalidDestination.Type = "invalid"

		err := webhookDestination.Publish(nil, &invalidDestination, nil)
		assert.Error(t, err)
	})

	t.Run("should send webhook request", func(t *testing.T) {
		t.Parallel()

		// Arrange
		suite := &webhookDestinationSuite{}
		var request *http.Request
		var body []byte
		suite.handler = func(w http.ResponseWriter, r *http.Request) {
			request = r
			var err error
			body, err = io.ReadAll(request.Body)
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
		}
		suite.SetupTest(t)
		defer suite.TeardownTest(t)

		// Act
		finalDestination := destination
		finalDestination.Config["url"] = suite.webhookURL
		require.NoError(t, webhookDestination.Publish(context.Background(), &finalDestination, &models.Event{
			ID:               uuid.New().String(),
			TenantID:         uuid.New().String(),
			DestinationID:    uuid.New().String(),
			Topic:            "test",
			EligibleForRetry: true,
			Time:             time.Now(),
			Metadata: map[string]string{
				"my_metadata":      "metadatavalue",
				"another_metadata": "anothermetadatavalue",
			},
			Data: map[string]interface{}{
				"mykey": "myvalue",
			},
		}))
		require.NoError(t, <-suite.errchan)

		// Assert
		assert.NotNil(t, request)
		assert.Equal(t, "POST", request.Method)
		assert.Equal(t, "/webhook", request.URL.Path)
		assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"mykey":"myvalue"}`, string(body), "webhook request body doesn't match expectation")
		// metadata
		assert.Equal(t, "metadatavalue", request.Header.Get("x-outpost-my_metadata"))
		assert.Equal(t, "anothermetadatavalue", request.Header.Get("x-outpost-another_metadata"))
	})
}
