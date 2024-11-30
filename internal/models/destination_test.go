package models_test

import (
	"fmt"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDestinationTopics_Validate(t *testing.T) {
	t.Parallel()

	type testCase struct {
		topics    models.Topics
		validated bool
	}

	testCases := []testCase{
		{
			topics:    []string{"user.created"},
			validated: true,
		},
		{
			topics:    []string{"user.created", "user.updated"},
			validated: true,
		},
		{
			topics:    []string{"*"},
			validated: true,
		},
		{
			topics:    []string{"*", "user.created"},
			validated: false,
		},
		{
			topics:    []string{"user.invalid"},
			validated: false,
		},
		{
			topics:    []string{"user.created", "user.invalid"},
			validated: false,
		},
		{
			topics:    []string{},
			validated: false,
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("validate topics %v", tc.topics), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.validated, tc.topics.Validate(testutil.TestTopics) == nil)
		})
	}
}
