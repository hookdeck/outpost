package destwebhook_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type webhookDestinationSuite struct {
	ctx           context.Context
	server        *http.Server
	webhookURL    string
	errchan       chan error
	request       *http.Request // Capture the request for verification
	requestBody   []byte        // Capture the request body
	responseCode  int           // Configurable response code
	responseDelay time.Duration // Configurable response delay
	teardown      func()
}

func (suite *webhookDestinationSuite) SetupTest(t *testing.T) {
	teardownFuncs := []func(){}

	// Setup context with timeout
	if suite.ctx == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		suite.ctx = ctx
		teardownFuncs = append(teardownFuncs, cancel)
	}

	// Default response code if not set
	if suite.responseCode == 0 {
		suite.responseCode = http.StatusOK
	}

	// Setup server
	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture request for verification
		suite.request = r
		var err error
		suite.requestBody, err = io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Apply configured delay
		if suite.responseDelay > 0 {
			time.Sleep(suite.responseDelay)
		}

		w.WriteHeader(suite.responseCode)
	}))

	suite.server = &http.Server{
		Addr:    testutil.RandomPort(),
		Handler: mux,
	}
	suite.webhookURL = "http://localhost" + suite.server.Addr + "/webhook"

	// Start server
	suite.errchan = make(chan error)
	go func() {
		if err := suite.server.ListenAndServe(); err != http.ErrServerClosed {
			suite.errchan <- err
		} else {
			suite.errchan <- nil
		}
	}()

	// Setup shutdown on context done
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

func (suite *webhookDestinationSuite) TearDownTest(t *testing.T) {
	suite.teardown()
}

func TestWebhookDestination_Publish(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithData(map[string]interface{}{
			"foo": "bar",
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"key1": "value1",
		}),
	)

	t.Run("should send webhook request without secret", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{}),
		)

		err := webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))
		assert.Empty(t, suite.request.Header.Get("x-outpost-signature"))
		assert.Equal(t, "value1", suite.request.Header.Get("x-outpost-key1"))
		assert.JSONEq(t, `{"foo":"bar"}`, string(suite.requestBody))
	})

	t.Run("should send webhook request with one secret", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": `[{"key":"secret1","created_at":"2024-01-01T00:00:00Z"}]`,
			}),
		)

		err := webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))

		// Verify signature
		signature := suite.request.Header.Get("x-outpost-signature")
		require.NotEmpty(t, signature)
		assertValidSignature(t, "secret1", event.ID, suite.requestBody, signature)

		assert.Equal(t, "value1", suite.request.Header.Get("x-outpost-key1"))
		assert.JSONEq(t, `{"foo":"bar"}`, string(suite.requestBody))
	})

	t.Run("should send webhook request with multiple active secrets", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		now := time.Now()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": fmt.Sprintf(`[
					{"key":"secret1","created_at":"%s"},
					{"key":"secret2","created_at":"%s"}
				]`,
					now.Add(-12*time.Hour).Format(time.RFC3339), // Active secret
					now.Add(-6*time.Hour).Format(time.RFC3339),  // Active secret
				),
			}),
		)

		err := webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))

		// Verify signatures
		signatureHeader := suite.request.Header.Get("x-outpost-signature")
		require.NotEmpty(t, signatureHeader)
		signatures := strings.Split(signatureHeader, " ")
		require.Len(t, signatures, 2)

		assertValidSignature(t, "secret1", event.ID, suite.requestBody, signatures[0])
		assertValidSignature(t, "secret2", event.ID, suite.requestBody, signatures[1])

		assert.Equal(t, "value1", suite.request.Header.Get("x-outpost-key1"))
		assert.JSONEq(t, `{"foo":"bar"}`, string(suite.requestBody))
	})

	t.Run("should handle secret rotation", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		now := time.Now()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": fmt.Sprintf(`[
					{"key":"current_secret","created_at":"%s"},
					{"key":"old_secret","created_at":"%s"}
				]`,
					now.Format(time.RFC3339),
					now.Add(-25*time.Hour).Format(time.RFC3339),
				),
			}),
		)

		err := webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))

		// Verify only current secret's signature is present
		signatureHeader := suite.request.Header.Get("x-outpost-signature")
		require.NotEmpty(t, signatureHeader)
		signatures := strings.Split(signatureHeader, " ")
		require.Len(t, signatures, 1)

		assertValidSignature(t, "current_secret", event.ID, suite.requestBody, signatures[0])

		assert.Equal(t, "value1", suite.request.Header.Get("x-outpost-key1"))
		assert.JSONEq(t, `{"foo":"bar"}`, string(suite.requestBody))
	})

	t.Run("should handle timeout", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{
			responseDelay: 2 * time.Second, // Delay longer than our timeout
		}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		webhookDestination, err := destwebhook.New(
			testutil.Registry.MetadataLoader(),
			destwebhook.WithTimeout(1), // 1 second timeout
		)
		require.NoError(t, err)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
		)

		err = webhookDestination.Publish(context.Background(), &destination, &event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	})
}
