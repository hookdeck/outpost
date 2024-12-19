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
		topics          models.Topics
		availableTopics []string
		validated       bool
	}

	testCases := []testCase{
		{
			topics:          []string{"user.created"},
			availableTopics: testutil.TestTopics,
			validated:       true,
		},
		{
			topics:          []string{"user.created", "user.updated"},
			availableTopics: testutil.TestTopics,
			validated:       true,
		},
		{
			topics:          []string{"*"},
			availableTopics: testutil.TestTopics,
			validated:       true,
		},
		{
			topics:          []string{"*", "user.created"},
			availableTopics: testutil.TestTopics,
			validated:       false,
		},
		{
			topics:          []string{"user.invalid"},
			availableTopics: testutil.TestTopics,
			validated:       false,
		},
		{
			topics:          []string{"user.created", "user.invalid"},
			availableTopics: testutil.TestTopics,
			validated:       false,
		},
		{
			topics:          []string{},
			availableTopics: testutil.TestTopics,
			validated:       false,
		},
		// Test cases for empty availableTopics
		{
			topics:          []string{"any.topic"},
			availableTopics: []string{},
			validated:       true,
		},
		{
			topics:          []string{"any.topic", "another.topic"},
			availableTopics: []string{},
			validated:       true,
		},
		{
			topics:          []string{"*"},
			availableTopics: []string{},
			validated:       true,
		},
		{
			topics:          []string{},
			availableTopics: []string{},
			validated:       false, // still require at least one topic
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("validate topics %v with available topics %v", tc.topics, tc.availableTopics), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.validated, tc.topics.Validate(tc.availableTopics) == nil)
		})
	}
}
