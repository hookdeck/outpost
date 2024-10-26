package backoff_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/EventKit/internal/backoff"
	"github.com/stretchr/testify/assert"
)

type testCase struct {
	retries int
	want    time.Duration
}

func testBackoff(t *testing.T, name string, bo backoff.Backoff, testCases []testCase) {
	t.Parallel()
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s.Duration(%d)", name, tc.retries), func(t *testing.T) {
			assert.Equal(t, tc.want, bo.Duration(tc.retries))
		})
	}
}

func TestBackoff_Exponential(t *testing.T) {
	t.Parallel()
	t.Run("ExponentialBackoff{Interval:30*time.Second,Base:2}", func(t *testing.T) {
		bo := &backoff.ExponentialBackoff{
			Interval: 30 * time.Second,
			Base:     2,
		}
		testCases := []testCase{
			{0, 30 * time.Second},
			{1, 60 * time.Second},
			{2, 120 * time.Second},
			{3, 240 * time.Second},
			{4, 480 * time.Second},
			{5, 960 * time.Second},
			{6, 1920 * time.Second},
			{7, 3840 * time.Second},
			{8, 7680 * time.Second},
			{9, 15360 * time.Second},
			{10, 30720 * time.Second},
		}
		testBackoff(t, "ExponentialBackoff{Interval:30*time.Second,Base:2}", bo, testCases)
	})

	t.Run("ExponentialBackoff{Interval:30*time.Second,Base:3}", func(t *testing.T) {
		bo := &backoff.ExponentialBackoff{
			Interval: 30 * time.Second,
			Base:     3,
		}
		testCases := []testCase{
			{0, 30 * time.Second},
			{1, 90 * time.Second},
			{2, 270 * time.Second},
			{3, 810 * time.Second},
			{4, 2430 * time.Second},
			{5, 7290 * time.Second},
			{6, 21870 * time.Second},
			{7, 65610 * time.Second},
			{8, 196830 * time.Second},
			{9, 590490 * time.Second},
			{10, 1771470 * time.Second},
		}
		testBackoff(t, "ExponentialBackoff{Interval:30*time.Second,Base:3}", bo, testCases)
	})
}

func TestBackoff_Constant(t *testing.T) {
	bo := &backoff.ConstantBackoff{
		Interval: 30 * time.Second,
	}
	testCases := []testCase{
		{0, 30 * time.Second},
		{1, 30 * time.Second},
		{2, 30 * time.Second},
		{3, 30 * time.Second},
		{4, 30 * time.Second},
		{5, 30 * time.Second},
		{6, 30 * time.Second},
		{7, 30 * time.Second},
		{8, 30 * time.Second},
		{9, 30 * time.Second},
		{10, 30 * time.Second},
	}
	testBackoff(t, "ConstantBackoff{Interval:30*time.Second}", bo, testCases)
}
