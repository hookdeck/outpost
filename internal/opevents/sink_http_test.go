package opevents_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testEvent() *opevents.OperationEvent {
	return &opevents.OperationEvent{
		ID:       "evt-1",
		Topic:    opevents.TopicAlertConsecutiveFailure,
		Time:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TenantID: "tenant-1",
		Data:     json.RawMessage(`{"key":"val"}`),
	}
}

func TestHTTPSink_Send(t *testing.T) {
	t.Parallel()

	t.Run("successful send with signature", func(t *testing.T) {
		t.Parallel()
		secret := "test-secret"
		var receivedBody []byte
		var receivedSig string

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			receivedSig = r.Header.Get("X-Outpost-Signature")

			var err error
			receivedBody, err = io.ReadAll(r.Body)
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		sink := opevents.NewHTTPSink(ts.URL, secret)
		event := testEvent()

		err := sink.Send(context.Background(), event)
		require.NoError(t, err)

		// Verify body is valid event JSON
		var got opevents.OperationEvent
		require.NoError(t, json.Unmarshal(receivedBody, &got))
		assert.Equal(t, event.ID, got.ID)
		assert.Equal(t, event.Topic, got.Topic)

		// Verify HMAC signature
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(receivedBody)
		expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		assert.Equal(t, expectedSig, receivedSig)
	})

	t.Run("no signature when secret is empty", func(t *testing.T) {
		t.Parallel()
		var receivedSig string

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedSig = r.Header.Get("X-Outpost-Signature")
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		sink := opevents.NewHTTPSink(ts.URL, "")
		err := sink.Send(context.Background(), testEvent())
		require.NoError(t, err)
		assert.Empty(t, receivedSig)
	})

	t.Run("server error returns error", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		sink := opevents.NewHTTPSink(ts.URL, "")
		err := sink.Send(context.Background(), testEvent())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 500")
	})

	t.Run("timeout returns error", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		sink := opevents.NewHTTPSink(ts.URL, "")
		err := sink.Send(ctx, testEvent())
		require.Error(t, err)
	})
}
