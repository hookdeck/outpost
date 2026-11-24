package memlogstore

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/drivertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memLogStoreHarness struct {
	logStore driver.LogStore
}

func (h *memLogStoreHarness) MakeDriver(ctx context.Context) (driver.LogStore, error) {
	return h.logStore, nil
}

func (h *memLogStoreHarness) Close() {
	// No-op for in-memory store
}

func (h *memLogStoreHarness) FlushWrites(ctx context.Context) error {
	// In-memory store is immediately consistent
	return nil
}

func newHarness(ctx context.Context, t *testing.T) (drivertest.Harness, error) {
	return &memLogStoreHarness{
		logStore: NewLogStore(),
	}, nil
}

func TestMemLogStoreConformance(t *testing.T) {
	drivertest.RunConformanceTests(t, newHarness)
}

func TestDecodeCursor(t *testing.T) {
	eventTime := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)

	t.Run("returns the position of a readable cursor", func(t *testing.T) {
		timeID := makeTimeID(eventTime, "evt_1")
		position, err := decodeCursor(cursor.Encode(cursorResourceEvent, cursorVersion, timeID), cursorResourceEvent)
		require.NoError(t, err)
		assert.Equal(t, timeID, position)
	})

	const (
		invalidCursor    = "invalid cursor"
		invalidTimestamp = "invalid cursor: invalid timestamp"
	)
	cases := []struct {
		name     string
		position string
		message  string
	}{
		{"empty position", "", invalidCursor},
		{"position that is one word", "yesterday", invalidCursor},
		{"position without an id", "2024-01-15T09:30:00.000000000Z_", invalidCursor},
		{"position without a timestamp", "_evt_1", invalidCursor},
		{"position with a timestamp that is not a time", "yesterday_evt_1", invalidTimestamp},
		{"position with a timestamp in another format", "2024-01-15T09:30:00Z_evt_1", invalidTimestamp},
		{"position with a timestamp in another time zone", "2024-01-15T09:30:00.000000000+01:00_evt_1", invalidTimestamp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeCursor(cursor.Encode(cursorResourceEvent, cursorVersion, tc.position), cursorResourceEvent)
			require.ErrorIs(t, err, cursor.ErrInvalidCursor)
			assert.EqualError(t, err, tc.message)
		})
	}
}
