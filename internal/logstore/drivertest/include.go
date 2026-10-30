package drivertest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testListAttemptInclude(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	tenantID := idgen.String()
	destID := idgen.Destination()
	t0 := time.Now().Truncate(time.Second).Add(-10 * time.Minute)

	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("include_evt"),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destID),
		testutil.EventFactory.WithMatchedDestinationIDs([]string{destID}),
		testutil.EventFactory.WithTime(t0),
		testutil.EventFactory.WithMetadata(map[string]string{"k": "v"}),
		testutil.EventFactory.WithData(json.RawMessage(`{"a":"b"}`)),
	)
	attempt := testutil.AttemptFactory.AnyPointer(
		testutil.AttemptFactory.WithID("include_att"),
		testutil.AttemptFactory.WithTenantID(tenantID),
		testutil.AttemptFactory.WithEventID(event.ID),
		testutil.AttemptFactory.WithDestinationID(destID),
		testutil.AttemptFactory.WithStatus("failed"),
		testutil.AttemptFactory.WithTime(t0),
		testutil.AttemptFactory.WithLatencyMs(42),
	)
	attempt.Code = "500"
	attempt.ResponseData = map[string]interface{}{"body": "oops"}
	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{{Event: event, Attempt: attempt}}))
	require.NoError(t, h.FlushWrites(ctx))

	list := func(t *testing.T, include ...driver.Field) *driver.AttemptRecord {
		t.Helper()
		res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
			TenantIDs: []string{tenantID},
			Limit:     10,
			Include:   include,
		})
		require.NoError(t, err)
		require.Len(t, res.Data, 1)
		return res.Data[0]
	}

	assertBase := func(t *testing.T, r *driver.AttemptRecord) {
		t.Helper()
		require.NotNil(t, r.Event)
		assert.Equal(t, event.ID, r.Event.ID)
		assert.Equal(t, tenantID, r.Event.TenantID)
		assert.Equal(t, event.Topic, r.Event.Topic)
		assert.Equal(t, event.EligibleForRetry, r.Event.EligibleForRetry)
		assert.Equal(t, attempt.ID, r.Attempt.ID)
		assert.Equal(t, "failed", r.Attempt.Status)
		assert.Equal(t, "500", r.Attempt.Code)
		assert.Equal(t, attempt.LatencyMs, r.Attempt.LatencyMs)
		assert.Equal(t, destID, r.Attempt.DestinationID)
	}

	t.Run("ListAttempt include", func(t *testing.T) {
		t.Run("nil reads no payload", func(t *testing.T) {
			r := list(t)
			assert.Empty(t, r.Event.Data)
			assert.Nil(t, r.Event.Metadata)
			assert.Nil(t, r.Attempt.ResponseData)
			assertBase(t, r)
		})
		t.Run("event.data only", func(t *testing.T) {
			r := list(t, driver.FieldEventData)
			assert.JSONEq(t, `{"a":"b"}`, string(r.Event.Data))
			assert.Nil(t, r.Event.Metadata)
			assert.Nil(t, r.Attempt.ResponseData)
			assertBase(t, r)
		})
		t.Run("event.metadata only", func(t *testing.T) {
			r := list(t, driver.FieldEventMetadata)
			assert.Empty(t, r.Event.Data)
			assert.Equal(t, event.Metadata, r.Event.Metadata)
			assert.Nil(t, r.Attempt.ResponseData)
			assertBase(t, r)
		})
		t.Run("response_data only", func(t *testing.T) {
			r := list(t, driver.FieldResponseData)
			assert.Empty(t, r.Event.Data)
			assert.Nil(t, r.Event.Metadata)
			assert.Equal(t, attempt.ResponseData, r.Attempt.ResponseData)
			assertBase(t, r)
		})
		t.Run("AllFields returns full record", func(t *testing.T) {
			r := list(t, driver.AllFields...)
			assert.JSONEq(t, `{"a":"b"}`, string(r.Event.Data))
			assert.Equal(t, event.Metadata, r.Event.Metadata)
			assert.Equal(t, attempt.ResponseData, r.Attempt.ResponseData)
			assertBase(t, r)
		})
	})
}
